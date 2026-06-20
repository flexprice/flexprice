package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
		PlanPriceSyncRepo:          s.GetStores().PlanPriceSyncRepo,
	}

	s.subscriptionSvc = NewSubscriptionService(params)
	s.customerSvc = NewCustomerService(params)

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
			s.Equal(tc.timezone, resp.CustomerTimezone,
				"CustomerTimezone should equal the customer's timezone")

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
