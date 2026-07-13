package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// SubscriptionTimezoneTestSuite tests that billing period boundaries are computed
// correctly in the customer's local timezone and stored as the right UTC instants.
type SubscriptionTimezoneTestSuite struct {
	testutil.BaseServiceTestSuite
	subscriptionSvc SubscriptionService
	customerSvc     CustomerService
	planID          string
	priceID         string
	meterID         string
	usagePriceID    string
	eventRepo       *testutil.InMemoryEventStore
}

func TestSubscriptionTimezoneTestSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionTimezoneTestSuite))
}

func (s *SubscriptionTimezoneTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.ClearStores()

	params := ServiceParams{
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
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		IntegrationFactory:         s.GetIntegrationFactory(),
		PlanPriceSyncRepo:          s.GetStores().PlanPriceSyncRepo,
	}

	s.subscriptionSvc = NewSubscriptionService(params)
	s.customerSvc = NewCustomerService(params)

	s.eventRepo = s.GetStores().EventRepo.(*testutil.InMemoryEventStore)

	s.setupSharedPlanAndPrice()
}

// TearDownTest is called after each test
func (s *SubscriptionTimezoneTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.BaseServiceTestSuite.ClearStores()
}

// setupSharedPlanAndPrice creates a plan with a single FLAT monthly price that all test cases share.
func (s *SubscriptionTimezoneTestSuite) setupSharedPlanAndPrice() {
	ctx := s.GetContext()

	testPlan := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Timezone Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, testPlan))
	s.planID = testPlan.ID

	testPrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromFloat(10.00),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           testPlan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, testPrice))
	s.priceID = testPrice.ID

	// Usage meter for counting "api_call" events.
	testMeter := &meter.Meter{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name:      "API Calls",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationCount,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().MeterRepo.CreateMeter(ctx, testMeter))
	s.meterID = testMeter.ID

	// Per-unit usage price linked to the meter, billed in arrear.
	testUsagePrice := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.Zero,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           testPlan.ID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_TIERED,
		TierMode:           types.BILLING_TIER_SLAB,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		MeterID:            testMeter.ID,
		Tiers: []price.PriceTier{
			{UpTo: nil, UnitAmount: decimal.NewFromFloat(1.0)},
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, testUsagePrice))
	s.usagePriceID = testUsagePrice.ID
}

// localMidnight returns the UTC instant that corresponds to 00:00:00 on the given date in tz.
func localMidnight(date time.Time, tz string) time.Time {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		panic("invalid timezone: " + tz)
	}
	local := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	return local.UTC()
}

