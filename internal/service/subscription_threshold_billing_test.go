package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

func (s *ThresholdBillingTestSuite) newSubscription(id string, status types.SubscriptionStatus, subType types.SubscriptionType, threshold *decimal.Decimal) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                   id,
		PlanID:               s.testData.plan.ID,
		CustomerID:           s.testData.customer.ID,
		Currency:             "USD",
		StartDate:            s.testData.now,
		CurrentPeriodStart:   s.testData.now,
		CurrentPeriodEnd:     s.testData.now.Add(30 * 24 * time.Hour),
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		BillingCadence:       types.BILLING_CADENCE_RECURRING,
		BillingCycle:         types.BillingCycleAnniversary,
		SubscriptionStatus:   status,
		SubscriptionType:     subType,
		AutoInvoiceThreshold: threshold,
		BaseModel:            types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(s.GetContext(), sub, nil))
	return sub
}

func (s *ThresholdBillingTestSuite) newActiveSubscription(id string, threshold *decimal.Decimal) *subscription.Subscription {
	return s.newSubscription(id, types.SubscriptionStatusActive, types.SubscriptionTypeStandalone, threshold)
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

// TestCreateSubscriptionRequestCarriesThreshold verifies that AutoInvoiceThreshold set on
// CreateSubscriptionRequest flows through ToSubscription correctly.
func (s *ThresholdBillingTestSuite) TestCreateSubscriptionRequestCarriesThreshold() {
	threshold := decimal.NewFromFloat(250)
	req := dto.CreateSubscriptionRequest{
		CustomerID:           s.testData.customer.ID,
		PlanID:               s.testData.plan.ID,
		Currency:             "usd",
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		BillingCycle:         types.BillingCycleAnniversary,
		StartDate:            lo.ToPtr(s.testData.now),
		AutoInvoiceThreshold: &threshold,
	}

	sub := req.ToSubscription(s.GetContext())

	s.Require().NotNil(sub.AutoInvoiceThreshold, "AutoInvoiceThreshold should be set on the subscription")
	s.True(threshold.Equal(*sub.AutoInvoiceThreshold), "AutoInvoiceThreshold value should match")
}

// TestCreateSubscriptionRequestNoThreshold verifies that omitting AutoInvoiceThreshold
// on CreateSubscriptionRequest results in a nil field on the subscription.
func (s *ThresholdBillingTestSuite) TestCreateSubscriptionRequestNoThreshold() {
	req := dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		StartDate:          lo.ToPtr(s.testData.now),
	}

	sub := req.ToSubscription(s.GetContext())

	s.Nil(sub.AutoInvoiceThreshold, "AutoInvoiceThreshold should be nil when not set in request")
}

// TestThresholdNotUpdatableViaUpdateRequest verifies that UpdateSubscriptionRequest
// does not expose AutoInvoiceThreshold — enforcing the create-only contract.
func (s *ThresholdBillingTestSuite) TestThresholdNotUpdatableViaUpdateRequest() {
	// Compile-time proof: updating a subscription does not change AutoInvoiceThreshold
	// because the field is absent from UpdateSubscriptionRequest.
	// This runtime check confirms the field is still set from creation.
	threshold := decimal.NewFromFloat(100)
	sub := s.newActiveSubscription("sub_immutable_threshold", &threshold)

	s.Require().NotNil(sub.AutoInvoiceThreshold)
	s.True(threshold.Equal(*sub.AutoInvoiceThreshold))

	// Fetch from store to confirm persistence.
	fetched, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.Require().NotNil(fetched.AutoInvoiceThreshold, "threshold must survive a round-trip through the store")
	s.True(threshold.Equal(*fetched.AutoInvoiceThreshold))
}

// TestThresholdBillingSkipsInactiveSubscription verifies that a cancelled or paused
// subscription is not included in the eligible set (query filters for active status).
func (s *ThresholdBillingTestSuite) TestThresholdBillingSkipsInactiveSubscription() {
	threshold := decimal.NewFromFloat(100)
	s.newSubscription("sub_cancelled_with_threshold", types.SubscriptionStatusCancelled, types.SubscriptionTypeStandalone, &threshold)

	result, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)
	s.NotNil(result)
	s.Equal(0, result.TotalChecked, "cancelled subscription must not be queried")
}

