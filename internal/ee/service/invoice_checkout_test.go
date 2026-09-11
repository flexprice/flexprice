package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

func (s *InvoiceServiceSuite) checkoutParamsRazorpay() *dto.CheckoutParams {
	return &dto.CheckoutParams{
		PaymentParams: dto.PaymentParams{
			PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		},
	}
}

// stubCheckoutProvider points the shared integration factory at a fake gateway.
func (s *InvoiceServiceSuite) stubCheckoutProvider() *fakeCheckoutProvider {
	provider := &fakeCheckoutProvider{}
	s.service.(*invoiceService).IntegrationFactory.SetCheckoutProvider(provider)
	return provider
}

func (s *InvoiceServiceSuite) checkoutServiceWith(provider interfaces.CheckoutProvider) *checkoutSessionService {
	s.service.(*invoiceService).IntegrationFactory.SetCheckoutProvider(provider)
	return NewCheckoutSessionService(s.service.(*invoiceService).ServiceParams).(*checkoutSessionService)
}

func (s *InvoiceServiceSuite) gatedInvoiceRequest() dto.CreateInvoiceRequest {
	return dto.CreateInvoiceRequest{
		CustomerID:    s.testData.customer.ID,
		InvoiceType:   types.InvoiceTypeOneOff,
		Currency:      "usd",
		AmountDue:     decimal.NewFromInt(100),
		Total:         decimal.NewFromInt(100),
		Subtotal:      decimal.NewFromInt(100),
		BillingReason: types.InvoiceBillingReasonManual,
		LineItems: []dto.CreateInvoiceLineItemRequest{{
			DisplayName: lo.ToPtr("Custom charge"),
			Amount:      decimal.NewFromInt(100),
			Quantity:    decimal.NewFromInt(1),
		}},
		Checkout: s.checkoutParamsRazorpay(),
	}
}

func (s *InvoiceServiceSuite) activeSessionsFor(invoiceID string) []*domainCheckout.CheckoutSession {
	sessions, err := s.GetStores().CheckoutSessionRepo.List(s.GetContext(), &types.CheckoutSessionFilter{
		QueryFilter:        types.NewNoLimitQueryFilter(),
		CheckoutInvoiceIDs: []string{invoiceID},
	})
	s.Require().NoError(err)
	return sessions
}

func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_NoCheckout_StillFinalizes() {
	resp, err := s.service.CreateOneOffInvoice(s.GetContext(), dto.CreateInvoiceRequest{
		CustomerID:    s.testData.customer.ID,
		InvoiceType:   types.InvoiceTypeOneOff,
		Currency:      "usd",
		AmountDue:     decimal.NewFromInt(100),
		Total:         decimal.NewFromInt(100),
		Subtotal:      decimal.NewFromInt(100),
		BillingReason: types.InvoiceBillingReasonManual,
	})
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, resp.InvoiceStatus)
	s.Nil(resp.CheckoutSession)
}

func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_Checkout_LeavesDraftAndReturnsSession() {
	s.stubCheckoutProvider()

	resp, err := s.service.CreateOneOffInvoice(s.GetContext(), s.gatedInvoiceRequest())
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, resp.InvoiceStatus)
	s.Empty(lo.FromPtr(resp.InvoiceNumber), "a gated invoice must not burn an invoice number")

	s.Require().NotNil(resp.CheckoutSession)
	s.Equal(types.CheckoutStatusPending, resp.CheckoutSession.CheckoutStatus)
	s.Equal(types.CheckoutActionPayInvoice, resp.CheckoutSession.Action)
	s.Require().NotNil(resp.CheckoutSession.CheckoutInvoiceID)
	s.Equal(resp.ID, *resp.CheckoutSession.CheckoutInvoiceID)
	s.Require().NotNil(resp.CheckoutSession.PaymentAction)
	s.NotEmpty(resp.CheckoutSession.PaymentAction.URL)

	s.Require().NotNil(resp.CheckoutSession.CheckoutPaymentID)
	payment, err := s.GetStores().PaymentRepo.Get(s.GetContext(), *resp.CheckoutSession.CheckoutPaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusPending, payment.PaymentStatus)
	s.Equal(types.PaymentMethodTypePaymentLink, payment.PaymentMethodType)
}

