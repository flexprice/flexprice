package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// WalletExpirySuite covers ExpireCredits and
// shouldSkipCreditExpiryDueToActiveSubscriptionOrInvoice.
type WalletExpirySuite struct {
	testutil.BaseServiceTestSuite
	service  WalletService
	testData struct {
		customer *customer.Customer
		wallet   *wallet.Wallet
		now      time.Time
	}
}

func TestWalletExpiryService(t *testing.T) {
	suite.Run(t, new(WalletExpirySuite))
}

func (s *WalletExpirySuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewWalletService(newTestServiceParams(&s.BaseServiceTestSuite))
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         "cust_expiry",
		ExternalID: "ext_cust_expiry",
		Name:       "Expiry Customer",
		Email:      "expiry@example.com",
		BaseModel:  types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.wallet = &wallet.Wallet{
		ID:                  "wallet_expiry",
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

// seedCreditTxn creates a completed credit transaction directly via the
// repository so expiry dates in the past can be modelled.
func (s *WalletExpirySuite) seedCreditTxn(id string, credits decimal.Decimal, expiryDate *time.Time, mutate ...func(*wallet.Transaction)) *wallet.Transaction {
	txn := &wallet.Transaction{
		ID:                  id,
		WalletID:            s.testData.wallet.ID,
		CustomerID:          s.testData.customer.ID,
		Type:                types.TransactionTypeCredit,
		Amount:              credits,
		CreditAmount:        credits,
		CreditsAvailable:    credits,
		CreditBalanceBefore: decimal.Zero,
		CreditBalanceAfter:  credits,
		TxStatus:            types.TransactionStatusCompleted,
		ReferenceType:       types.WalletTxReferenceTypeExternal,
		ReferenceID:         "ref_" + id,
		IdempotencyKey:      "seed_" + id,
		ExpiryDate:          expiryDate,
		Currency:            "usd",
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	for _, m := range mutate {
		m(txn)
	}
	s.NoError(s.GetStores().WalletRepo.CreateTransaction(s.GetContext(), txn))
	return txn
}

// setWalletBalance sets the wallet's stored balance so debits have something
// to consume.
func (s *WalletExpirySuite) setWalletBalance(credits decimal.Decimal) {
	s.NoError(s.GetStores().WalletRepo.UpdateWalletBalance(s.GetContext(), s.testData.wallet.ID, credits, credits))
}

func (s *WalletExpirySuite) TestExpireCreditsValidation() {
	pastExpiry := s.testData.now.Add(-1 * time.Hour)
	futureExpiry := s.testData.now.Add(24 * time.Hour)

	testCases := []struct {
		name          string
		setup         func() string // returns transaction ID
		expectedError bool
		isInvalidOp   bool
	}{
		{
			name:          "transaction_not_found_returns_error",
			setup:         func() string { return "wtxn_missing_expiry" },
			expectedError: true,
		},
		{
			name: "debit_transaction_cannot_be_expired",
			setup: func() string {
				txn := s.seedCreditTxn("wtxn_debit_exp", decimal.NewFromInt(5), &pastExpiry, func(t *wallet.Transaction) {
					t.Type = types.TransactionTypeDebit
					t.CreditsAvailable = decimal.Zero
				})
				return txn.ID
			},
			expectedError: true,
			isInvalidOp:   true,
		},
		{
			name: "credit_without_expiry_date_cannot_be_expired",
			setup: func() string {
				return s.seedCreditTxn("wtxn_no_expiry", decimal.NewFromInt(5), nil).ID
			},
			expectedError: true,
			isInvalidOp:   true,
		},
		{
			name: "credit_not_yet_expired_cannot_be_expired",
			setup: func() string {
				return s.seedCreditTxn("wtxn_future_expiry", decimal.NewFromInt(5), &futureExpiry).ID
			},
			expectedError: true,
			isInvalidOp:   true,
		},
		{
			name: "credit_with_no_credits_available_cannot_be_expired",
			setup: func() string {
				return s.seedCreditTxn("wtxn_zero_available", decimal.NewFromInt(5), &pastExpiry, func(t *wallet.Transaction) {
					t.CreditsAvailable = decimal.Zero
				}).ID
			},
			expectedError: true,
			isInvalidOp:   true,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			txnID := tc.setup()
			result, err := s.service.ExpireCredits(s.GetContext(), txnID)
			if tc.expectedError {
				s.Error(err)
				s.Nil(result)
				if tc.isInvalidOp {
					s.True(ierr.IsInvalidOperation(err))
				}
				return
			}
			s.NoError(err)
		})
	}
}

func (s *WalletExpirySuite) TestExpireCreditsSkippedForActiveSubscription() {
	pastExpiry := s.testData.now.Add(-1 * time.Hour)
	credits := decimal.RequireFromString("12.3456")
	txn := s.seedCreditTxn("wtxn_skip_sub", credits, &pastExpiry)
	s.setWalletBalance(credits)

	// Active standalone subscription for the customer blocks expiry.
	sub := &subscription.Subscription{
		ID:                 "sub_blocks_expiry",
		PlanID:             "plan_expiry",
		CustomerID:         s.testData.customer.ID,
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		StartDate:          s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(6 * 24 * time.Hour),
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	// Backdate so the created_at <= now time-range filter matches deterministically.
	sub.CreatedAt = s.testData.now.Add(-24 * time.Hour)
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))

	result, err := s.service.ExpireCredits(s.GetContext(), txn.ID)
	s.NoError(err)
	s.NotNil(result)
	s.False(result.Expired)
	s.Equal(types.CreditExpirySkipReasonActiveSubscription, result.SkipReason)

	// Nothing was debited: balance and credits available are unchanged.
	storedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.True(storedWallet.CreditBalance.Equal(credits))

	storedTxn, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), txn.ID)
	s.NoError(err)
	s.True(storedTxn.CreditsAvailable.Equal(credits))
}

