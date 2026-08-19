package service

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// seedFixedPricePlan registers a plan with one FIXED ADVANCE price — the shape that actually
// produces a charge on the opening invoice, unlike the suite's default usage-only plan.
//
// trialDays > 0 puts the trial on the PRICE rather than the request, which is the case the DTO
// allowlist cannot see and the create path resolves later.
func (s *SubscriptionServiceSuite) seedFixedPricePlan(
	planID string,
	amount decimal.Decimal,
	trialDays int,
) *plan.Plan {
	ctx := s.GetContext()

	p := &plan.Plan{
		ID:        planID,
		Name:      "Fixed Price Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, p))

	s.NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 "price_" + planID,
		Amount:             amount,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		TrialPeriodDays:    trialDays,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))

	return p
}

// seedUsageOnlyPlan registers a plan whose only price is metered and billed in arrears, so its
// opening invoice has no charges at all and ComputeInvoice reports it skipped. The suite's default
// plan cannot stand in for this — it carries price_fixed_monthly.
func (s *SubscriptionServiceSuite) seedUsageOnlyPlan(planID string) *plan.Plan {
	ctx := s.GetContext()

	p := &plan.Plan{
		ID:        planID,
		Name:      "Usage Only Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, p))

	s.NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 "price_" + planID,
		Amount:             decimal.NewFromFloat(0.1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           planID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            s.testData.meters.apiCalls.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))

	return p
}

func (s *SubscriptionServiceSuite) checkoutCreateRequest(planID string) dto.CreateSubscriptionRequest {
	return dto.CreateSubscriptionRequest{
		CustomerID:    s.testData.customer.ID,
		PlanID:        planID,
		Currency:      "usd",
		BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingCycle:  types.BillingCycleAnniversary,
		Checkout:      s.razorpayCheckoutParams(),
	}
}

// invoicesForSubscription lists every invoice regardless of status. The statuses are spelled out
// because an unfiltered list silently drops SKIPPED invoices — which is exactly what a zero-charge
// gated create produces.
func (s *SubscriptionServiceSuite) invoicesForSubscription(subscriptionID string) []*invoice.Invoice {
	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = subscriptionID
	filter.InvoiceStatus = []types.InvoiceStatus{
		types.InvoiceStatusDraft,
		types.InvoiceStatusFinalized,
		types.InvoiceStatusVoided,
		types.InvoiceStatusSkipped,
	}
	invoices, err := s.GetStores().InvoiceRepo.List(s.GetContext(), filter)
	s.NoError(err)
	return invoices
}

// ─────────────────────────────────────────────
// Pay-later must be untouched
// ─────────────────────────────────────────────

func (s *SubscriptionServiceSuite) TestCreateSubscription_WithoutCheckoutIsUnchanged() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_paylater_create", decimal.NewFromInt(50), 0)

	req := s.checkoutCreateRequest("plan_paylater_create")
	req.Checkout = nil

	resp, err := s.service.CreateSubscription(ctx, req)
	s.Require().NoError(err)

	s.NotEqual(types.SubscriptionStatusDraft, resp.SubscriptionStatus,
		"a create without checkout must not be gated behind payment")
	s.Nil(resp.CheckoutSession, "no session may be opened when checkout is omitted")

	sessions, err := s.GetStores().CheckoutSessionRepo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
	})
	s.Require().NoError(err)
	s.Empty(sessions)
}

// ─────────────────────────────────────────────
// Nothing to collect → activate immediately
// ─────────────────────────────────────────────

// A usage-only plan bills in arrears, so the opening invoice has no charges at all and
// ComputeInvoice reports it skipped.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_UsageOnlyPlanActivatesImmediately() {
	ctx := s.GetContext()
	s.seedUsageOnlyPlan("plan_usage_only_checkout")

	resp, err := s.service.CreateSubscription(ctx, s.checkoutCreateRequest("plan_usage_only_checkout"))
	s.Require().NoError(err)

	s.Equal(types.SubscriptionStatusActive, resp.SubscriptionStatus,
		"nothing to collect means the subscription activates without a payment gate")
	s.Nil(resp.CheckoutSession, "no session may be opened when there is no charge")

	sessions, err := s.GetStores().CheckoutSessionRepo.List(ctx, &types.CheckoutSessionFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
	})
	s.Require().NoError(err)
	s.Empty(sessions, "a zero-charge create must create no checkout session at all")

	s.Nil(resp.LatestInvoice, "a skipped invoice is reported as no invoice, like CreateSubscriptionInvoice does")

	// The SKIPPED row is left alone. It is the period's placeholder, not litter: ComputeInvoice
	// re-opens SKIPPED rows to DRAFT once usage accrues, and the idempotency and period-uniqueness
	// lookups both treat SKIPPED as "reuse this one". Archiving it would strand the period.
	invoices := s.invoicesForSubscription(resp.ID)
	s.Require().Len(invoices, 1, "the checkout path prices a draft before deciding not to gate")
	s.Equal(types.InvoiceStatusSkipped, invoices[0].InvoiceStatus)
	s.Equal(types.StatusPublished, invoices[0].Status,
		"the skipped invoice must stay live so the period keeps its anchor")
}

// ─────────────────────────────────────────────
// Trials fall through instead of being gated
// ─────────────────────────────────────────────

func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_ExplicitTrialFallsThrough() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_trial_explicit", decimal.NewFromInt(50), 0)

	req := s.checkoutCreateRequest("plan_trial_explicit")
	req.TrialPeriodDays = lo.ToPtr(14)

	resp, err := s.service.CreateSubscription(ctx, req)
	s.Require().NoError(err)

	s.Equal(types.SubscriptionStatusTrialing, resp.SubscriptionStatus,
		"a trial raises no charge, so the create must not be gated")
	s.Nil(resp.CheckoutSession)
	s.NotNil(resp.TrialEnd, "the trial window must survive the checkout path")
}

