package service

import (
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrialConversionPeriod(t *testing.T) {
	start := time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		cycle      types.BillingCycle
		wantAnchor time.Time
		wantEnd    time.Time
	}{
		{"calendar", types.BillingCycleCalendar, time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
		{"anniversary", types.BillingCycleAnniversary, start, time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &subscription.Subscription{BillingCycle: tt.cycle, BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, Timezone: "UTC"}
			anchor, end, err := trialConversionPeriod(sub, start)
			require.NoError(t, err)
			assert.True(t, anchor.Equal(tt.wantAnchor), "anchor %s", anchor)
			assert.True(t, end.Equal(tt.wantEnd), "end %s", end)
			assert.True(t, sub.BillingAnchor.IsZero(), "helper must not mutate the subscription")
		})
	}
}

// seedTrialingFixedFeeSubscription creates a trialing monthly subscription (13 days left) with one fixed advance fee,
// and points checkout at a fake gateway so pay-first sessions can start.
func (s *SubscriptionServiceSuite) seedTrialingFixedFeeSubscription(amount decimal.Decimal) *subscription.Subscription {
	ctx := s.GetContext()
	s.GetIntegrationFactory().SetCheckoutProvider(&fakeCheckoutProvider{})
	now := time.Now().UTC()
	trialStart, trialEnd := now.Add(-24*time.Hour), now.Add(13*24*time.Hour)

	pr := &price.Price{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE), Amount: amount, Currency: "usd",
		EntityType: types.PRICE_ENTITY_TYPE_PLAN, EntityID: s.testData.plan.ID, Type: types.PRICE_TYPE_FIXED,
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1, BillingModel: types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence: types.InvoiceCadenceAdvance, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, pr))

	sub := &subscription.Subscription{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION), PlanID: s.testData.plan.ID,
		CustomerID: s.testData.customer.ID, StartDate: trialStart, BillingAnchor: trialStart,
		CurrentPeriodStart: trialStart, CurrentPeriodEnd: trialEnd, TrialStart: &trialStart, TrialEnd: &trialEnd,
		Currency: "usd", BillingCycle: types.BillingCycleAnniversary, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, BillingCadence: types.BILLING_CADENCE_RECURRING,
		SubscriptionStatus: types.SubscriptionStatusTrialing,
		PaymentBehavior:    string(types.PaymentBehaviorDefaultActive), CollectionMethod: string(types.CollectionMethodChargeAutomatically),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM), SubscriptionID: sub.ID,
		CustomerID: sub.CustomerID, EntityID: s.testData.plan.ID, EntityType: types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: s.testData.plan.Name, PriceID: pr.ID, PriceType: pr.Type, DisplayName: "Platform fee",
		Quantity: decimal.NewFromInt(1), Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: types.InvoiceCadenceAdvance, StartDate: trialStart, BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	return sub
}

