package subscription

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/customer"
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
	_, err := subscriptionService.AddAddonToSubscription(ctx, sub.ID, &dto.AddAddonToSubscriptionRequest{
		AddonID:   addonID,
		StartDate: &addAddonNow,
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
