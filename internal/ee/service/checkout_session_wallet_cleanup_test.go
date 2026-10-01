package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
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

	checkoutSvc := NewCheckoutSessionService(params).(*checkoutSessionService)
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

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
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

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
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

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_cleanup_guard_001",
	}))

	s.Require().NoError(checkoutSvc.CleanupCheckoutSession(ctx, session.ID, nil))

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusCompleted, tx.TxStatus)
}

// A concurrent cancel holds a copy that still says pending after Complete has claimed
// the row. Cleanup must not overwrite completed or reverse the credit.
func (s *WalletServiceSuite) TestCleanupCheckoutSession_StalePendingCopyDoesNotClobberCompleted() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("cleanup-stale-pending", decimal.NewFromInt(200), nil)
	stale := *session

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_stale_cleanup_001",
	}))

	s.Require().NoError(checkoutSvc.terminateCheckoutSession(ctx, &stale, newTerminateCheckoutSessionParams(types.CheckoutStatusExpired, types.WebhookEventCheckoutSessionExpired, nil, true)))

	stored, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, stored.CheckoutStatus)

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

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	s.Require().NoError(checkoutSvc.Delete(ctx, session.ID))

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusFailed, tx.TxStatus)

	deleted, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCancelled, deleted.CheckoutStatus)
}

func (s *WalletServiceSuite) TestCancelCheckoutSession_ExpiresInFlightAndKeepsPublished() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("cancel-in-flight", decimal.NewFromInt(150), nil)

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	resp, err := checkoutSvc.Cancel(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCancelled, resp.CheckoutStatus)
	s.True(resp.Terminal)

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusFailed, tx.TxStatus)

	stored, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCancelled, stored.CheckoutStatus)
	s.Equal(types.StatusPublished, stored.Status)
}

func (s *WalletServiceSuite) TestCancelCheckoutSession_RejectsCompleted() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("cancel-after-complete", decimal.NewFromInt(200), nil)

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_cancel_guard_001",
	}))

	resp, err := checkoutSvc.Cancel(ctx, session.ID)
	s.Nil(resp)
	s.True(ierr.IsValidation(err))

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusCompleted, tx.TxStatus)
}

func (s *WalletServiceSuite) TestCancelCheckoutSession_AlreadyTerminalIsNoop() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("cancel-already-expired", decimal.NewFromInt(100), nil)

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	s.Require().NoError(checkoutSvc.CleanupCheckoutSession(ctx, session.ID, nil))

	resp, err := checkoutSvc.Cancel(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, resp.CheckoutStatus)

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusFailed, tx.TxStatus)
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

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
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

// The Step-5 void-before-archive must not disturb wallet_topup cleanup: nothing is
// funded on an unpaid top-up draft, so the void is skipped and the purchased-credit
// transaction is failed exactly once (not again via VoidInvoice's own handling).
func (s *WalletServiceSuite) TestCleanupCheckoutSession_TopupDraftNotVoided() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()

	txID, session := s.seedPayFirstTopupSession("cleanup-no-void", decimal.NewFromInt(250), nil)
	invoiceID := *session.CheckoutInvoiceID

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	s.Require().NoError(checkoutSvc.CleanupCheckoutSession(ctx, session.ID, nil))

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, invoiceID)
	s.Require().NoError(err)
	s.NotEqual(types.InvoiceStatusVoided, inv.InvoiceStatus,
		"an unfunded draft must be archived without a void")
	s.True(inv.RefundedAmount.IsZero(), "nothing was funded, so nothing may be refunded")

	tx, err := s.GetStores().WalletRepo.GetTransactionByID(ctx, txID)
	s.Require().NoError(err)
	s.Equal(types.TransactionStatusFailed, tx.TxStatus)
}

func (s *WalletServiceSuite) webhookNames() []types.WebhookEventName {
	events := s.GetPublishedWebhooks()
	names := make([]types.WebhookEventName, 0, len(events))
	for _, event := range events {
		names = append(names, event.EventName)
	}
	return names
}

func (s *WalletServiceSuite) TestCancelCheckoutSession_VoidsPendingPayment() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	_, session := s.seedPayFirstTopupSession("cancel-voids-payment", decimal.NewFromInt(100), nil)

	paySvc := NewPaymentService(s.buildServiceParams())
	_, err := paySvc.UpdatePayment(ctx, *session.CheckoutPaymentID, dto.UpdatePaymentRequest{
		GatewayTrackingID: lo.ToPtr("inv_open"),
	})
	s.Require().NoError(err)

	provider := &fakeCheckoutProvider{}
	s.GetIntegrationFactory().SetCheckoutProvider(provider)
	defer s.GetIntegrationFactory().SetCheckoutProvider(nil)
	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	resp, err := checkoutSvc.Cancel(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCancelled, resp.CheckoutStatus)
	s.Equal([]string{"inv_open"}, provider.cancelIDs)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, *session.CheckoutPaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusVoided, payment.PaymentStatus)
	s.Equal(types.StatusPublished, payment.Status)
	s.Contains(s.webhookNames(), types.WebhookEventCheckoutSessionCancelled)
	s.NotContains(s.webhookNames(), types.WebhookEventPaymentFailed)
}

func (s *WalletServiceSuite) TestCancelCheckoutSession_RefundsCapturedPayment() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	_, session := s.seedPayFirstTopupSession("cancel-refunds-capture", decimal.NewFromInt(100), nil)

	paySvc := NewPaymentService(s.buildServiceParams())
	_, err := paySvc.UpdatePayment(ctx, *session.CheckoutPaymentID, dto.UpdatePaymentRequest{
		GatewayTrackingID: lo.ToPtr("inv_paid"),
	})
	s.Require().NoError(err)

	provider := &fakeCheckoutProvider{openCharge: interfaces.OpenChargeResult{
		Captured:         true,
		GatewayPaymentID: "pay_rzp_1",
	}}
	s.GetIntegrationFactory().SetCheckoutProvider(provider)
	defer s.GetIntegrationFactory().SetCheckoutProvider(nil)
	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	resp, err := checkoutSvc.Cancel(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCancelled, resp.CheckoutStatus)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, *session.CheckoutPaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusRefunded, payment.PaymentStatus)
	s.Equal("pay_rzp_1", lo.FromPtr(payment.GatewayPaymentID))
	s.NotContains(s.webhookNames(), types.WebhookEventPaymentFailed)
}

func (s *WalletServiceSuite) TestCleanupAllExpiredSessions_PublishesExpired() {
	s.seedAutoComplete(false)
	ctx := s.GetContext()
	_, session := s.seedPayFirstTopupSession("expiry-publishes-expired", decimal.NewFromInt(100), nil)
	session.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Update(ctx, session))

	checkoutSvc := NewCheckoutSessionService(s.buildServiceParams()).(*checkoutSessionService)
	_, err := checkoutSvc.CleanupAllExpiredSessions(ctx, nil)
	s.Require().NoError(err)

	stored, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, stored.CheckoutStatus)
	s.Contains(s.webhookNames(), types.WebhookEventCheckoutSessionExpired)
}