func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_Checkout_RejectsConflictingFields() {
	tests := []struct {
		name    string
		mutate  func(*dto.CreateInvoiceRequest)
		wantErr string
	}{
		{"invoice_status", func(r *dto.CreateInvoiceRequest) {
			r.InvoiceStatus = lo.ToPtr(types.InvoiceStatusFinalized)
		}, "invoice_status is not supported"},
		{"payment_status", func(r *dto.CreateInvoiceRequest) {
			r.PaymentStatus = lo.ToPtr(types.PaymentStatusSucceeded)
		}, "payment_status is not supported"},
		{"amount_paid", func(r *dto.CreateInvoiceRequest) {
			r.AmountPaid = lo.ToPtr(decimal.NewFromInt(10))
		}, "amount_paid is not supported"},
		{"total_prepaid_applied", func(r *dto.CreateInvoiceRequest) {
			r.TotalPrepaidApplied = lo.ToPtr(decimal.NewFromInt(10))
		}, "total_prepaid_applied is not supported"},
		{"subscription_id", func(r *dto.CreateInvoiceRequest) {
			r.SubscriptionID = lo.ToPtr("sub_123")
		}, "subscription_id is not supported"},
		{"force_sync_invoice", func(r *dto.CreateInvoiceRequest) {
			r.ForceSyncInvoice = true
		}, "force_sync_invoice is not supported"},
		{"invoice_type_credit", func(r *dto.CreateInvoiceRequest) {
			r.InvoiceType = types.InvoiceTypeCredit
		}, "invoice_type must be ONE_OFF"},
		{"zero_amount_due", func(r *dto.CreateInvoiceRequest) {
			r.AmountDue = decimal.Zero
		}, "amount_due must be greater than zero"},
		{"empty_line_items", func(r *dto.CreateInvoiceRequest) {
			r.LineItems = nil
		}, "line_items is required"},
		{"past_due_date", func(r *dto.CreateInvoiceRequest) {
			r.DueDate = lo.ToPtr(time.Now().UTC().Add(-time.Hour))
		}, "due_date must be in the future"},
		{"unsupported_provider", func(r *dto.CreateInvoiceRequest) {
			r.Checkout.PaymentProvider = types.CheckoutPaymentProvider("stripe")
		}, "invalid checkout payment provider"},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := s.gatedInvoiceRequest()
			tt.mutate(&req)

			_, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
			s.Require().Error(err)
			s.Contains(err.Error(), tt.wantErr)
		})
	}
}

// The validation must be reached through the service, not only unit-tested on the DTO:
// CreateInvoiceRequest.Validate() is never called on this path.
func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_Checkout_ValidationRejectsBeforeAnyInvoice() {
	req := s.gatedInvoiceRequest()
	req.LineItems = nil

	filter := types.NewNoLimitInvoiceFilter()
	filter.CustomerID = s.testData.customer.ID
	before, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)

	_, err = s.service.CreateOneOffInvoice(s.GetContext(), req)
	s.Require().Error(err)

	after, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	s.Equal(len(before), len(after), "validation must reject before any draft is created")
}

func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_Checkout_SameIdempotencyKeyReusesSession() {
	s.stubCheckoutProvider()

	req := s.gatedInvoiceRequest()
	req.IdempotencyKey = lo.ToPtr("gated-invoice-idemp")

	first, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
	s.Require().NoError(err)
	second, err := s.service.CreateOneOffInvoice(s.GetContext(), req)
	s.Require().NoError(err)

	s.Equal(first.ID, second.ID, "same idempotency key must reuse the draft")
	s.Require().NotNil(second.CheckoutSession)
	s.Equal(first.CheckoutSession.ID, second.CheckoutSession.ID, "a second session must not be opened")
	s.Equal(first.CheckoutSession.PaymentAction.URL, second.CheckoutSession.PaymentAction.URL)
	s.Len(s.activeSessionsFor(first.ID), 1)
}