func (s *SubscriptionServiceSuite) trialEndCheckoutRequest() dto.ExecuteSubscriptionModifyRequest {
	return dto.ExecuteSubscriptionModifyRequest{
		Type:           dto.SubscriptionModifyTypeTrialEnd,
		TrialEndParams: &dto.SubModifyTrialEndRequest{Action: dto.TrialEndActionImmediate},
		Checkout:       s.razorpayCheckoutParams(),
	}
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_GatesWithoutTouchingTrial() {
	ctx := s.GetContext()
	sub := s.seedTrialingFixedFeeSubscription(decimal.NewFromInt(200))
	modSvc := NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams)

	resp, err := modSvc.Execute(ctx, sub.ID, s.trialEndCheckoutRequest())
	s.Require().NoError(err)
	s.Require().NotNil(resp.CheckoutSession, "a positive first-period invoice is gated")
	session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, resp.CheckoutSession.ID)
	s.Require().NoError(err)
	start := session.Configuration.ModifySubscriptionParams.TrialEndAt
	s.Require().NotNil(start)
	s.True(start.Equal(start.Truncate(time.Microsecond)), "checkout start must survive PostgreSQL timestamp persistence")

	got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusTrialing, got.SubscriptionStatus, "trial untouched until payment")
	s.True(got.TrialEnd.Equal(*sub.TrialEnd))

	draft, err := s.GetStores().InvoiceRepo.Get(ctx, lo.FromPtr(resp.CheckoutSession.CheckoutInvoiceID))
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, draft.InvoiceStatus)
	s.Equal(string(types.InvoiceBillingReasonSubscriptionTrialEnd), draft.BillingReason)
	s.True(draft.Total.Equal(decimal.NewFromInt(200)), "full first-period fee, got %s", draft.Total)
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_PendingCheckoutBlocksImmediateEnd() {
	tests := []struct {
		name         string
		withCheckout bool
	}{
		{"second pay-first start", true},
		{"direct immediate end", false},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			sub, _, _ := s.startTrialEndCheckout()
			req := s.trialEndCheckoutRequest()
			if !tt.withCheckout {
				req.Checkout = nil
			}
			_, err := NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams).Execute(ctx, sub.ID, req)
			s.True(ierr.IsAlreadyExists(err), "the pending checkout owns the trial end, got %v", err)
			got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			s.Equal(types.SubscriptionStatusTrialing, got.SubscriptionStatus)
		})
	}
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_PendingCheckoutBlocksScheduledDate() {
	ctx := s.GetContext()
	sub, _, _ := s.startTrialEndCheckout()
	before, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)

	_, err = NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams).Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeTrialEnd,
		TrialEndParams: &dto.SubModifyTrialEndRequest{
			Action:      dto.TrialEndActionScheduledDate,
			NewTrialEnd: lo.ToPtr(before.TrialEnd.Add(48 * time.Hour)),
		},
	})
	s.True(ierr.IsAlreadyExists(err), "a pending checkout freezes trial fields, got %v", err)

	got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.True(got.TrialEnd.Equal(*before.TrialEnd), "trial end must not move")
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_Validation() {
	future := time.Now().UTC().Add(48 * time.Hour)
	tests := []struct {
		name    string
		params  *dto.SubModifyTrialEndRequest
		wantErr bool
	}{
		{"immediate with checkout", &dto.SubModifyTrialEndRequest{Action: dto.TrialEndActionImmediate}, false},
		{"scheduled date with checkout", &dto.SubModifyTrialEndRequest{Action: dto.TrialEndActionScheduledDate, NewTrialEnd: &future}, true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			req := s.trialEndCheckoutRequest()
			req.TrialEndParams = tt.params
			err := req.Validate()
			if tt.wantErr {
				s.True(ierr.IsValidation(err))
				return
			}
			s.NoError(err)
		})
	}
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_ZeroInvoiceEndsTrialImmediately() {
	ctx := s.GetContext()
	sub := s.seedTrialingFixedFeeSubscription(decimal.Zero)
	modSvc := NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams)
	resp, err := modSvc.Execute(ctx, sub.ID, s.trialEndCheckoutRequest())
	s.Require().NoError(err)
	s.Nil(resp.CheckoutSession, "nothing to collect")
	got, _ := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.NotEqual(types.SubscriptionStatusTrialing, got.SubscriptionStatus)
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_CronSkipsWhilePending() {
	ctx := s.GetContext()
	sub, _, _ := s.startTrialEndCheckout()

	inv, err := s.service.(*subscriptionService).ProcessSingleSubscriptionTrialEnd(ctx, sub, sub.TrialEnd.Add(time.Hour))
	s.Require().NoError(err)
	s.Nil(inv, "cron must not raise a second trial-end invoice while checkout is pending")
	got, _ := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Equal(types.SubscriptionStatusTrialing, got.SubscriptionStatus)
}

func (s *SubscriptionServiceSuite) invoicesFor(subscriptionID string) []*invoice.Invoice {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subscriptionID
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return invoices
}

