package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// WalletTxnQuerySuite covers GetWalletTransactionByID,
// ListWalletTransactionsByFilter (incl. expansions) and
// CompletePurchasedCreditTransactionWithRetry.
type WalletTxnQuerySuite struct {
	testutil.BaseServiceTestSuite
	service  WalletService
	testData struct {
		customer *customer.Customer
		wallet   *wallet.Wallet
		now      time.Time
	}
}

func TestWalletTxnQueryService(t *testing.T) {
	suite.Run(t, new(WalletTxnQuerySuite))
}

func (s *WalletTxnQuerySuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewWalletService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_txn_query",
		ExternalID: "ext_cust_txn_query",
		Name:       "Txn Query Customer",
		Email:      "txnquery@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.wallet = &wallet.Wallet{
		ID:                  "wallet_txn_query",
		CustomerID:          s.testData.customer.ID,
		Currency:            "usd",
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletStatus:        types.WalletStatusActive,
		WalletType:          types.WalletTypePrePaid,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().WalletRepo.CreateWallet(s.GetContext(), s.testData.wallet))
}

// creditWallet runs a real credit operation through the service so the
// resulting transaction carries the full production shape.
func (s *WalletTxnQuerySuite) creditWallet(credits decimal.Decimal, idempotencyKey string) *wallet.Transaction {
	err := s.service.CreditWallet(s.GetContext(), &wallet.WalletOperation{
		WalletID:          s.testData.wallet.ID,
		Type:              types.TransactionTypeCredit,
		CreditAmount:      credits,
		TransactionReason: types.TransactionReasonFreeCredit,
		IdempotencyKey:    idempotencyKey,
	})
	s.NoError(err)

	txn, err := s.GetStores().WalletRepo.GetTransactionByIdempotencyKey(s.GetContext(), idempotencyKey)
	s.NoError(err)
	return txn
}

func (s *WalletTxnQuerySuite) TestGetWalletTransactionByID() {
	s.Run("returns_transaction_with_full_details", func() {
		credits := decimal.RequireFromString("15.75")
		txn := s.creditWallet(credits, "idem_get_by_id")

		resp, err := s.service.GetWalletTransactionByID(s.GetContext(), txn.ID)
		s.NoError(err)
		s.Equal(txn.ID, resp.ID)
		s.Equal(s.testData.wallet.ID, resp.WalletID)
		s.Equal(s.testData.customer.ID, resp.CustomerID)
		s.Equal(types.TransactionTypeCredit, resp.Type)
		s.True(resp.CreditAmount.Equal(credits))
		s.True(resp.CreditsAvailable.Equal(credits))
		s.Equal(types.TransactionStatusCompleted, resp.TxStatus)
	})

	s.Run("returns_error_for_unknown_transaction", func() {
		resp, err := s.service.GetWalletTransactionByID(s.GetContext(), "wtxn_missing")
		s.Error(err)
		s.Nil(resp)
	})
}

func (s *WalletTxnQuerySuite) TestListWalletTransactionsByFilterValidation() {
	s.Run("invalid_expand_field_returns_error", func() {
		filter := types.NewWalletTransactionFilter()
		filter.QueryFilter.Expand = lo.ToPtr("plan")

		resp, err := s.service.ListWalletTransactionsByFilter(s.GetContext(), filter)
		s.Error(err)
		s.Nil(resp)
	})

	s.Run("reference_type_without_reference_id_returns_validation_error", func() {
		filter := types.NewWalletTransactionFilter()
		filter.ReferenceType = lo.ToPtr("EXTERNAL")

		resp, err := s.service.ListWalletTransactionsByFilter(s.GetContext(), filter)
		s.Error(err)
		s.True(ierr.IsValidation(err))
		s.Nil(resp)
	})

	s.Run("nil_filter_on_empty_store_returns_empty_response", func() {
		resp, err := s.service.ListWalletTransactionsByFilter(s.GetContext(), nil)
		s.NoError(err)
		s.NotNil(resp)
		s.Empty(resp.Items)
		s.Equal(0, resp.Pagination.Total)
	})
}