func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_Checkout_ProviderLinkFailureCleansUp() {
	// No provider stub: the suite has no razorpay connection, so link creation fails.
	resp, err := s.service.CreateOneOffInvoice(s.GetContext(), s.gatedInvoiceRequest())
	s.Require().Error(err)
	s.Nil(resp)

	sessions, listErr := s.GetStores().CheckoutSessionRepo.List(s.GetContext(), &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerIDs: []string{s.testData.customer.ID},
		Actions:     []types.CheckoutAction{types.CheckoutActionPayInvoice},
	})
	s.Require().NoError(listErr)
	for _, sess := range sessions {
		s.True(sess.CheckoutStatus.IsTerminal(), "a failed fulfilment must leave the session terminal")
	}
}

func (s *InvoiceServiceSuite) TestCompletePayInvoiceCheckout_FinalizesAndMarksPaid() {
	provider := s.stubCheckoutProvider()
	ctx := s.GetContext()

	resp, err := s.service.CreateOneOffInvoice(ctx, s.gatedInvoiceRequest())
	s.Require().NoError(err)
	sessionID := resp.CheckoutSession.ID

	checkoutSvc := s.checkoutServiceWith(provider)
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_test_123",
	}))

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.NotEmpty(lo.FromPtr(inv.InvoiceNumber), "finalization must assign an invoice number")
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus)
	s.True(inv.AmountPaid.Equal(inv.AmountDue), "amount paid %s should equal amount due %s", inv.AmountPaid, inv.AmountDue)

	session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, sessionID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, session.CheckoutStatus)
}

func (s *InvoiceServiceSuite) TestPayInvoiceCheckout_ExpiryVoidsAndArchives() {
	provider := s.stubCheckoutProvider()
	ctx := s.GetContext()

	resp, err := s.service.CreateOneOffInvoice(ctx, s.gatedInvoiceRequest())
	s.Require().NoError(err)

	checkoutSvc := s.checkoutServiceWith(provider)
	cutoff := time.Now().UTC().Add(2 * time.Hour)
	result, err := checkoutSvc.CleanupAllExpiredSessions(ctx, &cutoff)
	s.Require().NoError(err)
	s.GreaterOrEqual(result.Total, 1)

	session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, resp.CheckoutSession.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, session.CheckoutStatus)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusDeleted, inv.Status)
}

func (s *InvoiceServiceSuite) TestPayInvoiceCheckout_CleanupIsIdempotent() {
	provider := s.stubCheckoutProvider()
	ctx := s.GetContext()

	resp, err := s.service.CreateOneOffInvoice(ctx, s.gatedInvoiceRequest())
	s.Require().NoError(err)

	checkoutSvc := s.checkoutServiceWith(provider)
	cutoff := time.Now().UTC().Add(2 * time.Hour)
	_, err = checkoutSvc.CleanupAllExpiredSessions(ctx, &cutoff)
	s.Require().NoError(err)

	first, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)

	_, err = checkoutSvc.CleanupAllExpiredSessions(ctx, &cutoff)
	s.Require().NoError(err)

	second, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)
	s.True(first.RefundedAmount.Equal(second.RefundedAmount), "a second cleanup must not refund again")
	s.Equal(first.InvoiceStatus, second.InvoiceStatus)
}

// ── Guards on the manual state-change operations ─────────────────────────────

// seedGatedInvoice returns a DRAFT one-off invoice with a pending checkout session over it.
func (s *InvoiceServiceSuite) seedGatedInvoice() (*dto.InvoiceResponse, string) {
	s.stubCheckoutProvider()
	resp, err := s.service.CreateOneOffInvoice(s.GetContext(), s.gatedInvoiceRequest())
	s.Require().NoError(err)
	s.Require().NotNil(resp.CheckoutSession)
	return resp, resp.CheckoutSession.ID
}

