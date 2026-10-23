package subscription

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/ee/service"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type BillingActivitiesSuite struct {
	testutil.BaseServiceTestSuite
	activities *BillingActivities
	testData   struct {
		customer *customer.Customer
		plan     *plan.Plan
		meter    *meter.Meter
		now      time.Time
	}
}

func TestBillingActivities(t *testing.T) {
	suite.Run(t, new(BillingActivitiesSuite))
}

func (s *BillingActivitiesSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()
	s.setupTestData()
}

func (s *BillingActivitiesSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.ClearStores()
}

func (s *BillingActivitiesSuite) setupTestData() {
	ctx := s.GetContext()
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_billing_activities_test",
		Name:       "Billing Activities Test Customer",
		Email:      "billing-activities-test@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:          types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:        "Billing Activities Test Plan",
		Description: "Test plan",
		BaseModel:   types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	s.testData.meter = &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "API Calls",
		EventName: "api_call_billing_activities",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.testData.meter))

	serviceParams := s.newServiceParams()
	subscriptionService := service.NewSubscriptionService(serviceParams)
	s.activities = NewBillingActivities(subscriptionService, serviceParams, s.GetLogger())
}

// newServiceParams builds a service.ServiceParams wired to this suite's test stores,
// mirroring the setupService() pattern used by internal/ee/service's test suites.
func (s *BillingActivitiesSuite) newServiceParams() service.ServiceParams {
	return service.ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EntitlementGrantRepo:       s.GetStores().EntitlementGrantRepo,
		MeterUsageCheckpointRepo:   s.GetStores().MeterUsageCheckpointRepo,
		AlertRepo:                  s.GetStores().AlertRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  s.GetStores().AddonRepo,
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		AlertLogsRepo:              s.GetStores().AlertLogsRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		MeterUsageRepo:             s.GetStores().MeterUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
		PlanPriceSyncRepo:          s.GetStores().PlanPriceSyncRepo,
		AddonPriceSyncRepo:         s.GetStores().AddonPriceSyncRepo,
	}
}