// The regression that motivates resolving the trial before forcing draft:
// syncTrialingStateFromCreateRequest returns early for draft, so a plan-inherited trial would be
// flattened into a plain draft and later activated as ACTIVE — silently losing the trial.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_PlanInheritedTrialFallsThrough() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_trial_inherited", decimal.NewFromInt(50), 30)

	// Note: no trial_period_days on the request — it comes from the plan's price.
	resp, err := s.service.CreateSubscription(ctx, s.checkoutCreateRequest("plan_trial_inherited"))
	s.Require().NoError(err)

	s.Equal(types.SubscriptionStatusTrialing, resp.SubscriptionStatus,
		"a trial inherited from the plan's prices must not be flattened into a draft")
	s.Nil(resp.CheckoutSession)
	s.Require().NotNil(resp.TrialEnd)
	s.Equal(30, int(resp.TrialEnd.Sub(lo.FromPtr(resp.TrialStart)).Hours()/24))
}

// ─────────────────────────────────────────────
// Pay-first rollback
// ─────────────────────────────────────────────

// seedCollidingCheckoutSession occupies the partial-unique idempotency key so the next session
// Create for it fails. This is the only way to drive StartPayFirstCheckoutSession's failure paths
// without a live Razorpay connection — integration.Factory is a concrete struct with no seam for a
// fake checkout provider.
func (s *SubscriptionServiceSuite) seedCollidingCheckoutSession(idempKey string) {
	ctx := s.GetContext()
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      s.testData.customer.ID,
		Action:          types.CheckoutActionCreateSubscription,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{
				SubscriptionID: "subs_some_other_subscription",
			},
		}),
		IdempotencyKey: &idempKey,
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}))
}

// subscriptionForPlan finds the subscription a gated create wrote, including archived ones — the
// rollback path soft deletes, so the row is still there.
func (s *SubscriptionServiceSuite) subscriptionForPlan(planID string) *subscription.Subscription {
	subs, err := s.GetStores().SubscriptionRepo.List(s.GetContext(), types.NewNoLimitSubscriptionFilter())
	s.Require().NoError(err)

	for _, sub := range subs {
		if sub.PlanID == planID {
			return sub
		}
	}
	return nil
}

// draftInvoiceForPlan finds the invoice the gated create priced, by the plan its subscription used.
func (s *SubscriptionServiceSuite) draftInvoiceForPlan(planID string) *invoice.Invoice {
	sub := s.subscriptionForPlan(planID)
	if sub == nil {
		return nil
	}

	invoices := s.invoicesForSubscription(sub.ID)
	if len(invoices) == 0 {
		return nil
	}
	return invoices[0]
}

// The whole point of gating on POST /subscriptions rather than POST /checkout/sessions: the amount
// the customer is asked to pay includes the addons the slim legacy params could not express.
//
// Asserted on the archived draft, because the run cannot get past the provider call.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_AddonIsPricedIntoTheDraft() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_addon_priced", decimal.NewFromInt(50), 0)
	s.seedFixedPriceAddon("addon_priced_into_draft", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	idempKey := "create-subscription-addon-priced-idemp-key"
	s.seedCollidingCheckoutSession(idempKey)

	req := s.checkoutCreateRequest("plan_addon_priced")
	req.Checkout.IdempotencyKey = &idempKey
	req.Addons = []dto.AddAddonToSubscriptionRequest{{
		AddonID: "addon_priced_into_draft",
		Cadence: types.AddonCadenceRecurring,
	}}

	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err, "expected the seeded idempotency collision to fail session create")

	inv := s.draftInvoiceForPlan("plan_addon_priced")
	s.Require().NotNil(inv, "the gated create must have priced a draft invoice before opening a session")
	s.True(inv.AmountDue.Equal(decimal.NewFromInt(80)),
		"the locked amount must cover plan (50) + addon (30), got %s", inv.AmountDue)
}

// Drives StartPayFirstCheckoutSession's session-create failure by colliding on the partial-unique
// idempotency key, which is reachable without a live Razorpay connection.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_SessionCreateFailureArchivesDraft() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_payfirst_rollback", decimal.NewFromInt(50), 0)

	idempKey := "create-subscription-orphan-idemp-key"
	s.seedCollidingCheckoutSession(idempKey)

	req := s.checkoutCreateRequest("plan_payfirst_rollback")
	req.Checkout.IdempotencyKey = &idempKey

	resp, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)
	s.True(ierr.IsAlreadyExists(err), "expected the seeded idempotency collision, got %v", err)
	s.Nil(resp)

	// Asserted on status rather than absence: both repositories soft delete, so the rows stay
	// readable and "is it gone" would pass whether or not the rollback ran.
	sub := s.subscriptionForPlan("plan_payfirst_rollback")
	s.Require().NotNil(sub, "the subscription is written before the session, so it must exist")
	s.Equal(types.StatusArchived, sub.Status,
		"the draft subscription must be archived when pay-first setup fails")
	s.Equal(types.SubscriptionStatusDraft, sub.SubscriptionStatus,
		"the soft delete leaves subscription_status untouched, matching the ent repository")

	invoices := s.invoicesForSubscription(sub.ID)
	s.Require().NotEmpty(invoices, "the draft invoice is priced before the session is opened")
	for _, inv := range invoices {
		s.Equal(types.StatusDeleted, inv.Status,
			"the draft invoice must be archived alongside its subscription")
	}
}

// ─────────────────────────────────────────────
// Completion
// ─────────────────────────────────────────────

// seedPayFirstSubscriptionCheckout reproduces the state gateSubscriptionOnCheckout leaves behind —
// DRAFT subscription, DRAFT invoice, INITIATED payment, pending session — without going through the
// provider, which needs a live Razorpay connection.
func (s *SubscriptionServiceSuite) seedPayFirstSubscriptionCheckout(
	planID string,
) (*domainCheckout.CheckoutSession, *subscription.Subscription, *dto.InvoiceResponse) {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)

	// Created without checkout so the seeding stops short of the session, but as draft so the
	// opening invoice is skipped exactly as the gated path leaves it.
	req := s.checkoutCreateRequest(planID)
	req.Checkout = nil
	req.SubscriptionStatus = types.SubscriptionStatusDraft
	subResp, err := subService.CreateSubscription(ctx, req)
	s.Require().NoError(err)

	draft, skipped, err := buildCheckoutDraftInvoice(ctx, subService.ServiceParams, subResp)
	s.Require().NoError(err)
	s.Require().False(skipped, "the seeded plan must produce a real charge")
	s.Require().True(draft.AmountDue.GreaterThan(decimal.Zero))

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	payResp, err := checkoutSvc.createCheckoutPayment(ctx, &draft.Invoice, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      subResp.CustomerID,
		Action:          types.CheckoutActionCreateSubscription,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{SubscriptionID: subResp.ID},
		}),
		CheckoutInvoiceID: &draft.ID,
		CheckoutPaymentID: &payResp.ID,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))

	return session, subResp.Subscription, draft
}