func (s *InvoiceServiceSuite) TestGatedInvoice_ManualStateChangesRejected() {
	inv, _ := s.seedGatedInvoice()
	ctx := s.GetContext()

	s.Run("finalize", func() {
		err := s.service.FinalizeInvoice(ctx, inv.ID, dto.FinalizeInvoiceRequest{})
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})

	s.Run("void", func() {
		_, err := s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{})
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})

	s.Run("compute", func() {
		_, _, err := s.service.ComputeInvoice(ctx, inv.ID, &dto.InvoiceComputeRequest{})
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})

	s.Run("update_payment_status", func() {
		err := s.service.UpdatePaymentStatus(ctx, inv.ID, types.PaymentStatusSucceeded, nil)
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})
}

// The owning session must be able to act on the invoice it created while still pending;
// any other caller identity must not. This is the re-entrancy regression test.
func (s *InvoiceServiceSuite) TestGatedInvoice_OwningSessionIsAdmitted() {
	inv, sessionID := s.seedGatedInvoice()
	ctx := s.GetContext()

	_, _, err := s.service.ComputeInvoice(ctx, inv.ID, &dto.InvoiceComputeRequest{
		InvoiceStateChangeSource: dto.NewInvoiceStateChangeSource("cs_someone_else"),
	})
	s.Require().Error(err, "a different session must still be blocked")
	s.Contains(err.Error(), "gated by an active checkout session")

	s.Require().NoError(s.service.FinalizeInvoice(ctx, inv.ID, dto.FinalizeInvoiceRequest{
		InvoiceStateChangeSource: dto.NewInvoiceStateChangeSource(sessionID),
	}), "the owning session must be admitted")

	_, err = s.service.VoidInvoice(ctx, inv.ID, dto.InvoiceVoidRequest{
		InvoiceStateChangeSource: dto.NewInvoiceStateChangeSource(sessionID),
	})
	s.Require().NoError(err, "the owning session must be admitted")
}

func (s *InvoiceServiceSuite) TestGatedInvoice_IsFinalizationDueSkipsWhileSessionActive() {
	provider := s.stubCheckoutProvider()
	ctx := s.GetContext()

	inv, err := s.service.CreateOneOffInvoice(ctx, s.gatedInvoiceRequest())
	s.Require().NoError(err)

	due, err := s.service.IsFinalizationDue(ctx, inv.ID)
	s.Require().NoError(err)
	s.False(due, "the cron must not finalize a draft under an open checkout")

	checkoutSvc := s.checkoutServiceWith(provider)
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, inv.CheckoutSession.ID, &types.CheckoutProviderResult{}))

	// Completion finalized it, so it is no longer a draft — the guard no longer applies.
	gating, err := s.service.(*invoiceService).activeCheckoutSessionForInvoice(ctx, inv.ID)
	s.Require().NoError(err)
	s.Nil(gating, "a completed session must stop gating")
}

func (s *InvoiceServiceSuite) TestGatedInvoice_DirectPaymentRejectedButCheckoutPaymentAllowed() {
	inv, _ := s.seedGatedInvoice()
	ctx := s.GetContext()

	paySvc := NewPaymentService(s.service.(*invoiceService).ServiceParams)
	_, err := paySvc.CreatePayment(ctx, &dto.CreatePaymentRequest{
		DestinationType:   types.PaymentDestinationTypeInvoice,
		DestinationID:     inv.ID,
		PaymentMethodType: types.PaymentMethodTypeOffline,
		Amount:            inv.AmountDue,
		Currency:          inv.Currency,
		ProcessPayment:    false,
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "gated by an active checkout session")

	// The checkout's own payment path does not route through the eligibility validator.
	domainInv, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	_, err = paySvc.CreatePaymentForCheckout(ctx, &dto.CreateCheckoutPaymentRequest{
		Invoice: domainInv,
		Gateway: types.PaymentGatewayTypeChargebee,
	})
	s.Require().NoError(err, "CreatePaymentForCheckout must stay unaffected by the guard")
}

// ── Prepaid credits (B2) ─────────────────────────────────────────────────────

func (s *InvoiceServiceSuite) seedPrepaidWallet(credits decimal.Decimal) (string, decimal.Decimal) {
	ctx := s.GetContext()
	w := &wallet.Wallet{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_WALLET),
		CustomerID:          s.testData.customer.ID,
		Currency:            "usd",
		Balance:             decimal.Zero,
		CreditBalance:       decimal.Zero,
		WalletStatus:        types.WalletStatusActive,
		Name:                "Gated checkout wallet",
		ConversionRate:      decimal.NewFromInt(1),
		TopupConversionRate: decimal.NewFromInt(1),
		WalletType:          types.WalletTypePrePaid,
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().WalletRepo.CreateWallet(ctx, w))

	// Fund through the service so the credits are backed by a transaction the debit
	// can draw down; a bare balance field is not enough.
	walletSvc := NewWalletService(s.service.(*invoiceService).ServiceParams)
	_, err := walletSvc.TopUpWallet(ctx, w.ID, &dto.TopUpWalletRequest{
		CreditsToAdd:      credits,
		TransactionReason: types.TransactionReasonPurchasedCreditDirect,
		IdempotencyKey:    lo.ToPtr("seed-" + w.ID),
	})
	s.Require().NoError(err)

	funded, err := s.GetStores().WalletRepo.GetWalletByID(ctx, w.ID)
	s.Require().NoError(err)
	return w.ID, funded.CreditBalance
}