func (s *BillingActivitiesSuite) TestCheckCancellationActivity_TerminatesResourcesWhenCancellationFires() {
	ctx := types.SetEnvironmentID(s.GetContext(), "env_billing_activities_test")

	sub := &subscription.Subscription{
		ID:                 "sub_check_cancellation_activity",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          s.testData.now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: s.testData.now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   s.testData.now.Add(24 * time.Hour),
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, sub.LineItems))

	// Attach an addon so we can verify it gets terminated when the activity fires the cancellation.
	addonID := "addon_check_cancellation_activity"
	priceID := "price_addon_check_cancellation_activity"
	a := &addon.Addon{ID: addonID, LookupKey: addonID, Name: "Addon", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().AddonRepo.Create(ctx, a))
	p := &price.Price{
		ID:                 priceID,
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            s.testData.meter.ID,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))
	addAddonNow := time.Now().UTC()
	subscriptionService := service.NewSubscriptionService(s.newServiceParams())
	_, err := subscriptionService.AddAddonToSubscription(ctx, &dto.AddAddonRequest{
		SubscriptionID: sub.ID,
		AddAddonToSubscriptionRequest: dto.AddAddonToSubscriptionRequest{
			AddonID:   addonID,
			StartDate: &addAddonNow,
		},
	})
	s.NoError(err)

	// Schedule an end-of-period cancellation via the real service (creates the schedule row
	// and sets CancelAtPeriodEnd/CancelAt, but must NOT terminate anything yet — earlier fix).
	_, err = subscriptionService.CancelSubscription(ctx, sub.ID, &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeEndOfPeriod,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "test_check_cancellation_activity",
	})
	s.NoError(err)

	scheduledSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.NoError(err)
	s.Require().NotNil(scheduledSub.CancelAt)

	// Sanity check: still untouched immediately after scheduling.
	aaFilterBefore := types.NewNoLimitAddonAssociationFilter()
	aaFilterBefore.EntityIDs = []string{sub.ID}
	aaFilterBefore.EntityType = lo.ToPtr(types.AddonAssociationEntityTypeSubscription)
	associationsBefore, err := s.GetStores().AddonAssociationRepo.List(ctx, aaFilterBefore)
	s.NoError(err)
	s.Require().NotEmpty(associationsBefore)
	s.Equal(types.AddonStatusActive, associationsBefore[0].AddonStatus)

	// Drive the Temporal activity directly, as ProcessSubscriptionBillingWorkflow would,
	// with a period whose End is at/after CancelAt so it decides to cancel.
	input := subscriptionModels.CheckSubscriptionCancellationActivityInput{
		SubscriptionID: sub.ID,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		UserID:         types.GetUserID(ctx),
		Period: dto.Period{
			Start: scheduledSub.CurrentPeriodStart,
			End:   *scheduledSub.CancelAt,
		},
	}
	output, err := s.activities.CheckCancellationActivity(ctx, input)
	s.NoError(err)
	s.Require().NotNil(output)
	s.True(output.IsCancelled)

	firedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.NoError(err)
	s.Equal(types.SubscriptionStatusCancelled, firedSub.SubscriptionStatus)

	// Addon association must now be terminated — this is the assertion that fails without the fix.
	aaFilter := types.NewNoLimitAddonAssociationFilter()
	aaFilter.EntityIDs = []string{sub.ID}
	aaFilter.EntityType = lo.ToPtr(types.AddonAssociationEntityTypeSubscription)
	associations, err := s.GetStores().AddonAssociationRepo.List(ctx, aaFilter)
	s.NoError(err)
	s.Require().NotEmpty(associations)
	s.Equal(types.AddonStatusCancelled, associations[0].AddonStatus, "addon association must be terminated when CheckCancellationActivity fires the cancellation")
	s.NotNil(associations[0].EndDate)

	// Line items must now be terminated too.
	liFilter := types.NewNoLimitSubscriptionLineItemFilter()
	liFilter.SubscriptionIDs = []string{sub.ID}
	lineItems, err := s.GetStores().SubscriptionLineItemRepo.List(ctx, liFilter)
	s.NoError(err)
	for _, li := range lineItems {
		s.False(li.EndDate.IsZero(), "line item %s must be terminated when CheckCancellationActivity fires the cancellation", li.ID)
	}
}

// TestCreateDraftInvoicesActivity_SkipsPeriodWithFinalizedInvoice guards the retry-storm
// root cause: when a period already has a finalized (non-draft) invoice, the underlying
// service returns ErrAlreadyExists. The activity must treat that as an idempotent no-op
// and continue with the remaining periods, otherwise Temporal retries the activity forever
// because the state can never become "no invoice for this period" again.
func (s *BillingActivitiesSuite) TestCreateDraftInvoicesActivity_SkipsPeriodWithFinalizedInvoice() {
	ctx := types.SetEnvironmentID(s.GetContext(), "env_create_draft_invoices_skip")

	// Minute-aligned so the service's Truncate(time.Minute) doesn't cause a mismatch
	// when it later calls InvoiceRepo.GetForPeriod.
	p1Start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	p1End := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	p2Start := p1End
	p2End := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	sub := &subscription.Subscription{
		ID:                 "sub_create_draft_invoices_skip",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          p1Start,
		CurrentPeriodStart: p2Start,
		CurrentPeriodEnd:   p2End,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, sub.LineItems))

	// Pre-existing finalized invoice for P1 — this is the state that causes
	// CreateDraftInvoiceForSubscription to return ErrAlreadyExists.
	existing := &invoice.Invoice{
		ID:              "inv_finalized_p1",
		CustomerID:      s.testData.customer.ID,
		SubscriptionID:  lo.ToPtr(sub.ID),
		InvoiceType:     types.InvoiceTypeSubscription,
		InvoiceStatus:   types.InvoiceStatusFinalized,
		PaymentStatus:   types.PaymentStatusSucceeded,
		Currency:        "usd",
		AmountDue:       decimal.Zero,
		AmountPaid:      decimal.Zero,
		AmountRemaining: decimal.Zero,
		Subtotal:        decimal.Zero,
		Total:           decimal.Zero,
		PeriodStart:     &p1Start,
		PeriodEnd:       &p1End,
		BillingReason:   string(types.InvoiceBillingReasonSubscriptionCycle),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().InvoiceRepo.Create(ctx, existing))

	input := subscriptionModels.CreateInvoicesActivityInput{
		SubscriptionID: sub.ID,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		UserID:         types.GetUserID(ctx),
		Periods: []dto.Period{
			{Start: p1Start, End: p1End},
			{Start: p2Start, End: p2End},
		},
	}

	output, err := s.activities.CreateDraftInvoicesActivity(ctx, input)
	s.NoError(err, "activity must not fail when a period is already invoiced — Temporal would retry the permanent failure forever")
	s.Require().NotNil(output)
	s.Len(output.InvoiceIDs, 1, "P1 must be skipped (already invoiced), only P2 gets a new draft")
	s.NotContains(output.InvoiceIDs, existing.ID, "output must contain newly-created draft IDs, not the pre-existing finalized invoice")
}