func (s *SubscriptionServiceSuite) webhookEventNames() []types.WebhookEventName {
	publisher, ok := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	s.Require().True(ok, "expected the capturing webhook publisher")

	return lo.Map(publisher.Events(), func(e *types.WebhookEvent, _ int) types.WebhookEventName {
		return e.EventName
	})
}

func (s *SubscriptionServiceSuite) TestCompleteSubscriptionCheckout_ActivatesAndFinalizes() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_complete_activates", decimal.NewFromInt(50), 0)

	session, draftSub, draft := s.seedPayFirstSubscriptionCheckout("plan_complete_activates")
	invoicesBefore := len(s.invoicesForSubscription(draftSub.ID))

	publisher := s.GetWebhookPublisher().(*testutil.InMemoryWebhookPublisher)
	publisher.Reset()

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.CompleteCheckoutSession(ctx, session.ID, &types.CheckoutProviderResult{
		ProviderPaymentIntentID: "pay_subs_complete_001",
	}))

	activated, err := s.GetStores().SubscriptionRepo.Get(ctx, draftSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus,
		"payment must activate the draft subscription")

	finalized, err := NewInvoiceService(subService.ServiceParams).GetInvoice(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)

	payment, err := s.GetStores().PaymentRepo.Get(ctx, *session.CheckoutPaymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusSucceeded, payment.PaymentStatus)

	s.Equal(invoicesBefore, len(s.invoicesForSubscription(draftSub.ID)),
		"completion must finalize the existing draft, never raise a second charge")

	// The gap this work closes: a checkout-created subscription used to emit only
	// subscription.draft_created and never announce that it went live. It announces itself as
	// ACTIVATED rather than created — the create already emitted subscription.draft_created, and
	// this matches how every other draft-to-live transition reports itself.
	s.Contains(s.webhookEventNames(), types.WebhookEventSubscriptionActivated,
		"activation must publish subscription.activated")

	completed, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusCompleted, completed.CheckoutStatus)
}

// CompleteCheckoutSession runs the action BEFORE the atomic MarkCompleted claim, so two concurrent
// webhooks can both enter completion. The subscription's own status is the replay fingerprint.
func (s *SubscriptionServiceSuite) TestCompleteSubscriptionCheckout_ReplayIsIdempotent() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_complete_replay", decimal.NewFromInt(50), 0)

	session, draftSub, draft := s.seedPayFirstSubscriptionCheckout("plan_complete_replay")
	invoicesBefore := len(s.invoicesForSubscription(draftSub.ID))

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	providerResult := &types.CheckoutProviderResult{ProviderPaymentIntentID: "pay_subs_replay_001"}

	// Applied directly, twice, so the second call is not short-circuited by the session's own
	// terminal-status guard — this isolates the subscription-level replay guard.
	s.Require().NoError(checkoutSvc.completeSubscriptionCheckout(ctx, session, providerResult))
	s.Require().NoError(checkoutSvc.completeSubscriptionCheckout(ctx, session, providerResult))

	activated, err := s.GetStores().SubscriptionRepo.Get(ctx, draftSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus)

	s.Equal(invoicesBefore, len(s.invoicesForSubscription(draftSub.ID)),
		"a replay must not raise another charge")

	finalized, err := NewInvoiceService(subService.ServiceParams).GetInvoice(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, finalized.InvoiceStatus)
}

// A session opened by the legacy POST /checkout/sessions carries its ids in session.Result and has
// no subscription id in its configuration. Those must keep completing.
func (s *SubscriptionServiceSuite) TestCompleteSubscriptionCheckout_LegacyResultShape() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_complete_legacy", decimal.NewFromInt(50), 0)

	session, draftSub, draft := s.seedPayFirstSubscriptionCheckout("plan_complete_legacy")

	// Rewrite the session into the legacy shape: slim params, ids in Result.
	session.Configuration = domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
		CreateSubscriptionParams: &types.CreateSubscriptionParams{
			PlanID:        "plan_complete_legacy",
			Currency:      "usd",
			BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		},
	})
	session.Result = domainCheckout.ToJSONBCheckoutResult(&types.CheckoutResult{
		CreateSubscriptionResult: &types.CreateSubscriptionResult{
			SubscriptionID: draftSub.ID,
			InvoiceID:      draft.ID,
			PaymentID:      *session.CheckoutPaymentID,
		},
	})
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Update(ctx, session))

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.completeSubscriptionCheckout(ctx, session,
		&types.CheckoutProviderResult{ProviderPaymentIntentID: "pay_subs_legacy_001"}))

	activated, err := s.GetStores().SubscriptionRepo.Get(ctx, draftSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus)
}

// An expired or failed session must take its draft subscription with it, or the draft outlives the
// session as an orphan the customer can never pay for.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionCheckout_CleanupArchivesDraftSubscription() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_cleanup_draft", decimal.NewFromInt(50), 0)

	session, draftSub, draft := s.seedPayFirstSubscriptionCheckout("plan_cleanup_draft")

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.cleanupCheckoutSession(ctx, session, nil))

	archived, err := s.GetStores().SubscriptionRepo.Get(ctx, draftSub.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusArchived, archived.Status, "cleanup must archive the draft subscription")
	s.Equal(types.SubscriptionStatusDraft, archived.SubscriptionStatus,
		"the soft delete leaves subscription_status untouched")

	archivedDraft, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusDeleted, archivedDraft.Status, "cleanup must archive the draft invoice")

	cleaned, err := s.GetStores().CheckoutSessionRepo.Get(ctx, session.ID)
	s.Require().NoError(err)
	s.Equal(types.CheckoutStatusExpired, cleaned.CheckoutStatus)
}

