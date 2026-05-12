package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type ThresholdBillingTestSuite struct {
	testutil.BaseServiceTestSuite
	service  SubscriptionService
	testData struct {
		customer *customer.Customer
		plan     *plan.Plan
		now      time.Time
	}
}

func TestThresholdBillingService(t *testing.T) {
	suite.Run(t, new(ThresholdBillingTestSuite))
}

func (s *ThresholdBillingTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *ThresholdBillingTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *ThresholdBillingTestSuite) setupService() {
	params := ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:              s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		PriceUnitRepo:                s.GetStores().PriceUnitRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		UserRepo:                     s.GetStores().UserRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		SecretRepo:                   s.GetStores().SecretRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CreditNoteRepo:               s.GetStores().CreditNoteRepo,
		CreditNoteLineItemRepo:       s.GetStores().CreditNoteLineItemRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		FeatureUsageRepo:             s.GetStores().FeatureUsageRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		WalletBalanceAlertPubSub:     types.WalletBalanceAlertPubSub{PubSub: testutil.NewInMemoryPubSub()},
	}
	s.service = NewSubscriptionService(params)
}

func (s *ThresholdBillingTestSuite) setupTestData() {
	s.testData.now = time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:        "cust_threshold_test",
		Name:      "Threshold Test Customer",
		Email:     "threshold@example.com",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        "plan_threshold_test",
		Name:      "Threshold Test Plan",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), s.testData.plan))
}

func (s *ThresholdBillingTestSuite) newActiveSubscription(id string, threshold *decimal.Decimal) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                  id,
		PlanID:              s.testData.plan.ID,
		CustomerID:          s.testData.customer.ID,
		Currency:            "USD",
		StartDate:           s.testData.now,
		CurrentPeriodStart:  s.testData.now,
		CurrentPeriodEnd:    s.testData.now.Add(30 * 24 * time.Hour),
		BillingPeriod:       types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:  1,
		BillingCadence:      types.BILLING_CADENCE_RECURRING,
		BillingCycle:        types.BillingCycleAnniversary,
		SubscriptionStatus:  types.SubscriptionStatusActive,
		SubscriptionType:    types.SubscriptionTypeStandalone,
		AutoInvoiceThreshold: threshold,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, nil))
	return sub
}

// TestNoEligibleSubscriptions verifies that when no subscriptions have a threshold,
// ProcessThresholdBilling returns zero counts.
func (s *ThresholdBillingTestSuite) TestNoEligibleSubscriptions() {
	// Sub without threshold — should not appear in eligible set.
	s.newActiveSubscription("sub_no_threshold", nil)

	result, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)
	s.NotNil(result)
	s.Equal(0, result.TotalChecked)
	s.Equal(0, result.TotalInvoiced)
	s.Equal(0, result.TotalSkipped)
	s.Equal(0, result.TotalFailed)
}

// TestBelowThreshold verifies that when a subscription has a threshold configured
// but current-period usage is below it (no events seeded), the subscription is skipped.
func (s *ThresholdBillingTestSuite) TestBelowThreshold() {
	threshold := decimal.NewFromFloat(100)
	s.newActiveSubscription("sub_with_threshold", &threshold)

	result, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)
	s.NotNil(result)
	s.Equal(1, result.TotalChecked, "subscription with threshold should be checked")
	s.Equal(0, result.TotalInvoiced, "usage=0 is below threshold=100, no invoice expected")
	s.Equal(1, result.TotalSkipped, "subscription should be skipped when usage < threshold")
	s.Equal(0, result.TotalFailed)
}

// TestMultipleSubscriptionsMixedThreshold verifies batch handling when only some
// subscriptions have a threshold configured.
func (s *ThresholdBillingTestSuite) TestMultipleSubscriptionsMixedThreshold() {
	threshold := decimal.NewFromFloat(50)
	s.newActiveSubscription("sub_a_with_threshold", &threshold)
	s.newActiveSubscription("sub_b_without_threshold", nil)
	s.newActiveSubscription("sub_c_with_threshold", &threshold)

	result, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)
	s.NotNil(result)
	s.Equal(2, result.TotalChecked, "only the two subs with thresholds should be checked")
	s.Equal(0, result.TotalInvoiced)
	s.Equal(2, result.TotalSkipped)
	s.Equal(0, result.TotalFailed)
}