func (s *SubscriptionServiceSuite) startTrialEndCheckout() (*subscription.Subscription, string, string) {
	sub := s.seedTrialingFixedFeeSubscription(decimal.NewFromInt(200))
	modSvc := NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams)
	resp, err := modSvc.Execute(s.GetContext(), sub.ID, s.trialEndCheckoutRequest())
	s.Require().NoError(err)
	return sub, resp.CheckoutSession.ID, lo.FromPtr(resp.CheckoutSession.CheckoutInvoiceID)
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_PaidEndsTrialAndActivates() {
	ctx := s.GetContext()
	sub, sessionID, invoiceID := s.startTrialEndCheckout()
	checkoutSvc := &checkoutSessionService{ServiceParams: s.service.(*subscriptionService).ServiceParams}

	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{ProviderPaymentIntentID: "pi_trial_1"}))

	got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, got.SubscriptionStatus)
	inv, err := s.GetStores().InvoiceRepo.Get(ctx, invoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus)
	s.True(got.TrialEnd.Equal(lo.FromPtr(inv.PeriodStart)), "trial ends where the paid period starts")
	s.True(got.CurrentPeriodEnd.Equal(lo.FromPtr(inv.PeriodEnd)))
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_CompletesAfterPaymentStatusSync() {
	ctx := s.GetContext()
	sub, sessionID, invoiceID := s.startTrialEndCheckout()
	params := s.service.(*subscriptionService).ServiceParams
	session, err := params.CheckoutSessionRepo.Get(ctx, sessionID)
	s.Require().NoError(err)
	p, err := params.PaymentRepo.Get(ctx, lo.FromPtr(session.CheckoutPaymentID))
	s.Require().NoError(err)
	paySvc := NewPaymentService(params).(*paymentService)
	_, err = paySvc.applyGatewayPaymentStatus(ctx, p, types.PaymentStatusSucceeded, "pay_trial_synced")
	s.Require().NoError(err)
	before, err := params.InvoiceRepo.Get(ctx, invoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusDraft, before.InvoiceStatus)
	s.Equal(types.PaymentStatusSucceeded, before.PaymentStatus)
	s.True(before.AmountPaid.Equal(before.AmountDue))
	trial, err := params.SubRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusTrialing, trial.SubscriptionStatus)

	// A paid draft must not activate even after conversion has moved the sub to incomplete.
	modSvc := NewSubscriptionModificationService(params).(*subscriptionModificationService)
	s.Require().NoError(modSvc.applyTrialEndConversion(ctx, sub.ID, *session.Configuration.ModifySubscriptionParams.TrialEndAt))
	s.Require().NoError(NewInvoiceService(params).HandleIncompleteSubscriptionPayment(ctx, before))
	converted, err := params.SubRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusIncomplete, converted.SubscriptionStatus)

	checkoutSvc := NewCheckoutSessionService(params)
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{ProviderPaymentIntentID: "pay_trial_synced"}))
	after, err := params.SubRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, after.SubscriptionStatus)
	s.True(after.TrialEnd.Equal(*session.Configuration.ModifySubscriptionParams.TrialEndAt))
	inv, err := params.InvoiceRepo.Get(ctx, invoiceID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus)
	s.True(inv.AmountPaid.Equal(inv.AmountDue), "checkout must settle the invoice exactly once")
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_DoubleCompleteIdempotent() {
	ctx := s.GetContext()
	sub, sessionID, _ := s.startTrialEndCheckout()
	params := s.service.(*subscriptionService).ServiceParams
	checkoutSvc := &checkoutSessionService{ServiceParams: params}
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{ProviderPaymentIntentID: "pi_trial_2"}))
	first, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)

	// A replay stops at the session layer; a delivery that reaches apply again must not move the period.
	err = checkoutSvc.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{ProviderPaymentIntentID: "pi_trial_2"})
	s.True(ierr.IsAlreadyExists(err), "replayed completion is rejected at the session layer")
	modSvc := NewSubscriptionModificationService(params).(*subscriptionModificationService)
	s.Require().NoError(modSvc.applyTrialEndConversion(ctx, sub.ID, lo.FromPtr(first.TrialEnd)))

	again, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, again.SubscriptionStatus)
	s.True(again.CurrentPeriodStart.Equal(first.CurrentPeriodStart))
	s.True(again.CurrentPeriodEnd.Equal(first.CurrentPeriodEnd))
	finalizedTrialEnd := lo.Filter(s.invoicesFor(sub.ID), func(i *invoice.Invoice, _ int) bool {
		return i.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialEnd) && i.InvoiceStatus == types.InvoiceStatusFinalized
	})
	s.Len(finalizedTrialEnd, 1, "exactly one finalized trial-end invoice")
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_ReplayAfterTimestampPersistence() {
	tests := []struct {
		name   string
		nanos  int
		active bool
		offset time.Duration
	}{
		{"incomplete rounded down", 123456123, false, 0},
		{"active rounded up", 123456789, true, 0},
		{"active rounded into next second", 999999789, true, 0},
		{"different conversion rejected", 123456789, true, time.Microsecond},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			sub := s.seedTrialingFixedFeeSubscription(decimal.NewFromInt(200))
			subSvc := s.service.(*subscriptionService)
			start := time.Date(2026, 10, 15, 18, 27, 24, tt.nanos, time.UTC).Truncate(time.Microsecond)
			// Model the timestamp returned by PostgreSQL after the first conversion commits.
			persistedStart := start.Round(time.Microsecond).Add(tt.offset)
			_, err := subSvc.convertTrialToFirstPaidPeriod(ctx, sub, persistedStart)
			s.Require().NoError(err)
			if tt.active {
				s.Require().NoError(subSvc.completeTrialConversionToActive(ctx, sub))
			}
			before, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)

			modSvc := NewSubscriptionModificationService(subSvc.ServiceParams).(*subscriptionModificationService)
			err = modSvc.applyTrialEndConversion(ctx, sub.ID, start)
			if tt.offset != 0 {
				s.Require().Error(err)
			} else {
				s.Require().NoError(err)
			}
			after, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			s.Equal(before, after, "replay must not change subscription state or billing dates")
		})
	}
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_UnpaidCleanupLeavesTrial() {
	tests := []struct {
		name   string
		reason error
	}{
		{"expired", nil},
		{"payment failed", errors.New("card_declined")},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			sub, sessionID, invoiceID := s.startTrialEndCheckout()
			checkoutSvc := &checkoutSessionService{ServiceParams: s.service.(*subscriptionService).ServiceParams}
			session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, sessionID)
			s.Require().NoError(err)

			s.Require().NoError(checkoutSvc.cleanupCheckoutSession(ctx, session, tt.reason))

			got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			s.Equal(types.SubscriptionStatusTrialing, got.SubscriptionStatus)
			s.True(got.TrialEnd.Equal(*sub.TrialEnd), "trial end unchanged")
			inv, err := s.GetStores().InvoiceRepo.Get(ctx, invoiceID)
			s.Require().NoError(err)
			s.Equal(types.InvoiceStatusDraft, inv.InvoiceStatus, "unpaid draft never finalized")

			// The cron owns the trial again once the session is gone.
			cronInv, err := s.service.(*subscriptionService).ProcessSingleSubscriptionTrialEnd(ctx, got, got.TrialEnd.Add(time.Hour))
			s.Require().NoError(err)
			s.NotNil(cronInv)
		})
	}
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_ConcurrentStartLosesOnIdempotencyKey() {
	ctx := s.GetContext()
	sub := s.seedTrialingFixedFeeSubscription(decimal.NewFromInt(200))
	// A racing start whose session the pending-check could not see yet holds the deterministic key.
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		CustomerID:      sub.CustomerID,
		Action:          types.CheckoutActionWalletTopup,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		IdempotencyKey:  lo.ToPtr("trial_end:" + sub.ID),
		ExpiresAt:       time.Now().UTC().Add(20 * time.Minute),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}))
	modSvc := NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams)

	_, err := modSvc.Execute(ctx, sub.ID, s.trialEndCheckoutRequest())
	s.True(ierr.IsAlreadyExists(err), "duplicate trial-end checkout rejected by the idempotency key, got %v", err)

	drafts := lo.Filter(s.invoicesFor(sub.ID), func(i *invoice.Invoice, _ int) bool {
		return i.BillingReason == string(types.InvoiceBillingReasonSubscriptionTrialEnd) && i.Status == types.StatusPublished
	})
	s.Empty(drafts, "the losing start leaves no orphan draft")
	got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusTrialing, got.SubscriptionStatus)
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_CallerIdempotencyKeyRespected() {
	ctx := s.GetContext()
	sub := s.seedTrialingFixedFeeSubscription(decimal.NewFromInt(200))
	req := s.trialEndCheckoutRequest()
	req.Checkout.IdempotencyKey = lo.ToPtr("caller-key-" + sub.ID)

	resp, err := NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams).Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, resp.CheckoutSession.ID)
	s.Require().NoError(err)
	s.Equal("caller-key-"+sub.ID, lo.FromPtr(session.IdempotencyKey))
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_CompletionUsesStoredStart() {
	ctx := s.GetContext()
	sub, sessionID, _ := s.startTrialEndCheckout()
	session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, sessionID)
	s.Require().NoError(err)
	params := session.Configuration.ModifySubscriptionParams
	s.Require().NotNil(params.TrialEndAt, "execute records the conversion start on the session")

	// Shift the stored start so the test proves completion reads it rather than the invoice period.
	stored := params.TrialEndAt.Add(time.Minute)
	params.TrialEndAt = &stored
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Update(ctx, session))

	checkoutSvc := &checkoutSessionService{ServiceParams: s.service.(*subscriptionService).ServiceParams}
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{ProviderPaymentIntentID: "pi_trial_3"}))

	got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.True(got.CurrentPeriodStart.Equal(stored), "period starts at the stored start, got %s", got.CurrentPeriodStart)
	s.True(got.TrialEnd.Equal(stored), "trial end moves to the stored start")
	_, wantEnd, err := trialConversionPeriod(got, stored)
	s.Require().NoError(err)
	s.True(got.CurrentPeriodEnd.Equal(wantEnd))
}