func (s *BillingActivitiesSuite) TestUpdateCurrentPeriodActivity_AdvancesGroupedInvoicingChildren() {
	ctx := types.SetEnvironmentID(s.GetContext(), "env_update_period_grouped")

	oldStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	oldEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newStart := oldEnd
	newEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	parent := &subscription.Subscription{
		ID:                 "sub_update_period_parent",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeParent,
		StartDate:          oldStart,
		CurrentPeriodStart: oldStart,
		CurrentPeriodEnd:   oldEnd,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, parent, parent.LineItems))

	child := &subscription.Subscription{
		ID:                   "sub_update_period_grouped_child",
		CustomerID:           s.testData.customer.ID,
		PlanID:               s.testData.plan.ID,
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     types.SubscriptionTypeGroupedInvoicing,
		ParentSubscriptionID: lo.ToPtr(parent.ID),
		StartDate:            oldStart,
		CurrentPeriodStart:   oldStart,
		CurrentPeriodEnd:     oldEnd,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		Currency:             "usd",
		BaseModel:            types.GetDefaultBaseModel(ctx),
		LineItems:            []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, child, child.LineItems))

	inherited := &subscription.Subscription{
		ID:                   "sub_update_period_inherited_child",
		CustomerID:           s.testData.customer.ID,
		PlanID:               s.testData.plan.ID,
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     types.SubscriptionTypeInherited,
		ParentSubscriptionID: lo.ToPtr(parent.ID),
		StartDate:            oldStart,
		CurrentPeriodStart:   oldStart,
		CurrentPeriodEnd:     oldEnd,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		Currency:             "usd",
		BaseModel:            types.GetDefaultBaseModel(ctx),
		LineItems:            []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, inherited, inherited.LineItems))

	output, err := s.activities.UpdateCurrentPeriodActivity(ctx, subscriptionModels.UpdateSubscriptionPeriodActivityInput{
		SubscriptionID: parent.ID,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		UserID:         types.GetUserID(ctx),
		PeriodStart:    newStart,
		PeriodEnd:      newEnd,
	})
	s.NoError(err)
	s.Require().NotNil(output)
	s.True(output.Success)

	updatedParent, err := s.GetStores().SubscriptionRepo.Get(ctx, parent.ID)
	s.NoError(err)
	s.True(updatedParent.CurrentPeriodStart.Equal(newStart))
	s.True(updatedParent.CurrentPeriodEnd.Equal(newEnd))

	updatedChild, err := s.GetStores().SubscriptionRepo.Get(ctx, child.ID)
	s.NoError(err)
	s.True(updatedChild.CurrentPeriodStart.Equal(newStart), "grouped_invoicing child period must roll with the parent")
	s.True(updatedChild.CurrentPeriodEnd.Equal(newEnd), "grouped_invoicing child period must roll with the parent")

	updatedInherited, err := s.GetStores().SubscriptionRepo.Get(ctx, inherited.ID)
	s.NoError(err)
	s.True(updatedInherited.CurrentPeriodStart.Equal(oldStart), "inherited children are not cascaded by this activity")
	s.True(updatedInherited.CurrentPeriodEnd.Equal(oldEnd), "inherited children are not cascaded by this activity")
}