func (s *SubscriptionTimezoneTestSuite) TestTimezoneAwareBillingPeriods() {
	// Fixed "wall-clock" start date: March 1, 2024 (local midnight in each tz)
	march1 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	testCases := []struct {
		name          string
		timezone      string
		expectedStart time.Time
		expectedEnd   time.Time
	}{
		{
			name:     "UTC customer",
			timezone: "UTC",
			// 2024-03-01 00:00 UTC  →  UTC instant is the same
			expectedStart: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			// 2024-04-01 00:00 UTC
			expectedEnd: time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "IST customer",
			timezone: "Asia/Kolkata",
			// 2024-03-01 00:00 IST (UTC+5:30) = 2024-02-29 18:30:00 UTC
			expectedStart: time.Date(2024, 2, 29, 18, 30, 0, 0, time.UTC),
			// 2024-04-01 00:00 IST (UTC+5:30) = 2024-03-31 18:30:00 UTC
			expectedEnd: time.Date(2024, 3, 31, 18, 30, 0, 0, time.UTC),
		},
		{
			name:     "EST customer",
			timezone: "America/New_York",
			// 2024-03-01 00:00 EST (UTC-5) = 2024-03-01 05:00:00 UTC
			expectedStart: time.Date(2024, 3, 1, 5, 0, 0, 0, time.UTC),
			// 2024-04-01 00:00 EDT (UTC-4, DST started Mar 10) = 2024-04-01 04:00:00 UTC
			expectedEnd: time.Date(2024, 4, 1, 4, 0, 0, 0, time.UTC),
		},
		{
			name:     "PST customer",
			timezone: "America/Los_Angeles",
			// 2024-03-01 00:00 PST (UTC-8) = 2024-03-01 08:00:00 UTC
			expectedStart: time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC),
			// 2024-04-01 00:00 PDT (UTC-7, DST started Mar 10) = 2024-04-01 07:00:00 UTC
			expectedEnd: time.Date(2024, 4, 1, 7, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()

			// Create a customer with the scenario timezone
			custResp, err := s.customerSvc.CreateCustomer(ctx, dto.CreateCustomerRequest{
				ExternalID: "ext-tz-" + tc.timezone,
				Name:       tc.name,
				Email:      "tz-test@example.com",
				Timezone:   tc.timezone,
			})
			s.Require().NoError(err)
			s.Require().NotNil(custResp)

			// Compute the UTC instant for local midnight on March 1, 2024 in this tz
			startUTC := localMidnight(march1, tc.timezone)

			// Create the subscription starting at the customer's local midnight
			resp, err := s.subscriptionSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
				CustomerID:         custResp.Customer.ID,
				PlanID:             s.planID,
				StartDate:          lo.ToPtr(startUTC),
				Currency:           "usd",
				BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
				BillingPeriodCount: 1,
				BillingCycle:       types.BillingCycleAnniversary,
			})
			s.Require().NoError(err)
			s.Require().NotNil(resp)

			// 1. Timezone is inherited from the customer
			s.Equal(tc.timezone, resp.Timezone,
				"Timezone should equal the customer's timezone")

			// 2. Period start matches the expected UTC instant
			s.True(resp.CurrentPeriodStart.UTC().Equal(tc.expectedStart),
				"CurrentPeriodStart mismatch: got %v, want %v",
				resp.CurrentPeriodStart.UTC(), tc.expectedStart)

			// 3. Period end matches the expected UTC instant
			s.True(resp.CurrentPeriodEnd.UTC().Equal(tc.expectedEnd),
				"CurrentPeriodEnd mismatch: got %v, want %v",
				resp.CurrentPeriodEnd.UTC(), tc.expectedEnd)

			// 4. Verify the period spans exactly 1 calendar month in the customer's local timezone.
			loc, err := time.LoadLocation(tc.timezone)
			s.Require().NoError(err)

			localStart := resp.CurrentPeriodStart.UTC().In(loc)
			localEnd := resp.CurrentPeriodEnd.UTC().In(loc)

			// Local start should be at midnight (00:00:00)
			s.Equal(0, localStart.Hour(), "period start hour in local tz should be 0")
			s.Equal(0, localStart.Minute(), "period start minute in local tz should be 0")
			s.Equal(0, localStart.Second(), "period start second in local tz should be 0")

			// Local end should also be at midnight (00:00:00)
			s.Equal(0, localEnd.Hour(), "period end hour in local tz should be 0")
			s.Equal(0, localEnd.Minute(), "period end minute in local tz should be 0")
			s.Equal(0, localEnd.Second(), "period end second in local tz should be 0")

			// End should be exactly one month after start in local time
			expectedLocalEnd := localStart.AddDate(0, 1, 0)
			s.Equal(expectedLocalEnd.Year(), localEnd.Year(),
				"period end year in local tz mismatch")
			s.Equal(expectedLocalEnd.Month(), localEnd.Month(),
				"period end month in local tz mismatch")
			s.Equal(expectedLocalEnd.Day(), localEnd.Day(),
				"period end day in local tz mismatch")
		})
	}
}