// Ordering guard. The end state is identical whichever way round these run, so this
// asserts the thing that actually differs: when the invoice step fails, the payment must
// still be unsettled. Settling first would strand a SUCCEEDED payment against an
// unfinalized invoice — the gateway sync skips SUCCEEDED rows and the already-SUCCEEDED
// webhook guard blocks replays, so nothing could repair it.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionCheckout_InvoiceFailureLeavesPaymentUnsettled() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_finalize_guard", decimal.NewFromInt(50), 0)

	session, _, _ := s.seedPayFirstSubscriptionCheckout("plan_finalize_guard")
	paymentID := *session.CheckoutPaymentID

	before, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
	s.Require().NoError(err)
	s.Require().NotEqual(types.PaymentStatusSucceeded, before.PaymentStatus)

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	err = checkoutSvc.finalizeCheckoutInvoiceAndPayment(ctx, "inv_does_not_exist", paymentID,
		&types.CheckoutProviderResult{ProviderPaymentIntentID: "pay_never_collected"})
	s.Require().Error(err, "an unresolvable invoice must abort before the payment is touched")

	after, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
	s.Require().NoError(err)
	s.Equal(before.PaymentStatus, after.PaymentStatus,
		"the payment must not settle when the invoice step fails")
	s.Nil(after.GatewayPaymentID, "no transaction may be claimed by a failed finalize")

	attempts, err := s.GetStores().PaymentRepo.ListAttempts(ctx, paymentID)
	s.NoError(err)
	s.Empty(attempts, "a payment that never settled must not carry a succeeded attempt")
}

// Happy path: the invoice ends finalized AND reconciled, the payment settled, and the
// collecting transaction is recorded once in the attempt ledger.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionCheckout_FinalizeSettlesAndReconciles() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_finalize_order", decimal.NewFromInt(50), 0)

	session, _, draft := s.seedPayFirstSubscriptionCheckout("plan_finalize_order")
	paymentID := *session.CheckoutPaymentID

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.finalizeCheckoutInvoiceAndPayment(ctx, draft.ID, paymentID,
		&types.CheckoutProviderResult{ProviderPaymentIntentID: "pay_order_001"}))

	settled, err := s.GetStores().PaymentRepo.Get(ctx, paymentID)
	s.Require().NoError(err)
	s.Equal(types.PaymentStatusSucceeded, settled.PaymentStatus)
	s.Equal("pay_order_001", lo.FromPtr(settled.GatewayPaymentID))

	attempts, err := s.GetStores().PaymentRepo.ListAttempts(ctx, paymentID)
	s.Require().NoError(err)
	s.Require().Len(attempts, 1, "the settling charge must appear in the attempt ledger")
	s.Equal(types.PaymentStatusSucceeded, attempts[0].PaymentStatus)

	inv, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)
	s.Equal(types.InvoiceStatusFinalized, inv.InvoiceStatus)
	s.Equal(types.PaymentStatusSucceeded, inv.PaymentStatus, "the invoice must end reconciled")
}

// Re-delivery must be safe: the checkout reconcile passes a nil amount, which is absolute
// (AmountPaid = AmountDue) rather than additive, so replaying cannot overpay the invoice.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionCheckout_FinalizeIsReplaySafe() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_finalize_replay", decimal.NewFromInt(50), 0)

	session, _, draft := s.seedPayFirstSubscriptionCheckout("plan_finalize_replay")
	paymentID := *session.CheckoutPaymentID
	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	res := &types.CheckoutProviderResult{ProviderPaymentIntentID: "pay_replay_001"}

	s.Require().NoError(checkoutSvc.finalizeCheckoutInvoiceAndPayment(ctx, draft.ID, paymentID, res))
	first, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)

	s.Require().NoError(checkoutSvc.finalizeCheckoutInvoiceAndPayment(ctx, draft.ID, paymentID, res))
	again, err := s.GetStores().InvoiceRepo.Get(ctx, draft.ID)
	s.Require().NoError(err)

	s.True(first.AmountPaid.Equal(again.AmountPaid), "replay must not change amount_paid")
	s.Equal(types.PaymentStatusSucceeded, again.PaymentStatus, "replay must not flip to OVERPAID")
}

// A session that completed and only later expired must leave the live subscription alone.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionCheckout_CleanupSkipsActivatedSubscription() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_cleanup_active", decimal.NewFromInt(50), 0)

	session, draftSub, _ := s.seedPayFirstSubscriptionCheckout("plan_cleanup_active")

	draftSub.SubscriptionStatus = types.SubscriptionStatusActive
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, draftSub))

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.cleanupCheckoutSession(ctx, session, nil))

	live, err := s.GetStores().SubscriptionRepo.Get(ctx, draftSub.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusPublished, live.Status, "cleanup must not archive an activated subscription")
	s.Equal(types.SubscriptionStatusActive, live.SubscriptionStatus)
}

// The other gap this work closes: a checkout-created subscription's credit grants stayed pending
// until the cron happened to sweep them, because completion never processed them at activation.
func (s *SubscriptionServiceSuite) TestCompleteSubscriptionCheckout_ProcessesPendingCreditGrants() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_complete_grants", decimal.NewFromInt(50), 0)

	session, draftSub, _ := s.seedPayFirstSubscriptionCheckoutWithGrants("plan_complete_grants")

	grants, err := s.GetStores().CreditGrantRepo.List(ctx, &types.CreditGrantFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{draftSub.ID},
	})
	s.Require().NoError(err)
	s.Require().Len(grants, 1, "the draft create must have written the grant")

	before := s.firstApplicationFor(grants[0].ID)
	s.Require().NotNil(before)
	s.Require().NotEqual(types.ApplicationStatusApplied, before.ApplicationStatus,
		"the grant must still be unapplied while the subscription is a draft")

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	s.Require().NoError(checkoutSvc.completeSubscriptionCheckout(ctx, session,
		&types.CheckoutProviderResult{ProviderPaymentIntentID: "pay_subs_grants_001"}))

	after := s.firstApplicationFor(grants[0].ID)
	s.Require().NotNil(after)
	s.Equal(types.ApplicationStatusApplied, after.ApplicationStatus,
		"activation must process the pending credit grant instead of leaving it for the cron")
}