// Credits only apply to USAGE line items — see CalculateCreditAdjustments.
func (s *InvoiceServiceSuite) gatedUsageInvoiceRequest(amount decimal.Decimal) dto.CreateInvoiceRequest {
	req := s.gatedInvoiceRequest()
	req.AmountDue = amount
	req.Total = amount
	req.Subtotal = amount
	req.LineItems = []dto.CreateInvoiceLineItemRequest{{
		DisplayName: lo.ToPtr("Metered usage"),
		PriceType:   lo.ToPtr(string(types.PRICE_TYPE_USAGE)),
		Amount:      amount,
		Quantity:    decimal.NewFromInt(1),
	}}
	return req
}

func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_Checkout_FullCreditCoverageRejected() {
	s.stubCheckoutProvider()
	ctx := s.GetContext()
	walletID, seeded := s.seedPrepaidWallet(decimal.NewFromInt(500))

	_, err := s.service.CreateOneOffInvoice(ctx, s.gatedUsageInvoiceRequest(decimal.NewFromInt(100)))
	s.Require().Error(err)
	s.Contains(err.Error(), "prepaid credits cover the full amount")

	after, err := s.GetStores().WalletRepo.GetWalletByID(ctx, walletID)
	s.Require().NoError(err)
	s.True(after.CreditBalance.Equal(seeded),
		"the void must return the credits: had %s, now %s", seeded, after.CreditBalance)

	sessions, err := s.GetStores().CheckoutSessionRepo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerIDs: []string{s.testData.customer.ID},
		Actions:     []types.CheckoutAction{types.CheckoutActionPayInvoice},
	})
	s.Require().NoError(err)
	s.Empty(sessions, "no session may be opened for a fully-credited invoice")
}

// B2 regression: credits are debited at compute, so an expiring session must void
// (returning them) before it archives the invoice.
func (s *InvoiceServiceSuite) TestPayInvoiceCheckout_ExpiryRefundsPrepaidCredits() {
	provider := s.stubCheckoutProvider()
	ctx := s.GetContext()
	walletID, seeded := s.seedPrepaidWallet(decimal.NewFromInt(30))

	resp, err := s.service.CreateOneOffInvoice(ctx, s.gatedUsageInvoiceRequest(decimal.NewFromInt(100)))
	s.Require().NoError(err)
	s.Require().True(resp.TotalPrepaidCreditsApplied.IsPositive(),
		"expected credits applied at compute, got %s", resp.TotalPrepaidCreditsApplied)

	debited, err := s.GetStores().WalletRepo.GetWalletByID(ctx, walletID)
	s.Require().NoError(err)
	s.Require().True(debited.CreditBalance.LessThan(seeded),
		"compute must debit the wallet: before=%s after=%s", seeded, debited.CreditBalance)

	checkoutSvc := s.checkoutServiceWith(provider)
	cutoff := time.Now().UTC().Add(2 * time.Hour)
	_, err = checkoutSvc.CleanupAllExpiredSessions(ctx, &cutoff)
	s.Require().NoError(err)

	restored, err := s.GetStores().WalletRepo.GetWalletByID(ctx, walletID)
	s.Require().NoError(err)
	s.True(restored.CreditBalance.Equal(seeded),
		"expiry must refund the credits: started %s, ended %s", seeded, restored.CreditBalance)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusVoided, inv.InvoiceStatus)
	s.Equal(types.StatusDeleted, inv.Status)
}