func (s *SubscriptionServiceSuite) TestTrialConversionActivatesOnlyFromIncomplete() {
	tests := []struct {
		name   string
		status types.SubscriptionStatus
		want   types.SubscriptionStatus
	}{
		{"trialing waits for conversion", types.SubscriptionStatusTrialing, types.SubscriptionStatusTrialing},
		{"incomplete activates", types.SubscriptionStatusIncomplete, types.SubscriptionStatusActive},
		{"active is a no-op", types.SubscriptionStatusActive, types.SubscriptionStatusActive},
		{"cancelled is a no-op", types.SubscriptionStatusCancelled, types.SubscriptionStatusCancelled},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			sub := s.seedTrialingFixedFeeSubscription(decimal.NewFromInt(200))
			sub.SubscriptionStatus = tt.status
			s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

			s.Require().NoError(s.service.(*subscriptionService).completeTrialConversionToActive(ctx, sub))

			got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			s.Equal(tt.want, got.SubscriptionStatus)
		})
	}
}

func (s *SubscriptionServiceSuite) TestTrialConversionUsesFreshStoredRow() {
	tests := []struct {
		name         string
		stored       types.SubscriptionStatus
		callerStatus types.SubscriptionStatus
		want         types.SubscriptionStatus
	}{
		{"stale trialing copy, stored incomplete activates", types.SubscriptionStatusIncomplete, types.SubscriptionStatusTrialing, types.SubscriptionStatusActive},
		{"stale incomplete copy, stored active is a no-op", types.SubscriptionStatusActive, types.SubscriptionStatusIncomplete, types.SubscriptionStatusActive},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			ctx := s.GetContext()
			sub := s.seedTrialingFixedFeeSubscription(decimal.NewFromInt(200))
			sub.SubscriptionStatus = tt.stored
			s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

			stale := *sub
			stale.SubscriptionStatus = tt.callerStatus
			s.Require().NoError(s.service.(*subscriptionService).completeTrialConversionToActive(ctx, &stale))

			got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
			s.Require().NoError(err)
			s.Equal(tt.want, got.SubscriptionStatus)
		})
	}
}