func (s *WalletExpirySuite) TestExpireCreditsSkippedForUnpaidInvoiceInGrantPeriod() {
	pastExpiry := s.testData.now.Add(-1 * time.Hour)
	credits := decimal.NewFromInt(30)
	txn := s.seedCreditTxn("wtxn_skip_inv", credits, &pastExpiry)
	s.setWalletBalance(credits)

	// Unpaid subscription invoice whose billing period ends before the grant
	// expiry blocks expiry.
	inv := &invoice.Invoice{
		ID:              "inv_blocks_expiry",
		CustomerID:      s.testData.customer.ID,
		Currency:        "usd",
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusPending,
		AmountDue:       decimal.NewFromInt(50),
		AmountRemaining: decimal.NewFromInt(50),
		PeriodStart:     lo.ToPtr(s.testData.now.Add(-48 * time.Hour)),
		PeriodEnd:       lo.ToPtr(s.testData.now.Add(-2 * time.Hour)),
		BaseModel:       types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(s.GetContext(), inv))

	result, err := s.service.ExpireCredits(s.GetContext(), txn.ID)
	s.NoError(err)
	s.NotNil(result)
	s.False(result.Expired)
	s.Equal(types.CreditExpirySkipReasonActiveInvoice, result.SkipReason)

	storedTxn, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), txn.ID)
	s.NoError(err)
	s.True(storedTxn.CreditsAvailable.Equal(credits), "skipped expiry must not consume credits")
}

func (s *WalletExpirySuite) TestExpireCreditsDebitsWallet() {
	pastExpiry := s.testData.now.Add(-1 * time.Hour)
	farFuture := s.testData.now.Add(30 * 24 * time.Hour)

	expiring := decimal.RequireFromString("12.3456")
	remaining := decimal.NewFromInt(5)
	total := expiring.Add(remaining)

	expiredTxn := s.seedCreditTxn("wtxn_expiring", expiring, &pastExpiry)
	s.seedCreditTxn("wtxn_still_valid", remaining, &farFuture)
	s.setWalletBalance(total)

	result, err := s.service.ExpireCredits(s.GetContext(), expiredTxn.ID)
	s.NoError(err)
	s.NotNil(result)
	s.True(result.Expired)
	s.Equal(types.CreditExpirySkipReasonNone, result.SkipReason)

	// The expired credit is fully consumed.
	storedTxn, err := s.GetStores().WalletRepo.GetTransactionByID(s.GetContext(), expiredTxn.ID)
	s.NoError(err)
	s.True(storedTxn.CreditsAvailable.IsZero())

	// Wallet retains only the non-expired credits (decimal-exact).
	storedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
	s.NoError(err)
	s.True(storedWallet.CreditBalance.Equal(remaining), "expected %s got %s", remaining, storedWallet.CreditBalance)
	s.True(storedWallet.Balance.Equal(remaining))

	// A debit transaction with reason CREDIT_EXPIRED is recorded, keyed by the
	// expired transaction ID for idempotency.
	debit, err := s.GetStores().WalletRepo.GetTransactionByIdempotencyKey(s.GetContext(), expiredTxn.ID)
	s.NoError(err)
	s.Equal(types.TransactionTypeDebit, debit.Type)
	s.Equal(types.TransactionReasonCreditExpired, debit.TransactionReason)
	s.True(debit.CreditAmount.Equal(expiring))
	s.True(debit.CreditBalanceAfter.Equal(remaining))

	s.Run("second_expiry_attempt_is_rejected_and_does_not_double_debit", func() {
		result, err := s.service.ExpireCredits(s.GetContext(), expiredTxn.ID)
		s.Error(err)
		s.True(ierr.IsInvalidOperation(err), "already-expired credit has no credits available")
		s.Nil(result)

		storedWallet, err := s.GetStores().WalletRepo.GetWalletByID(s.GetContext(), s.testData.wallet.ID)
		s.NoError(err)
		s.True(storedWallet.CreditBalance.Equal(remaining), "repeat expiry must not debit the wallet again")
	})
}