// TestTimezoneEventPeriodInclusion verifies that the same UTC event timestamp falls
// inside or outside a billing period depending on the customer's timezone.
//
// Scenario:
//
//	A boundary event fires at 2024-02-29T20:00:00Z (Feb 29, 20:00 UTC).
//
//	IST customer's March 1 period starts at 2024-02-29T18:30:00Z (IST midnight = UTC−5h30m).
//	The event is 1h30m inside that period → INCLUDED → count = 2.
//
//	UTC customer's March 1 period starts at 2024-03-01T00:00:00Z.
//	The event is 4 hours before that boundary → EXCLUDED → count = 1.
//
//	A mid-month event at 2024-03-15T12:00:00Z is clearly inside both periods → always INCLUDED.
func (s *SubscriptionTimezoneTestSuite) TestTimezoneEventPeriodInclusion() {
	ctx := s.GetContext()

	// --- Create customers ---
	utcCustResp, err := s.customerSvc.CreateCustomer(ctx, dto.CreateCustomerRequest{
		ExternalID: "ext-utc-event-test",
		Name:       "UTC Event Customer",
		Email:      "utc-event@example.com",
		Timezone:   "UTC",
	})
	s.Require().NoError(err)
	s.Require().NotNil(utcCustResp)

	istCustResp, err := s.customerSvc.CreateCustomer(ctx, dto.CreateCustomerRequest{
		ExternalID: "ext-ist-event-test",
		Name:       "IST Event Customer",
		Email:      "ist-event@example.com",
		Timezone:   "Asia/Kolkata",
	})
	s.Require().NoError(err)
	s.Require().NotNil(istCustResp)

	// --- Create subscriptions ---
	// UTC customer: March 1, 2024 00:00 UTC
	utcStart := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	utcSubResp, err := s.subscriptionSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         utcCustResp.ID,
		PlanID:             s.planID,
		StartDate:          lo.ToPtr(utcStart),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	})
	s.Require().NoError(err)
	s.Require().NotNil(utcSubResp)

	// IST customer: March 1, 2024 00:00 IST = 2024-02-29T18:30:00Z
	march1 := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	istStart := localMidnight(march1, "Asia/Kolkata")
	istSubResp, err := s.subscriptionSvc.CreateSubscription(ctx, dto.CreateSubscriptionRequest{
		CustomerID:         istCustResp.ID,
		PlanID:             s.planID,
		StartDate:          lo.ToPtr(istStart),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
	})
	s.Require().NoError(err)
	s.Require().NotNil(istSubResp)

	// Extract tenant/environment IDs from context to stamp events correctly.
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// --- Insert events ---
	// boundaryEvent: 2024-02-29T20:00:00Z
	//   - 1h30m after IST period start (18:30Z) → inside IST period
	//   - 4h before UTC period start (2024-03-01T00:00Z) → outside UTC period
	boundaryEventTS := time.Date(2024, 2, 29, 20, 0, 0, 0, time.UTC)

	// midMonthEvent: 2024-03-15T12:00:00Z — clearly inside both periods
	midMonthEventTS := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		externalCustomerID string
		customerID         string
	}{
		{utcCustResp.ExternalID, utcCustResp.ID},
		{istCustResp.ExternalID, istCustResp.ID},
	} {
		s.Require().NoError(s.eventRepo.InsertEvent(ctx, &events.Event{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT),
			TenantID:           tenantID,
			EnvironmentID:      environmentID,
			EventName:          "api_call",
			ExternalCustomerID: tc.externalCustomerID,
			CustomerID:         tc.customerID,
			Timestamp:          boundaryEventTS,
			Properties:         map[string]interface{}{},
		}))
		s.Require().NoError(s.eventRepo.InsertEvent(ctx, &events.Event{
			ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EVENT),
			TenantID:           tenantID,
			EnvironmentID:      environmentID,
			EventName:          "api_call",
			ExternalCustomerID: tc.externalCustomerID,
			CustomerID:         tc.customerID,
			Timestamp:          midMonthEventTS,
			Properties:         map[string]interface{}{},
		}))
	}

	// --- Assert UTC customer: only mid-month event is inside the period ---
	// UTC period starts 2024-03-01T00:00Z; boundary event at 20:00Z Feb 29 is before it.
	utcUsage, err := s.eventRepo.GetUsage(ctx, &events.UsageParams{
		ExternalCustomerID: utcCustResp.ExternalID,
		EventName:          "api_call",
		AggregationType:    types.AggregationCount,
		StartTime:          utcSubResp.CurrentPeriodStart,
		EndTime:            utcSubResp.CurrentPeriodEnd,
	})
	s.Require().NoError(err)
	s.Equal(float64(1), utcUsage.Value.InexactFloat64(),
		"UTC customer should see only the mid-March event (period start=%v, boundary event=%v)",
		utcSubResp.CurrentPeriodStart, boundaryEventTS)

	// --- Assert IST customer: both events are inside the period ---
	// IST period starts 2024-02-29T18:30Z; boundary event at 20:00Z is 1h30m inside it.
	istUsage, err := s.eventRepo.GetUsage(ctx, &events.UsageParams{
		ExternalCustomerID: istCustResp.ExternalID,
		EventName:          "api_call",
		AggregationType:    types.AggregationCount,
		StartTime:          istSubResp.CurrentPeriodStart,
		EndTime:            istSubResp.CurrentPeriodEnd,
	})
	s.Require().NoError(err)
	s.Equal(float64(2), istUsage.Value.InexactFloat64(),
		"IST customer should see both events (boundary event at %v is inside IST period starting %v)",
		boundaryEventTS, istSubResp.CurrentPeriodStart)
}
