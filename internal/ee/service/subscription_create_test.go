package service

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
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
// Draft-child cascade plumbing
//
// Inheritance is still rejected with checkout, so no create path produces draft children yet.
// These seed the state directly through the repo — the exact shape the gated parent create will
// materialize once a field is unlocked.
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

func (s *SubscriptionServiceSuite) TestDraftChildSubscriptions_ListsBothShapesAndOnlyDrafts() {
	ctx := s.GetContext()
	subService := s.service.(*subscriptionService)
	s.seedFixedPricePlan("plan_draft_children_lister", decimal.NewFromInt(50), 0)

	parent := s.seedDraftParent("plan_draft_children_lister")
	inheritedDraft := s.seedChildSubscription(parent, types.SubscriptionTypeInherited, types.SubscriptionStatusDraft)
	groupedDraft := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusDraft)
	activeChild := s.seedChildSubscription(parent, types.SubscriptionTypeGroupedInvoicing, types.SubscriptionStatusActive)

	children, err := subService.draftChildSubscriptions(ctx, parent.ID)
	s.Require().NoError(err)

	ids := lo.Map(children, func(c *subscription.Subscription, _ int) string { return c.ID })
	s.ElementsMatch([]string{inheritedDraft.ID, groupedDraft.ID}, ids,
		"both inheritance shapes must be listed, and only while draft")
	s.NotContains(ids, activeChild.ID, "an already-active child is not the cascade's business")
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