// seedPayFirstSubscriptionCheckoutWithGrants is seedPayFirstSubscriptionCheckout with a
// subscription-scoped credit grant attached at create.
func (s *SubscriptionServiceSuite) seedPayFirstSubscriptionCheckoutWithGrants(
	planID string,
) (*domainCheckout.CheckoutSession, *subscription.Subscription, *dto.InvoiceResponse) {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)

	req := s.checkoutCreateRequest(planID)
	req.Checkout = nil
	req.SubscriptionStatus = types.SubscriptionStatusDraft
	req.CreditGrants = []dto.CreateCreditGrantRequest{{
		Name:           "Gated Signup Credits",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(250),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
	}}

	subResp, err := subService.CreateSubscription(ctx, req)
	s.Require().NoError(err)

	draft, skipped, err := buildCheckoutDraftInvoice(ctx, subService.ServiceParams, subResp)
	s.Require().NoError(err)
	s.Require().False(skipped)

	checkoutSvc := &checkoutSessionService{ServiceParams: subService.ServiceParams}
	payResp, err := checkoutSvc.createCheckoutPayment(ctx, &draft.Invoice, types.CheckoutPaymentProviderRazorpay)
	s.Require().NoError(err)

	session := &domainCheckout.CheckoutSession{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CHECKOUT_SESSION),
		EnvironmentID:   types.GetEnvironmentID(ctx),
		CustomerID:      subResp.CustomerID,
		Action:          types.CheckoutActionCreateSubscription,
		CheckoutStatus:  types.CheckoutStatusPending,
		PaymentProvider: types.CheckoutPaymentProviderRazorpay,
		Configuration: domainCheckout.ToJSONBCheckoutConfiguration(types.CheckoutConfiguration{
			CreateSubscriptionParams: &types.CreateSubscriptionParams{SubscriptionID: subResp.ID},
		}),
		CheckoutInvoiceID: &draft.ID,
		CheckoutPaymentID: &payResp.ID,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CheckoutSessionRepo.Create(ctx, session))

	return session, subResp.Subscription, draft
}

// activateDraftSubscription is the step both zero-charge creates and paid webhooks share: flip
// DRAFT to ACTIVE and apply the credit grants the draft was holding back. Grants are the part worth
// pinning — they are written at create but stay pending while the subscription is a draft, so an
// activation that forgets them leaves the customer's credits stranded until the cron sweeps.
//
// NOTE: the sibling behaviour — a zero-due invoice being FINALIZED rather than archived, unlike the
// zero-subtotal case — is inlined in CreateSubscription and no longer has a callable seam. Producing
// it needs subtotal > 0 with amount_due == 0, i.e. a real discount, and the in-memory coupon
// machinery applies none at invoice time. That branch is covered by the Postman collection only.
func (s *SubscriptionServiceSuite) TestActivateDraftSubscription_AppliesHeldBackGrants() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_zero_due", decimal.NewFromInt(50), 0)

	req := s.checkoutCreateRequest("plan_zero_due")
	req.Checkout = nil
	req.SubscriptionStatus = types.SubscriptionStatusDraft
	req.CreditGrants = []dto.CreateCreditGrantRequest{{
		Name:           "Discounted Signup Credits",
		Scope:          types.CreditGrantScopeSubscription,
		Credits:        decimal.NewFromInt(100),
		Cadence:        types.CreditGrantCadenceOneTime,
		ExpirationType: types.CreditGrantExpiryTypeNever,
	}}
	subResp, err := subService.CreateSubscription(ctx, req)
	s.Require().NoError(err)
	s.Require().Equal(types.SubscriptionStatusDraft, subResp.SubscriptionStatus)

	grants, err := s.GetStores().CreditGrantRepo.List(ctx, &types.CreditGrantFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{subResp.ID},
	})
	s.Require().NoError(err)
	s.Require().Len(grants, 1)

	before := s.firstApplicationFor(grants[0].ID)
	s.Require().NotNil(before)
	s.Require().NotEqual(types.ApplicationStatusApplied, before.ApplicationStatus,
		"the grant must still be unapplied while the subscription is a draft")

	s.Require().NoError(subService.activateDraftSubscription(ctx, subResp.Subscription))

	activated, err := s.GetStores().SubscriptionRepo.Get(ctx, subResp.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus)

	after := s.firstApplicationFor(grants[0].ID)
	s.Require().NotNil(after)
	s.Equal(types.ApplicationStatusApplied, after.ApplicationStatus,
		"activating must apply the grants the draft held back")

	// Replay guard: a second call must be a harmless no-op, not a second application.
	s.Require().NoError(subService.activateDraftSubscription(ctx, activated))
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus)
}

// The subscription's collection_method governs FUTURE invoices; the checkout's governs only the
// gated payment. Pinned here because the fallback is easy to lose: without it a link-paid
// subscription takes Validate's own charge_automatically default and would try to auto-charge its
// first renewal against a mandate that was never created. The legacy create-session path stores
// send_invoice, and these two entry points must agree.
//
// Uses the usage-only plan so the create activates immediately — a charging plan would need the
// payment provider, which is unreachable in this suite.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_CollectionMethodInheritance() {
	tests := []struct {
		name     string
		mutate   func(*dto.CreateSubscriptionRequest)
		expected types.CollectionMethod
	}{
		{
			name:     "checkout with no provider config inherits send_invoice",
			mutate:   func(r *dto.CreateSubscriptionRequest) {},
			expected: types.CollectionMethodSendInvoice,
		},
		{
			name: "checkout config wins over the fallback",
			mutate: func(r *dto.CreateSubscriptionRequest) {
				r.Checkout.PaymentProviderConfig = &types.CheckoutPaymentProviderConfig{
					CollectionMethod: types.CollectionMethodChargeAutomatically,
				}
			},
			expected: types.CollectionMethodChargeAutomatically,
		},
		{
			name: "an explicit subscription-level choice beats both",
			mutate: func(r *dto.CreateSubscriptionRequest) {
				r.CollectionMethod = lo.ToPtr(types.CollectionMethodChargeAutomatically)
				r.Checkout.PaymentProviderConfig = &types.CheckoutPaymentProviderConfig{
					CollectionMethod: types.CollectionMethodSendInvoice,
				}
			},
			expected: types.CollectionMethodChargeAutomatically,
		},
		{
			name:     "without checkout the default is untouched",
			mutate:   func(r *dto.CreateSubscriptionRequest) { r.Checkout = nil },
			expected: types.CollectionMethodChargeAutomatically,
		},
	}

	for i, tt := range tests {
		s.Run(tt.name, func() {
			planID := fmt.Sprintf("plan_cm_inherit_%d", i)
			s.seedUsageOnlyPlan(planID)

			req := s.checkoutCreateRequest(planID)
			tt.mutate(&req)

			resp, err := s.service.CreateSubscription(s.GetContext(), req)
			s.Require().NoError(err)
			s.Equal(string(tt.expected), resp.CollectionMethod)
		})
	}
}

