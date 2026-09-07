package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// seedPayFirstTopupSession creates a pending pay-first top-up and its checkout
// session, returning the wallet transaction id and the session.
func (s *WalletServiceSuite) seedPayFirstTopupSession(idempotencyKey string, credits decimal.Decimal, bonus *decimal.Decimal) (string, *domainCheckout.CheckoutSession) {
	ctx := s.GetContext()
	params := s.buildServiceParams()
	ws := s.service.(*walletService)

	txID, invID, err := ws.handlePurchasedCreditInvoicedTransaction(
		ctx,
		s.testData.wallet.ID,
		lo.ToPtr(idempotencyKey),
		&dto.TopUpWalletRequest{
			CreditsToAdd:      credits,
			BonusCreditsToAdd: bonus,
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
			Checkout:          s.checkoutParamsRazorpay(),
			Metadata:          types.Metadata{types.WalletMetadataKeyAutoTopup: "true"},
		},
	)
	s.Require().NoError(err)

	invSvc := NewInvoiceService(params)
	draftInv, err := invSvc.GetInvoice(ctx, invID)
	s.Require().NoError(err)

	checkoutSvc := &checkoutSessionService{ServiceParams: params}
	payResp, err := checkoutSvc.createCheckoutPayment(ctx, &draftInv.Invoice, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      s.testData.customer.ID,
		Action:          types.CheckoutActionWalletTopup,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			WalletTopupParams: &types.WalletTopupParams{
				WalletID:            s.testData.wallet.ID,
				WalletTransactionID: txID,
			},
		}),
		CheckoutInvoiceID: &invID,
		CheckoutPaymentID: &payResp.ID,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))

	return txID, session
}

func (s *WalletServiceSuite) TestCleanupCheckoutSession_FailsPendingWalletTopupTransaction() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	balanceBefore := s.testData.wallet.CreditBalance

	txID, session := s.seedPayFirstTopupSession("cleanup-fails-topup", decimal.NewFromInt(300), nil)

	checkoutSvc := &checkoutSessionService{ServiceParams: s.buildServiceParams()}
	s.Require().NoError(checkoutSvc.CleanupCheckoutSession(ctx, session.ID, nil))

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusFailed, tx.TxStatus, "abandoned top-up must not stay pending")

	w, err := s.GetStores().WalletRepo.GetWalletByID(ctx, s.testData.wallet.ID)
	s.Require().NoError(err)
	s.True(balanceBefore.Equal(w.CreditBalance), "failing a pending top-up must be balance-neutral")

	cleaned, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, cleaned.CheckoutStatus)
}

// A pending bonus grant hangs off the purchase transaction; leaving it behind
// strands a credit that is never granted nor failed.
func (s *WalletServiceSuite) TestCleanupCheckoutSession_FailsPendingBonusGrant() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	bonus := decimal.NewFromInt(40)

	txID, session := s.seedPayFirstTopupSession("cleanup-fails-bonus", decimal.NewFromInt(400), &bonus)

	checkoutSvc := &checkoutSessionService{ServiceParams: s.buildServiceParams()}
	s.Require().NoError(checkoutSvc.CleanupCheckoutSession(ctx, session.ID, nil))

	filter := types.NewWalletTransactionFilter()
	filter.WalletID = &s.testData.wallet.ID
	txs, err := s.GetStores().WalletRepo.ListWalletTransactions(ctx, filter)
	s.Require().NoError(err)

	var foundBonus bool
	for _, tx := range txs {
		if tx.ParentTransactionID == txID {
			s.Equal(types.TransactionStatusFailed, tx.TxStatus)
			foundBonus = true
		}
	}
	s.True(foundBonus, "expected a bonus child transaction")
}

// Cleanup is idempotent, and a completed session must never be un-credited.
func (s *WalletServiceSuite) TestCleanupCheckoutSession_CompletedSessionUntouched() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("cleanup-after-complete", decimal.NewFromInt(200), nil)

	checkoutSvc := &checkoutSessionService{ServiceParams: s.buildServiceParams()}
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_cleanup_guard_001",
	}))

	s.Require().NoError(checkoutSvc.CleanupCheckoutSession(ctx, session.ID, nil))

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusCompleted, tx.TxStatus)
}

// B2: Delete must reach a terminal checkout_status, not just archive the row —
// otherwise it keeps holding the idempotency key and blocking the pending guard.
func (s *WalletServiceSuite) TestDeleteCheckoutSession_CleansUpBeforeArchiving() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("delete-cleans-up", decimal.NewFromInt(150), nil)

	checkoutSvc := &checkoutSessionService{ServiceParams: s.buildServiceParams()}
	s.Require().NoError(checkoutSvc.Delete(ctx, session.ID))

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusFailed, tx.TxStatus)

	deleted, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, deleted.CheckoutStatus)
}

// The auto-topup pending guard reads the last auto_topup-tagged transaction, so a
// checkout-backed top-up that never settles would block it. Auto top-up does not
// route through checkout today; this pins the behaviour for when it does (Phase 14).
func (s *WalletServiceSuite) TestFailedTopupTransaction_UnblocksAutoTopup() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("unblocks-auto-topup", decimal.NewFromInt(100), nil)

	last, err := s.GetStores().WalletRepo.GetLastAutoTopupTransactionForWallet(ctx, s.testData.wallet.ID)
	s.Require().NoError(err)
	s.Require().NotNil(last)
	s.Equal(types.TransactionStatusPending, last.TxStatus, "guard blocks while pending")

	checkoutSvc := &checkoutSessionService{ServiceParams: s.buildServiceParams()}
	s.Require().NoError(checkoutSvc.CleanupCheckoutSession(ctx, session.ID, nil))

	last, err = s.GetStores().WalletRepo.GetLastAutoTopupTransactionForWallet(ctx, s.testData.wallet.ID)
	s.Require().NoError(err)
	s.Require().NotNil(last)
	s.Equal(txID, last.ID)
	s.NotEqual(types.TransactionStatusPending, last.TxStatus, "guard must no longer block")
}

// A voided invoice will never be paid, so its pending purchased-credit transaction
// must not stay pending — that permanently blocks the wallet's auto-topup guard.
func (s *WalletServiceSuite) TestVoidInvoice_FailsPendingPurchasedCreditTransaction() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	params := s.buildServiceParams()
	ws := s.service.(*walletService)
	balanceBefore := s.testData.wallet.CreditBalance

	txID, invID, err := ws.handlePurchasedCreditInvoicedTransaction(
		ctx,
		s.testData.wallet.ID,
		lo.ToPtr("void-fails-pending-tx"),
		&dto.TopUpWalletRequest{
			CreditsToAdd:      decimal.NewFromInt(100),
			TransactionReason: types.TransactionReasonPurchasedCreditInvoiced,
			Metadata:          types.Metadata{types.WalletMetadataKeyAutoTopup: "true"},
		},
	)
	s.Require().NoError(err)

	invSvc := NewInvoiceService(params)
	_, voidErr := invSvc.VoidInvoice(ctx, invID, dto.InvoiceVoidRequest{})
	s.Require().NoError(voidErr)

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusFailed, tx.TxStatus, "voided invoice must not leave a pending top-up")

	w, err := s.GetStores().WalletRepo.GetWalletByID(ctx, s.testData.wallet.ID)
	s.Require().NoError(err)
	s.True(balanceBefore.Equal(w.CreditBalance), "voiding an unpaid top-up must be balance-neutral")
}