func (s *SubscriptionServiceSuite) TestTrialEndCheckout_InvoicePaidWhileTrialingThenCheckoutCompletes() {
	ctx := s.GetContext()
	sub, sessionID, invoiceID := s.startTrialEndCheckout()
	subSvc := s.service.(*subscriptionService)
	session, err := s.GetStores().CheckoutSessionRepo.Get(ctx, sessionID)
	s.Require().NoError(err)
	trialEndAt := lo.FromPtr(session.Configuration.ModifySubscriptionParams.TrialEndAt)

	// The paid hook firing before checkout converts the trial must not activate it.
	inv, err := s.GetStores().InvoiceRepo.Get(ctx, invoiceID)
	s.Require().NoError(err)
	s.Require().NoError(subSvc.HandleSubscriptionActivatingInvoicePaid(ctx, inv))
	got, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusTrialing, got.SubscriptionStatus)

	checkoutSvc := &checkoutSessionService{ServiceParams: subSvc.ServiceParams}
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, sessionID, &types.CheckoutProviderResult{ProviderPaymentIntentID: "pi_trial_race"}))

	got, err = s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, got.SubscriptionStatus)
	s.True(got.CurrentPeriodStart.Equal(trialEndAt), "first paid period starts at the stored trial end, got %s", got.CurrentPeriodStart)
}