// ── Amount-mutating edits are gated too ──────────────────────────────────────

// A live link was created at the old amount, so anything that moves what the customer
// owes must be refused while the session is open.
func (s *InvoiceServiceSuite) TestGatedInvoice_AmountMutatingEditsRejected() {
	inv, _ := s.seedGatedInvoice()
	ctx := s.GetContext()

	lineItems, err := s.GetStores().InvoiceLineItemRepo.ListByInvoiceID(ctx, inv.ID)
	s.Require().NoError(err)
	s.Require().NotEmpty(lineItems)

	s.Run("add_line_items", func() {
		_, err := s.service.AddBulkLineItem(ctx, inv.ID, dto.AddBulkLineItemRequest{
			Items: []dto.AddLineItemRequest{{
				DisplayName: "Sneaky extra",
				Amount:      decimal.NewFromInt(50),
				Quantity:    decimal.NewFromInt(1),
			}},
		})
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})

	s.Run("remove_line_items", func() {
		_, err := s.service.RemoveBulkLineItem(ctx, inv.ID, dto.RemoveBulkLineItemRequest{
			LineItemIDs: []string{lineItems[0].ID},
		})
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})

	s.Run("modify_invoice", func() {
		_, err := s.service.ModifyInvoice(ctx, inv.ID, dto.ExecuteInvoiceModifyRequest{
			Type: dto.InvoiceModifyTypeLineItem,
			LineItemParams: &dto.InvoiceModifyLineItemParams{
				Action:      dto.InvoiceModifyLineItemActionRemove,
				LineItemIDs: []string{lineItems[0].ID},
			},
		})
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})

	s.Run("apply_discount", func() {
		_, err := s.service.UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{ApplyDiscount: true})
		s.Require().Error(err)
		s.Contains(err.Error(), "gated by an active checkout session")
	})

	// Vendor sync writes metadata on gated invoices; blocking that would break it.
	s.Run("metadata_only_still_allowed", func() {
		_, err := s.service.UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{
			Metadata: &types.Metadata{"razorpay_payment_url": "https://rzp.io/x"},
		})
		s.Require().NoError(err)
	})

	after, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.True(after.AmountDue.Equal(inv.AmountDue), "amount must be untouched, got %s", after.AmountDue)
}

// Session create can fail after compute has already debited prepaid credits (duplicate
// checkout idempotency key). Archiving the draft alone would burn them.
func (s *InvoiceServiceSuite) TestCreateOneOffInvoice_Checkout_SessionCreateFailureRefundsCredits() {
	s.stubCheckoutProvider()
	ctx := s.GetContext()
	walletID, seeded := s.seedPrepaidWallet(decimal.NewFromInt(30))

	idempKey := "gated-session-create-conflict"
	blocking := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      s.testData.customer.ID,
		Action:          types.CheckoutActionPayInvoice,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		IdempotencyKey:  &idempKey,
		ExpiresAt:       time.Now().UTC().Add(time.Hour),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, blocking))

	req := s.gatedUsageInvoiceRequest(decimal.NewFromInt(100))
	req.Checkout.IdempotencyKey = &idempKey

	_, err := s.service.CreateOneOffInvoice(ctx, req)
	s.Require().Error(err, "duplicate checkout idempotency key must fail session create")

	restored, err := s.GetStores().WalletRepo.GetWalletByID(ctx, walletID)
	s.Require().NoError(err)
	s.True(restored.CreditBalance.Equal(seeded),
		"credits debited at compute must be returned: started %s, ended %s", seeded, restored.CreditBalance)
}