// ─────────────────────────────────────────────
// Child cascade plumbing
//
// A gated parent create materializes these through grouped_invoicing_children_to_create; the
// inherited shapes are still gated off. These seed the state directly through the repo so the
// cascade can be exercised against every child status independently of how it was produced.
// ─────────────────────────────────────────────

func (s *SubscriptionServiceSuite) seedChildSubscription(
	parent *subscription.Subscription,
	subType types.SubscriptionType,
	status types.SubscriptionStatus,
) *subscription.Subscription {
	ctx := s.GetContext()

	child := &subscription.Subscription{
		ID:                   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:           parent.CustomerID,
		PlanID:               parent.PlanID,
		Currency:             parent.Currency,
		SubscriptionStatus:   status,
		SubscriptionType:     subType,
		ParentSubscriptionID: lo.ToPtr(parent.ID),
		BillingAnchor:        parent.BillingAnchor,
		BillingCycle:         parent.BillingCycle,
		StartDate:            parent.StartDate,
		CurrentPeriodStart:   parent.CurrentPeriodStart,
		CurrentPeriodEnd:     parent.CurrentPeriodEnd,
		BillingCadence:       parent.BillingCadence,
		BillingPeriod:        parent.BillingPeriod,
		BillingPeriodCount:   parent.BillingPeriodCount,
		Version:              1,
		EnvironmentID:        parent.EnvironmentID,
		BaseModel:            types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, child))
	return child
}

// seedDraftParent creates a draft parent-typed subscription without going through the gated path.
func (s *SubscriptionServiceSuite) seedDraftParent(planID string) *subscription.Subscription {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)

	req := s.checkoutCreateRequest(planID)
	req.Checkout = nil
	req.SubscriptionStatus = types.SubscriptionStatusDraft
	resp, err := subService.CreateSubscription(ctx, req)
	s.Require().NoError(err)

	resp.Subscription.SubscriptionType = types.SubscriptionTypeParent
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, resp.Subscription))
	return resp.Subscription
}

func (s *SubscriptionServiceSuite) TestChildSubscriptions_DraftFilterVsEveryStatus() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_draft_children_lister", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_draft_children_lister")
	inheritedDraft := s.seedChildSubscription(parent, types.SubscriptionTypeInherited, types.SubscriptionStatusDraft)
	groupedDraft := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusDraft)
	trialingChild := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusTrialing)
	activeChild := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusActive)

	drafts, err := subService.childSubscriptions(ctx, parent.ID, types.SubscriptionStatusDraft)
	s.Require().NoError(err)
	draftIDs := lo.Map(drafts, func(c *subscription.Subscription, _ int) string { return c.ID })
	s.ElementsMatch([]string{inheritedDraft.ID, groupedDraft.ID}, draftIDs,
		"the activation cascade takes both inheritance shapes, and only while draft")

	all, err := subService.childSubscriptions(ctx, parent.ID)
	s.Require().NoError(err)
	allIDs := lo.Map(all, func(c *subscription.Subscription, _ int) string { return c.ID })
	s.ElementsMatch([]string{inheritedDraft.ID, groupedDraft.ID, trialingChild.ID, activeChild.ID}, allIDs,
		"cleanup takes every child, whatever status it resolved to")
}

func (s *SubscriptionServiceSuite) TestActivateDraftSubscription_CascadesToDraftChildren() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_activate_cascade", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_activate_cascade")
	inheritedChild := s.seedChildSubscription(parent, types.SubscriptionTypeInherited, types.SubscriptionStatusDraft)
	groupedChild := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusDraft)

	s.Require().NoError(subService.activateDraftSubscription(ctx, parent))

	for _, id := range []string{parent.ID, inheritedChild.ID, groupedChild.ID} {
		activated, err := s.GetStores().SubscriptionRepo.Get(ctx, id)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus,
			"payment activates the whole group, not just the parent (%s)", id)
	}
}

// A second webhook delivery must not error, and must not un-activate anything.
func (s *SubscriptionServiceSuite) TestActivateDraftSubscription_CascadeIsIdempotent() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_activate_cascade_replay", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_activate_cascade_replay")
	child := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusDraft)

	s.Require().NoError(subService.activateDraftSubscription(ctx, parent))
	s.Require().NoError(subService.activateDraftSubscription(ctx, parent))

	activated, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus)
}

func (s *SubscriptionServiceSuite) TestArchiveDraftCheckoutSubscription_CascadesToDraftChildren() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_archive_cascade", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_archive_cascade")
	inheritedChild := s.seedChildSubscription(parent, types.SubscriptionTypeInherited, types.SubscriptionStatusDraft)
	groupedChild := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusDraft)

	subService.archiveDraftCheckoutSubscription(ctx, parent.ID)

	for _, id := range []string{parent.ID, inheritedChild.ID, groupedChild.ID} {
		archived, err := s.GetStores().SubscriptionRepo.Get(ctx, id)
		s.Require().NoError(err)
		s.Equal(types.StatusArchived, archived.Status,
			"an abandoned checkout must not leave the group behind (%s)", id)
	}
}

// The cascade must stay inert for the standalone subscriptions every existing gated create produces.
func (s *SubscriptionServiceSuite) TestActivateDraftSubscription_StandaloneIsUnchanged() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_activate_standalone", decimal.NewFromInt(50), 0)

	_, draftSub, _ := s.seedPayFirstSubscriptionCheckout("plan_activate_standalone")
	s.Require().NoError(subService.activateDraftSubscription(ctx, draftSub))

	activated, err := s.GetStores().SubscriptionRepo.Get(ctx, draftSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus)
}

// ─────────────────────────────────────────────
// Inline grouped-invoicing children behind the gate
// ─────────────────────────────────────────────

func (s *SubscriptionServiceSuite) seedChildCustomer(externalID string) *customer.Customer {
	ctx := s.GetContext()
	c := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: externalID,
		Name:       externalID,
		Email:      externalID + "@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, c))
	return c
}

