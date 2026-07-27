package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type ListSubscriptionsDueForDailyDraftComputeSuite struct {
	InvoiceServiceSuite // reuse InvoiceService wiring, customer/plan/subscription fixtures
}

func TestListSubscriptionsDueForDailyDraftCompute(t *testing.T) {
	suite.Run(t, new(ListSubscriptionsDueForDailyDraftComputeSuite))
}

func (s *ListSubscriptionsDueForDailyDraftComputeSuite) enableForTenant(tenantID, environmentID string) {
	setting := &settings.Setting{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SETTING),
		Key:           types.SettingKeyDraftInvoiceRecomputeConfig,
		Value:         map[string]interface{}{"enabled": true},
		EnvironmentID: environmentID,
		BaseModel:     types.BaseModel{TenantID: tenantID, Status: types.StatusPublished},
	}
	s.Require().NoError(s.GetStores().SettingsRepo.Create(s.GetContext(), setting))
}

func (s *ListSubscriptionsDueForDailyDraftComputeSuite) TestDisabledTenantYieldsNoSubscriptions() {
	ctx := s.GetContext()

	subs, err := s.service.ListSubscriptionsDueForDailyDraftCompute(ctx, nil)
	s.Require().NoError(err)
	s.Require().Empty(subs, "no tenant has draft_invoice_recompute_config enabled yet")
}

func (s *ListSubscriptionsDueForDailyDraftComputeSuite) TestEnabledTenantYieldsOnlyActivePublishedSubscriptions() {
	ctx := s.GetContext()
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	s.enableForTenant(tenantID, environmentID)

	// s.testData.subscription (from InvoiceServiceSuite.setupTestData) is active + published
	// with valid CurrentPeriodStart/End, so it must be returned.
	callCount := 0
	subs, err := s.service.ListSubscriptionsDueForDailyDraftCompute(ctx, func() { callCount++ })
	s.Require().NoError(err)

	found := false
	for _, sub := range subs {
		if sub.ID == s.testData.subscription.ID {
			found = true
		}
	}
	s.Require().True(found, "the enabled tenant's active subscription must be included")
	s.Require().GreaterOrEqual(callCount, 1, "onTenantEnvScanned must fire at least once per enabled tenant×env")
}

func (s *ListSubscriptionsDueForDailyDraftComputeSuite) TestCancelledSubscriptionIsExcluded() {
	ctx := s.GetContext()
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	s.enableForTenant(tenantID, environmentID)

	cancelled := &subscription.Subscription{
		ID:                 "sub_cancelled_test",
		CustomerID:         s.testData.customer.ID,
		PlanID:             s.testData.plan.ID,
		SubscriptionStatus: types.SubscriptionStatusCancelled,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		CurrentPeriodStart: s.testData.now,
		CurrentPeriodEnd:   s.testData.now.AddDate(0, 1, 0),
		StartDate:          s.testData.now,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, cancelled, nil))

	subs, err := s.service.ListSubscriptionsDueForDailyDraftCompute(ctx, nil)
	s.Require().NoError(err)
	for _, sub := range subs {
		s.Require().NotEqual(cancelled.ID, sub.ID, "cancelled subscriptions must be excluded")
	}
}

func (s *ListSubscriptionsDueForDailyDraftComputeSuite) TestNilCallbackDoesNotPanic() {
	ctx := s.GetContext()
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	s.enableForTenant(tenantID, environmentID)

	s.Require().NotPanics(func() {
		_, err := s.service.ListSubscriptionsDueForDailyDraftCompute(ctx, nil)
		s.Require().NoError(err)
	})
}