// pausedSubscription stores a subscription paused since its current period started, with its pause row.
func (s *BillingActivitiesSuite) pausedSubscription(ctx context.Context, id string, periodStart, periodEnd time.Time) (*subscription.Subscription, *subscription.SubscriptionPause) {
	pause := &subscription.SubscriptionPause{
		ID:                  "pause_" + id,
		SubscriptionID:      id,
		PauseStatus:         types.PauseStatusActive,
		PauseMode:           types.PauseModeImmediate,
		PauseStart:          periodStart,
		OriginalPeriodStart: periodStart,
		OriginalPeriodEnd:   periodEnd,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreatePause(ctx, pause))

	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusPaused,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		PauseStatus:        types.PauseStatusActive,
		ActivePauseID:      lo.ToPtr(pause.ID),
		StartDate:          periodStart,
		BillingAnchor:      periodStart,
		CurrentPeriodStart: periodStart,
		CurrentPeriodEnd:   periodEnd,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Currency:           "usd",
		Timezone:           "UTC",
		BaseModel:          types.GetDefaultBaseModel(ctx),
		LineItems:          []*subscription.SubscriptionLineItem{},
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, sub.LineItems))
	return sub, pause
}

// A period that passes while paused is recorded as a SKIPPED invoice and is not handed to the
// invoice workflow, since there is nothing to finalize or collect.
func (s *BillingActivitiesSuite) TestCreateDraftInvoicesActivity_RecordsPausedPeriodWithoutProcessingIt() {
	ctx := types.SetEnvironmentID(s.GetContext(), "env_create_draft_invoices_paused")
	p1Start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	p1End := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	sub, _ := s.pausedSubscription(ctx, "sub_create_draft_invoices_paused", p1Start, p1End)

	input := subscriptionModels.CreateInvoicesActivityInput{
		SubscriptionID: sub.ID,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		UserID:         types.GetUserID(ctx),
		Periods:        []dto.Period{{Start: p1Start, End: p1End}},
	}

	output, err := s.activities.CreateDraftInvoicesActivity(ctx, input)
	s.Require().NoError(err)
	s.Empty(output.InvoiceIDs, "the paused period is not sent to the invoice workflow")

	filter := types.NewNoLimitInvoiceFilter()
	filter.SubscriptionID = sub.ID
	filter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusSkipped}
	invoices, err := s.GetStores().InvoiceRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(invoices, 1)
	s.Equal(string(types.InvoiceBillingReasonSubscriptionPaused), invoices[0].BillingReason)
	s.True(invoices[0].PeriodStart.Equal(p1Start))
	s.True(invoices[0].PeriodEnd.Equal(p1End))
	s.True(invoices[0].Total.IsZero())
}

// A subscription that reaches its end date while paused is cancelled and its pause completed.
func (s *BillingActivitiesSuite) TestCheckCancellationActivity_EndDateWhilePausedCompletesThePause() {
	ctx := types.SetEnvironmentID(s.GetContext(), "env_check_cancellation_paused")
	periodStart := s.testData.now.Add(-20 * 24 * time.Hour)
	periodEnd := s.testData.now.Add(-24 * time.Hour)
	sub, pause := s.pausedSubscription(ctx, "sub_check_cancellation_paused", periodStart, periodEnd)
	sub.EndDate = lo.ToPtr(periodEnd)
	s.NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))

	output, err := s.activities.CheckCancellationActivity(ctx, subscriptionModels.CheckSubscriptionCancellationActivityInput{
		SubscriptionID: sub.ID,
		TenantID:       types.GetTenantID(ctx),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		UserID:         types.GetUserID(ctx),
		Period:         dto.Period{Start: periodStart, End: periodEnd},
	})
	s.Require().NoError(err)
	s.True(output.IsCancelled)

	cancelled, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionStatusCancelled, cancelled.SubscriptionStatus)
	s.Equal(types.PauseStatusNone, cancelled.PauseStatus)
	s.Nil(cancelled.ActivePauseID)

	completed, err := s.GetStores().SubscriptionRepo.GetPause(ctx, pause.ID)
	s.Require().NoError(err)
	s.Equal(types.PauseStatusCompleted, completed.PauseStatus)
}