func (s *SubscriptionServiceSuite) groupedChildrenOf(parentID string) []*subscription.Subscription {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.ParentSubscriptionIDs = []string{parentID}
	filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeGroupedInvoicing}
	children, err := s.GetStores().SubscriptionRepo.List(s.GetContext(), filter)
	s.Require().NoError(err)
	return children
}

// The point of the whole phase: one payment link whose amount covers the parent AND every inline
// seat. Without the draft-aware merge filter the children are invisible and this reads 50.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_GroupedChildrenArePricedIntoTheDraft() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_grouped_parent", decimal.NewFromInt(50), 0)
	s.seedFixedPricePlan("plan_grouped_seat", decimal.NewFromInt(30), 0)
	s.seedChildCustomer("ext_seat_priced_1")
	s.seedChildCustomer("ext_seat_priced_2")

	idempKey := "create-subscription-grouped-priced-idemp-key"
	s.seedCollidingCheckoutSession(idempKey)

	req := s.checkoutCreateRequest("plan_grouped_parent")
	req.Checkout.IdempotencyKey = &idempKey
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		GroupedInvoicingChildrenToCreate: []dto.GroupedInvoicingChildRequest{
			{PlanID: "plan_grouped_seat", ExternalCustomerID: "ext_seat_priced_1"},
			{PlanID: "plan_grouped_seat", ExternalCustomerID: "ext_seat_priced_2"},
		},
	}

	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err, "expected the seeded idempotency collision to fail session create")

	inv := s.draftInvoiceForPlan("plan_grouped_parent")
	s.Require().NotNil(inv, "the gated create must have priced a draft invoice before opening a session")
	s.True(inv.AmountDue.Equal(decimal.NewFromInt(110)),
		"the locked amount must cover parent (50) + two seats (30 each), got %s", inv.AmountDue)
}

func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_GroupedChildrenAreCreatedDraft() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_grouped_draft_parent", decimal.NewFromInt(50), 0)
	s.seedFixedPricePlan("plan_grouped_draft_seat", decimal.NewFromInt(30), 0)
	s.seedChildCustomer("ext_seat_draft_1")

	idempKey := "create-subscription-grouped-draft-idemp-key"
	s.seedCollidingCheckoutSession(idempKey)

	req := s.checkoutCreateRequest("plan_grouped_draft_parent")
	req.Checkout.IdempotencyKey = &idempKey
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		GroupedInvoicingChildrenToCreate: []dto.GroupedInvoicingChildRequest{
			{PlanID: "plan_grouped_draft_seat", ExternalCustomerID: "ext_seat_draft_1"},
		},
	}

	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)

	parent := s.subscriptionForPlan("plan_grouped_draft_parent")
	s.Require().NotNil(parent)
	s.Equal(types.SubscriptionTypeParent, parent.SubscriptionType)

	children := s.groupedChildrenOf(parent.ID)
	s.Require().Len(children, 1)
	s.Equal(types.SubscriptionStatusDraft, children[0].SubscriptionStatus,
		"a seat must not go live before the bundle is paid for")
	s.Empty(s.invoicesForSubscription(children[0].ID),
		"the seat's charges belong on the parent's invoice, not its own")
}

// The status relaxation is keyed on the internal grouped_invoicing marker, so the wire-reachable
// inherited shape must still be refused a draft parent.
func (s *SubscriptionServiceSuite) TestPrepareInheritance_InheritedChildUnderDraftParentStillRejected() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_inherited_draft_parent", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_inherited_draft_parent")

	req := &dto.CreateSubscriptionRequest{
		Inheritance: &dto.SubscriptionInheritanceConfig{ParentSubscriptionID: parent.ID},
	}
	sub := &subscription.Subscription{CustomerID: s.testData.customer.ID}

	_, _, err := subService.prepareSubscriptionInheritanceForCreate(ctx, req, sub)
	s.Require().Error(err, "an inherited child must not attach to an unpaid draft parent")
	s.Contains(err.Error(), "parent subscription is not active")
}

// A seat whose plan carries a trial resolves to trialing and must not be flattened into a draft
// that would later activate as ACTIVE, destroying the trial.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_GroupedChildTrialFallsThrough() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_grouped_trial_parent", decimal.NewFromInt(50), 0)
	s.seedFixedPricePlan("plan_grouped_trial_seat", decimal.NewFromInt(30), 14)
	s.seedChildCustomer("ext_seat_trial_1")

	idempKey := "create-subscription-grouped-trial-idemp-key"
	s.seedCollidingCheckoutSession(idempKey)

	req := s.checkoutCreateRequest("plan_grouped_trial_parent")
	req.Checkout.IdempotencyKey = &idempKey
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		GroupedInvoicingChildrenToCreate: []dto.GroupedInvoicingChildRequest{
			{PlanID: "plan_grouped_trial_seat", ExternalCustomerID: "ext_seat_trial_1"},
		},
	}

	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)

	parent := s.subscriptionForPlan("plan_grouped_trial_parent")
	s.Require().NotNil(parent)
	children := s.groupedChildrenOf(parent.ID)
	s.Require().Len(children, 1)
	s.Equal(types.SubscriptionStatusTrialing, children[0].SubscriptionStatus,
		"a plan-inherited trial on a seat must survive the gate")
}

func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_SessionFailureArchivesGroupedChildren() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_grouped_archive_parent", decimal.NewFromInt(50), 0)
	s.seedFixedPricePlan("plan_grouped_archive_seat", decimal.NewFromInt(30), 0)
	s.seedChildCustomer("ext_seat_archive_1")

	idempKey := "create-subscription-grouped-archive-idemp-key"
	s.seedCollidingCheckoutSession(idempKey)

	req := s.checkoutCreateRequest("plan_grouped_archive_parent")
	req.Checkout.IdempotencyKey = &idempKey
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		GroupedInvoicingChildrenToCreate: []dto.GroupedInvoicingChildRequest{
			{PlanID: "plan_grouped_archive_seat", ExternalCustomerID: "ext_seat_archive_1"},
		},
	}

	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)

	parent := s.subscriptionForPlan("plan_grouped_archive_parent")
	s.Require().NotNil(parent)
	archivedParent, err := s.GetStores().SubscriptionRepo.Get(ctx, parent.ID)
	s.Require().NoError(err)
	s.Equal(types.StatusArchived, archivedParent.Status)

	children := s.groupedChildrenOf(parent.ID)
	s.Require().Len(children, 1, "the seat must exist for its archival to mean anything")
	for _, child := range children {
		archivedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
		s.Require().NoError(err)
		s.Equal(types.StatusArchived, archivedChild.Status,
			"an abandoned checkout must not leave a seat behind")
	}
}