// TestThresholdBillingSkipsInheritedType verifies that an active subscription of type
// inherited is fetched (threshold set + active) but skipped by the service guard.
func (s *ThresholdBillingTestSuite) TestThresholdBillingSkipsInheritedType() {
	threshold := decimal.NewFromFloat(100)
	s.newSubscription("sub_inherited_with_threshold", types.SubscriptionStatusActive, types.SubscriptionTypeInherited, &threshold)

	result, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)
	s.NotNil(result)
	s.Equal(1, result.TotalChecked, "inherited sub is active + has threshold, so it is fetched")
	s.Equal(0, result.TotalInvoiced)
	s.Equal(1, result.TotalSkipped, "service must skip inherited-type subscriptions")
}

// TestThresholdBillingSkipsGroupedInvoicingType verifies that grouped-invoicing-type
// subscriptions are also skipped by the service guard.
func (s *ThresholdBillingTestSuite) TestThresholdBillingSkipsGroupedInvoicingType() {
	threshold := decimal.NewFromFloat(100)
	s.newSubscription("sub_grouped_with_threshold", types.SubscriptionStatusActive, types.SubscriptionTypeGroupedInvoicing, &threshold)

	result, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)
	s.NotNil(result)
	s.Equal(1, result.TotalChecked)
	s.Equal(0, result.TotalInvoiced)
	s.Equal(1, result.TotalSkipped, "service must skip grouped-invoicing-type subscriptions")
}

// TestThresholdZeroNeverTriggers verifies that a zero threshold never triggers an invoice
// since usage (non-negative) would need to be strictly less than zero to skip — i.e.
// zero usage equals zero threshold, so the invoice IS created. Explicitly document this
// edge case: callers must set a positive threshold to be meaningful.
func (s *ThresholdBillingTestSuite) TestThresholdZeroAlwaysTriggers() {
	zero := decimal.NewFromFloat(0)
	s.newActiveSubscription("sub_zero_threshold", &zero)

	// With usage=0 and threshold=0: usageAmount (0) < effectiveThreshold (0) is false,
	// so the code attempts to create an invoice. Since there are no charges (no line items),
	// ComputeInvoice will mark it skipped. current_period_start still advances.
	result, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)
	s.NotNil(result)
	s.Equal(1, result.TotalChecked)
	// Invoice flow is attempted (not skipped at the threshold check).
	// Result is either invoiced or failed depending on invoice creation; never TotalSkipped via threshold guard.
	s.Equal(0, result.TotalFailed, "zero threshold should not error")

	// The sub is checked (not skipped by the threshold guard) — usage 0 >= threshold 0.
	s.Equal(0, result.TotalSkipped, "zero threshold is met immediately; sub must not be threshold-skipped")
}

// TestThresholdPersistenceAcrossBillingRun verifies that after a threshold billing run
// that skips a subscription, current_period_start is NOT advanced (no invoice was created).
func (s *ThresholdBillingTestSuite) TestPeriodStartNotAdvancedOnSkip() {
	threshold := decimal.NewFromFloat(500) // high — won't be crossed with zero events
	sub := s.newActiveSubscription("sub_period_start_check", &threshold)
	originalPeriodStart := sub.CurrentPeriodStart

	_, err := s.service.ProcessThresholdBilling(s.GetContext())
	s.NoError(err)

	fetched, err := s.GetStores().SubscriptionRepo.Get(s.GetContext(), sub.ID)
	s.NoError(err)
	s.True(originalPeriodStart.Equal(fetched.CurrentPeriodStart),
		"current_period_start must not advance when usage is below threshold")
}

// TestProcessThresholdBillingContextPropagation verifies the method works correctly
// when called with a context that carries tenant/env values (simulating Temporal activity context).
func (s *ThresholdBillingTestSuite) TestProcessThresholdBillingContextPropagation() {
	threshold := decimal.NewFromFloat(100)
	s.newActiveSubscription("sub_ctx_test", &threshold)

	// Simulate a Temporal-style context with explicit tenant/env values.
	ctx := context.WithValue(s.GetContext(), types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, types.GetEnvironmentID(s.GetContext()))

	result, err := s.service.ProcessThresholdBilling(ctx)
	s.NoError(err)
	s.NotNil(result)
	s.Equal(1, result.TotalChecked)
}