func (s *BillingActivitiesSuite) TestUpdateCurrentPeriodActivity_QuietNeverCycleOpensAndEvaluatesOnce() {
	s.WithEnvironment("env_rollover_grants")
	ctx := s.GetContext()
	envID := types.GetEnvironmentID(ctx)
	now := time.Now().UTC()
	currentStart := now.Add(-time.Minute)
	cust := &customer.Customer{ID: "customer-rollover", ExternalID: "external-rollover", EnvironmentID: envID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, cust))
	p := &plan.Plan{ID: "plan-rollover", Name: "Storage", EnvironmentID: envID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, p))
	m := &meter.Meter{ID: "meter-rollover", EventName: "storage", ResetUsage: types.ResetUsageNever,
		Aggregation: meter.Aggregation{Type: types.AggregationSum}, EnvironmentID: envID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, m))
	f := &feature.Feature{ID: "feature-rollover", Type: types.FeatureTypeMetered, MeterID: m.ID, EnvironmentID: envID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().FeatureRepo.Create(ctx, f))
	ec := &entitlement.Entitlement{ID: "ec-rollover", EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN, EntityID: p.ID,
		FeatureID: f.ID, FeatureType: types.FeatureTypeMetered, IsEnabled: true, GrantMeasure: types.EntitlementGrantMeasureQuantity,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod, GrantQuota: lo.ToPtr(decimal.NewFromInt(100)),
		EnvironmentID: envID, BaseModel: types.GetDefaultBaseModel(ctx)}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, ec)
	s.Require().NoError(err)
	sub := &subscription.Subscription{ID: "sub-rollover", CustomerID: cust.ID, PlanID: p.ID,
		SubscriptionStatus: types.SubscriptionStatusActive, SubscriptionType: types.SubscriptionTypeStandalone,
		Currency: "usd", BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		StartDate: currentStart.AddDate(0, -1, 0), CurrentPeriodStart: currentStart.AddDate(0, -1, 0), CurrentPeriodEnd: currentStart,
		EnvironmentID: envID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	previous := entitlementgrant.NewEntitlementGrantBuilder(nil).
		WithID("grant-rollover-previous").WithEntitlementConfigID(ec.ID).WithCustomerID(cust.ID).
		WithSubscriptionID(sub.ID).WithScope(types.EntitlementGrantScopeFeature, f.ID).
		WithMeasure(types.EntitlementGrantMeasureQuantity).WithQuota(decimal.NewFromInt(100)).
		WithUsage(decimal.NewFromInt(120)).WithWindow(sub.StartDate, currentStart).
		WithGrantStatus(types.EntitlementGrantStatusExhausted).WithLastComputedAt(&currentStart).
		WithEnvironmentID(envID).WithBaseModel(types.GetDefaultBaseModel(ctx)).Build()
	_, err = s.GetStores().EntitlementGrantRepo.Create(ctx, previous)
	s.Require().NoError(err)
	s.Require().NoError(s.GetStores().MeterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{{
		Event: events.Event{ID: "event-previous-period", TenantID: types.GetTenantID(ctx), EnvironmentID: envID,
			ExternalCustomerID: cust.ExternalID, Timestamp: currentStart.Add(-time.Hour)},
		MeterID: m.ID, QtyTotal: decimal.NewFromInt(120),
	}}))
	params := s.newServiceParams()
	_, err = service.NewMeterUsageService(params).SaveUsageCheckpoint(ctx, service.CheckpointedUsageRequest{
		CustomerID: cust.ID, ExternalCustomerIDs: []string{cust.ExternalID}, MeterID: m.ID,
		AggregationType: types.AggregationSum, Until: currentStart,
	}, nil)
	s.Require().NoError(err)
	activities := NewBillingActivities(service.NewSubscriptionService(params), params, s.GetLogger())
	input := subscriptionModels.UpdateSubscriptionPeriodActivityInput{SubscriptionID: sub.ID,
		PeriodStart: currentStart, PeriodEnd: currentStart.AddDate(0, 1, 0),
		TenantID: types.GetTenantID(ctx), EnvironmentID: envID, UserID: types.GetUserID(ctx)}
	var firstID string
	for retry := 0; retry < 2; retry++ {
		_, err := activities.UpdateCurrentPeriodActivity(context.Background(), input)
		s.Require().NoError(err)
		rows, err := s.GetStores().EntitlementGrantRepo.List(ctx, types.NewNoLimitEntitlementGrantFilter().
			WithSubscriptionIDs(sub.ID).WithCycleOverlap(input.PeriodStart, input.PeriodEnd))
		s.Require().NoError(err)
		s.Require().Len(rows, 1)
		g := rows[0]
		if retry == 0 {
			firstID = g.ID
		}
		s.Equal(firstID, g.ID)
		s.True(g.ValidFrom.Equal(currentStart))
		s.True(g.ValidTo.Equal(input.PeriodEnd))
		s.Equal("100", g.Quota.String())
		s.Equal("120", g.Usage.String())
		s.Equal(types.EntitlementGrantStatusExhausted, g.GrantStatus)
		s.NotNil(g.LastComputedAt)
	}
}