func (s *WalletTxnQuerySuite) TestListWalletTransactionsByFilter() {
	creditA := s.creditWallet(decimal.NewFromInt(10), "idem_list_a")
	s.creditWallet(decimal.NewFromInt(20), "idem_list_b")

	s.Run("lists_all_transactions_without_expand", func() {
		filter := types.NewWalletTransactionFilter()
		filter.WalletID = lo.ToPtr(s.testData.wallet.ID)

		resp, err := s.service.ListWalletTransactionsByFilter(s.GetContext(), filter)
		s.NoError(err)
		s.Len(resp.Items, 2)
		s.Equal(2, resp.Pagination.Total)
		for _, item := range resp.Items {
			s.Equal(s.testData.wallet.ID, item.WalletID)
			s.Nil(item.Customer)
			s.Nil(item.Wallet)
			s.Nil(item.CreatedByUser)
		}
	})

	s.Run("filters_by_transaction_type", func() {
		filter := types.NewWalletTransactionFilter()
		filter.WalletID = lo.ToPtr(s.testData.wallet.ID)
		filter.Type = lo.ToPtr(types.TransactionTypeDebit)

		resp, err := s.service.ListWalletTransactionsByFilter(s.GetContext(), filter)
		s.NoError(err)
		s.Empty(resp.Items, "no debit transactions were created")
	})

	s.Run("expands_customer_and_wallet_without_user_fixture", func() {
		// No tenant/user fixture exists yet: created_by_user expansion silently
		// yields nothing while customer and wallet still expand.
		filter := types.NewWalletTransactionFilter()
		filter.WalletID = lo.ToPtr(s.testData.wallet.ID)
		filter.QueryFilter.Expand = lo.ToPtr("customer,wallet,created_by_user")

		resp, err := s.service.ListWalletTransactionsByFilter(s.GetContext(), filter)
		s.NoError(err)
		s.Len(resp.Items, 2)
		for _, item := range resp.Items {
			s.NotNil(item.Customer)
			s.Equal(s.testData.customer.ID, item.Customer.Customer.ID)
			s.NotNil(item.Wallet)
			s.Equal(s.testData.wallet.ID, item.Wallet.ID)
			s.Nil(item.CreatedByUser, "user lookup fails without tenant fixture and must not fail the request")
		}
	})

	s.Run("expands_created_by_user_when_user_exists", func() {
		// Seed the tenant + user matching the context identities.
		s.NoError(s.GetStores().TenantRepo.Create(s.GetContext(), &tenant.Tenant{
			ID:        types.GetTenantID(s.GetContext()),
			Name:      "Test Tenant",
			Status:    types.StatusPublished,
			CreatedAt: s.testData.now,
			UpdatedAt: s.testData.now,
		}))
		s.NoError(s.GetStores().UserRepo.Create(s.GetContext(), &user.User{
			ID:        creditA.CreatedBy,
			Email:     "creator@example.com",
			Type:      types.UserTypeUser,
			BaseModel: types.GetDefaultBaseModel(s.GetContext()),
		}))

		filter := types.NewWalletTransactionFilter()
		filter.WalletID = lo.ToPtr(s.testData.wallet.ID)
		filter.QueryFilter.Expand = lo.ToPtr("created_by_user")

		resp, err := s.service.ListWalletTransactionsByFilter(s.GetContext(), filter)
		s.NoError(err)
		s.Len(resp.Items, 2)
		for _, item := range resp.Items {
			s.NotNil(item.CreatedByUser)
			s.Equal(creditA.CreatedBy, item.CreatedByUser.ID)
			s.Equal("creator@example.com", item.CreatedByUser.Email)
			s.Nil(item.Customer, "customer expansion was not requested")
			s.Nil(item.Wallet, "wallet expansion was not requested")
		}
	})
}

func (s *WalletTxnQuerySuite) TestCompletePurchasedCreditTransactionWithRetry() {
	credits := decimal.RequireFromString("25.5")

	// Create a PENDING purchased-credit transaction plus its invoice through
	// the real top-up flow (auto-complete is disabled by default settings).
	topUpResp, err := s.service.TopUpWallet(s.GetContext(), s.testData.wallet.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      credits,
		TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
		IdempotencyKey:    lo.ToPtr("idem_purchase_pending"),
	})
	s.NoError(err)
	s.NotNil(topUpResp.WalletTransaction)
	s.NotNil(topUpResp.InvoiceID, "invoiced purchase must create an invoice")
	pendingTxnID := topUpResp.WalletTransaction.ID
	s.Equal(types.TransactionStatusPending, topUpResp.WalletTransaction.TxStatus)

	// Pending purchase must not credit the wallet yet.
	storedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.True(storedWallet.CreditBalance.IsZero(), "pending purchase must not move the balance")

	s.Run("completes_pending_purchased_credit_transaction", func() {
		err := s.service.CompletePurchasedCreditTransactionWithRetry(s.GetContext(), pendingTxnID)
		s.NoError(err)

		// Transaction is completed with the credits made available.
		storedTxn, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), pendingTxnID)
		s.NoError(err)
		s.Equal(types.TransactionStatusCompleted, storedTxn.TxStatus)
		s.True(storedTxn.CreditsAvailable.Equal(credits))
		s.True(storedTxn.CreditBalanceAfter.Equal(credits))

		// Wallet balance reflects exactly one credit application.
		storedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
		s.NoError(err)
		s.True(storedWallet.CreditBalance.Equal(credits), "expected %s got %s", credits, storedWallet.CreditBalance)
		s.True(storedWallet.Balance.Equal(credits), "conversion rate is 1 so balance equals credits")
	})

	s.Run("second_completion_is_idempotent", func() {
		err := s.service.CompletePurchasedCreditTransactionWithRetry(s.GetContext(), pendingTxnID)
		s.NoError(err, "completing an already-completed transaction must succeed without side effects")

		storedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
		s.NoError(err)
		s.True(storedWallet.CreditBalance.Equal(credits), "duplicate completion must not double-credit the wallet")
	})

	s.Run("rejects_pending_debit_transaction", func() {
		debitTxn := &wallet.Transaction{
			ID:             "wtxn_pending_debit",
			WalletID:       s.testData.wallet.ID,
			CustomerID:     s.testData.customer.ID,
			Type:           types.TransactionTypeDebit,
			Amount:         decimal.NewFromInt(5),
			CreditAmount:   decimal.NewFromInt(5),
			TxStatus:       types.TransactionStatusPending,
			ReferenceType:  types.WalletTxReferenceTypeExternal,
			ReferenceID:    "ref_pending_debit",
			IdempotencyKey: "idem_pending_debit",
			Currency:       "usd",
			BaseModel:      types.GetDefaultBaseModel(s.GetContext()),
		}
		s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), debitTxn))

		err := s.service.CompletePurchasedCreditTransactionWithRetry(s.GetContext(), debitTxn.ID)
		s.Error(err)
		s.True(ierr.IsInvalidOperation(err))

		// Wallet balance untouched by the rejected completion.
		storedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
		s.NoError(err)
		s.True(storedWallet.CreditBalance.Equal(credits))
	})

	s.Run("returns_error_for_unknown_transaction", func() {
		err := s.service.CompletePurchasedCreditTransactionWithRetry(s.GetContext(), "wtxn_does_not_exist")
		s.Error(err)
	})
}