// A seat whose plan carries a trial falls through the gate as trialing rather than draft, so a
// draft-only cleanup would leave it live — entitlements running for a bundle nobody paid for.
func (s *SubscriptionServiceSuite) TestCreateSubscriptionWithCheckout_SessionFailureArchivesTrialingSeat() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_trial_archive_parent", decimal.NewFromInt(50), 0)
	s.seedFixedPricePlan("plan_trial_archive_seat", decimal.NewFromInt(30), 14)
	s.seedChildCustomer("ext_seat_trial_archive")

	idempKey := "create-subscription-trial-archive-idemp-key"
	s.seedCollidingCheckoutSession(idempKey)

	req := s.checkoutCreateRequest("plan_trial_archive_parent")
	req.Checkout.IdempotencyKey = &idempKey
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		GroupedInvoicingChildrenToCreate: []dto.GroupedInvoicingChildRequest{
			{PlanID: "plan_trial_archive_seat", ExternalCustomerID: "ext_seat_trial_archive"},
		},
	}

	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)

	parent := s.subscriptionForPlan("plan_trial_archive_parent")
	s.Require().NotNil(parent)

	children := s.groupedChildrenOf(parent.ID)
	s.Require().Len(children, 1)
	s.Require().Equal(types.SubscriptionStatusTrialing, children[0].SubscriptionStatus,
		"fixture guard — this test is only meaningful while the seat falls through as trialing")

	archivedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, children[0].ID)
	s.Require().NoError(err)
	s.Equal(types.StatusArchived, archivedChild.Status,
		"a trialing seat must not outlive the abandoned bundle it belongs to")
}

// seedDraftGroupedChild builds a draft grouped_invoicing child that carries real line items, which
// a bare repo insert cannot do — the merge only ever sees a child through its line items.
func (s *SubscriptionServiceSuite) seedDraftGroupedChild(
	parent *subscription.Subscription,
	planID string,
) *subscription.Subscription {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan(planID, decimal.NewFromInt(30), 0)

	req := s.checkoutCreateRequest(planID)
	req.Checkout = nil
	req.SubscriptionStatus = types.SubscriptionStatusDraft
	req.StartDate = lo.ToPtr(parent.StartDate)
	resp, err := subService.CreateSubscription(ctx, req)
	s.Require().NoError(err)

	resp.Subscription.SubscriptionType = types.SubscriptionTypeGroupedInvoicing
	resp.Subscription.ParentSubscriptionID = lo.ToPtr(parent.ID)
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, resp.Subscription))
	return resp.Subscription
}

// The merge relaxation is scoped to a draft parent. A live parent's cycle invoice must never pick
// up a draft child left behind by a partial activation — that child was never paid for.
func (s *SubscriptionServiceSuite) TestPrepareSubscriptionInvoiceRequest_ActiveParentIgnoresDraftChildren() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_active_parent_merge", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_active_parent_merge")
	draftChild := s.seedDraftGroupedChild(parent, "plan_active_parent_merge_seat")

	billingSvc := NewBillingService(subService.ServiceParams)
	params := &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   parent,
		PeriodStart:    parent.CurrentPeriodStart,
		PeriodEnd:      parent.CurrentPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodStart,
	}

	gated, err := billingSvc.PrepareSubscriptionInvoiceRequest(ctx, params)
	s.Require().NoError(err)
	s.True(lo.SomeBy(gated.LineItems, func(li dto.CreateInvoiceLineItemRequest) bool {
		return lo.FromPtr(li.SubscriptionID) == draftChild.ID
	}), "a draft parent must price its draft children into the checkout amount")

	parent.SubscriptionStatus = types.SubscriptionStatusActive
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, parent))

	live, err := billingSvc.PrepareSubscriptionInvoiceRequest(ctx, params)
	s.Require().NoError(err)
	s.False(lo.SomeBy(live.LineItems, func(li dto.CreateInvoiceLineItemRequest) bool {
		return lo.FromPtr(li.SubscriptionID) == draftChild.ID
	}), "an active parent must not bill an unpaid draft child on its cycle invoice")
}

// A seat's first period was already covered by the parent's checkout invoice, so activating it
// must not raise one of its own.
func (s *SubscriptionServiceSuite) TestActivateDraftSubscription_GroupedChildGetsNoOwnInvoice() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_seat_no_own_invoice_parent", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_seat_no_own_invoice_parent")
	child := s.seedDraftGroupedChild(parent, "plan_seat_no_own_invoice_seat")

	s.Require().NoError(subService.activateDraftSubscription(ctx, parent))

	activated, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusActive, activated.SubscriptionStatus)
	s.Empty(s.invoicesForSubscription(child.ID),
		"the seat's charges settled on the parent's invoice; a second one would double-charge")
}

// An explicitly-draft parent is not a gated one: nothing would ever activate its seats, and once
// the parent goes live the merge excludes draft children, so the seats would never be billed.
// Only a checkout-gated create may link a child to a draft parent.
func (s *SubscriptionServiceSuite) TestCreateSubscription_ExplicitDraftParentRejectsGroupedChildren() {
	ctx := s.GetContext()
	s.seedFixedPricePlan("plan_explicit_draft_parent", decimal.NewFromInt(50), 0)
	s.seedFixedPricePlan("plan_explicit_draft_seat", decimal.NewFromInt(30), 0)
	s.seedChildCustomer("ext_seat_explicit_draft")

	req := s.checkoutCreateRequest("plan_explicit_draft_parent")
	req.Checkout = nil
	req.SubscriptionStatus = types.SubscriptionStatusDraft
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		GroupedInvoicingChildrenToCreate: []dto.GroupedInvoicingChildRequest{
			{PlanID: "plan_explicit_draft_seat", ExternalCustomerID: "ext_seat_explicit_draft"},
		},
	}

	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)
	s.Contains(err.Error(), "parent subscription is not active")
}