func (s *BillingActivitiesSuite) TestRolloverSkipsSubscriptionsWithoutNeverMeterGrants() {
	ctx := s.GetContext()
	f := &feature.Feature{ID: "feature-normal-rollover", Type: types.FeatureTypeMetered,
		MeterID: s.testData.meter.ID, BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().FeatureRepo.Create(ctx, f))
	ec := &entitlement.Entitlement{ID: "ec-normal-rollover", EntityType: types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID: s.testData.plan.ID, FeatureID: f.ID, FeatureType: types.FeatureTypeMetered, IsEnabled: true,
		GrantMeasure:      types.EntitlementGrantMeasureQuantity,
		GrantDurationUnit: types.EntitlementGrantDurationUnitSubscriptionPeriod,
		GrantQuota:        lo.ToPtr(decimal.NewFromInt(100)), BaseModel: types.GetDefaultBaseModel(ctx)}
	_, err := s.GetStores().EntitlementRepo.Create(ctx, ec)
	s.Require().NoError(err)
	now := time.Now().UTC()
	sub := &subscription.Subscription{ID: "sub-normal-rollover", CustomerID: s.testData.customer.ID,
		PlanID: s.testData.plan.ID, SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType: types.SubscriptionTypeStandalone, StartDate: now.Add(-time.Minute),
		CurrentPeriodStart: now.Add(-time.Minute), CurrentPeriodEnd: now.AddDate(0, 1, 0),
		BillingPeriod: types.BILLING_PERIOD_MONTHLY, BillingPeriodCount: 1,
		BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	params := s.newServiceParams()
	activities := NewBillingActivities(service.NewSubscriptionService(params), params, s.GetLogger())
	activities.refreshEntitlementGrantsAfterRollover(ctx, sub)
	grants, err := s.GetStores().EntitlementGrantRepo.List(ctx,
		types.NewNoLimitEntitlementGrantFilter().WithSubscriptionIDs(sub.ID))
	s.Require().NoError(err)
	s.Empty(grants)
}

func (s *BillingActivitiesSuite) TestUpdateCurrentPeriodActivity_DoesNotRefreshCancelledSubscription() {
	ctx := s.GetContext()
	s.WithEnvironment("env_cancelled_rollover")
	ctx = s.GetContext()
	sub := &subscription.Subscription{ID: "sub-cancelled-rollover", CustomerID: "no-customer",
		SubscriptionStatus: types.SubscriptionStatusCancelled, EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	activities := NewBillingActivities(nil, s.newServiceParams(), s.GetLogger())
	_, err := activities.UpdateCurrentPeriodActivity(context.Background(), subscriptionModels.UpdateSubscriptionPeriodActivityInput{
		SubscriptionID: sub.ID, TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx),
		PeriodStart: time.Now().UTC(), PeriodEnd: time.Now().UTC().AddDate(0, 1, 0),
	})
	s.NoError(err)
}
