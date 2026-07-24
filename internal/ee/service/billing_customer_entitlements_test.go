package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type CustomerEntitlementsTestSuite struct {
	testutil.BaseServiceTestSuite
	service BillingService
}

func TestCustomerEntitlements(t *testing.T) {
	suite.Run(t, new(CustomerEntitlementsTestSuite))
}

func (s *CustomerEntitlementsTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	stores := s.GetStores()
	s.service = NewBillingService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  stores.SubscriptionRepo,
		SubscriptionLineItemRepo: stores.SubscriptionLineItemRepo,
		PlanRepo:                 stores.PlanRepo,
		PriceRepo:                stores.PriceRepo,
		EventRepo:                stores.EventRepo,
		MeterRepo:                stores.MeterRepo,
		CustomerRepo:             stores.CustomerRepo,
		InvoiceRepo:              stores.InvoiceRepo,
		EntitlementRepo:          stores.EntitlementRepo,
		EntitlementGrantRepo:         stores.EntitlementGrantRepo,
		EnvironmentRepo:          stores.EnvironmentRepo,
		FeatureRepo:              stores.FeatureRepo,
		TenantRepo:               stores.TenantRepo,
		UserRepo:                 stores.UserRepo,
		AuthRepo:                 stores.AuthRepo,
		WalletRepo:               stores.WalletRepo,
		PaymentRepo:              stores.PaymentRepo,
		CouponAssociationRepo:    stores.CouponAssociationRepo,
		CouponRepo:               stores.CouponRepo,
		CouponApplicationRepo:    stores.CouponApplicationRepo,
		AddonAssociationRepo:     stores.AddonAssociationRepo,
		TaxRateRepo:              stores.TaxRateRepo,
		TaxAssociationRepo:       stores.TaxAssociationRepo,
		TaxAppliedRepo:           stores.TaxAppliedRepo,
		SettingsRepo:             stores.SettingsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		AlertLogsRepo:            stores.AlertLogsRepo,
	})
}

func (s *CustomerEntitlementsTestSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CustomerEntitlementsTestSuite) createCustomer(id string) *customer.Customer {
	c := &customer.Customer{
		ID:        id,
		Name:      "Test Customer",
		Email:     id + "@example.com",
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(s.GetContext(), c))
	return c
}

func (s *CustomerEntitlementsTestSuite) createPlan(id string) *plan.Plan {
	p := &plan.Plan{
		ID:        id,
		Name:      "Plan " + id,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().PlanRepo.Create(s.GetContext(), p))
	return p
}

func (s *CustomerEntitlementsTestSuite) createFeature(id string, featureType types.FeatureType) *feature.Feature {
	f := &feature.Feature{
		ID:        id,
		Name:      "Feature " + id,
		Type:      featureType,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), f))
	return f
}

func (s *CustomerEntitlementsTestSuite) createEntitlement(planID, featureID string, featureType types.FeatureType, usageLimit *int64) *entitlement.Entitlement {
	e := &entitlement.Entitlement{
		ID:          "ent_" + planID + "_" + featureID,
		EntityType:  types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		EntityID:    planID,
		FeatureID:   featureID,
		FeatureType: featureType,
		IsEnabled:   true,
		UsageLimit:  usageLimit,
		BaseModel:   types.GetDefaultBaseModel(s.GetContext()),
	}
	_, err := s.GetStores().EntitlementRepo.Create(s.GetContext(), e)
	s.NoError(err)
	return e
}

func (s *CustomerEntitlementsTestSuite) createSubscription(id, customerID, planID string) *subscription.Subscription {
	sub := &subscription.Subscription{
		ID:                 id,
		CustomerID:         customerID,
		PlanID:             planID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), sub))
	return sub
}

// TestNoSubscriptions returns empty features and subscriptions when customer has no subscriptions.
func (s *CustomerEntitlementsTestSuite) TestNoSubscriptions() {
	s.createCustomer("cust_no_sub")

	resp, err := s.service.GetCustomerEntitlements(s.GetContext(), "cust_no_sub", &dto.GetCustomerEntitlementsRequest{})
	s.NoError(err)
	s.Equal("cust_no_sub", resp.CustomerID)
	s.Empty(resp.Features)
	s.Empty(resp.Subscriptions)
}

// TestSubscriptionsReturnedInResponse verifies subscriptions are always populated.
func (s *CustomerEntitlementsTestSuite) TestSubscriptionsReturnedInResponse() {
	s.createCustomer("cust_sub")
	p := s.createPlan("plan_sub")
	f := s.createFeature("feat_bool", types.FeatureTypeBoolean)
	s.createEntitlement(p.ID, f.ID, types.FeatureTypeBoolean, nil)
	sub := s.createSubscription("sub_001", "cust_sub", p.ID)

	resp, err := s.service.GetCustomerEntitlements(s.GetContext(), "cust_sub", &dto.GetCustomerEntitlementsRequest{})
	s.NoError(err)
	s.Len(resp.Subscriptions, 1)
	s.Equal(sub.ID, resp.Subscriptions[0].ID)
}

// TestFeatureFilterReturnsOnlyMatchingFeatures verifies feature_ids filter works.
func (s *CustomerEntitlementsTestSuite) TestFeatureFilterReturnsOnlyMatchingFeatures() {
	s.createCustomer("cust_filter")
	p := s.createPlan("plan_filter")
	f1 := s.createFeature("feat_f1", types.FeatureTypeBoolean)
	f2 := s.createFeature("feat_f2", types.FeatureTypeBoolean)
	s.createEntitlement(p.ID, f1.ID, types.FeatureTypeBoolean, nil)
	s.createEntitlement(p.ID, f2.ID, types.FeatureTypeBoolean, nil)
	s.createSubscription("sub_filter", "cust_filter", p.ID)

	resp, err := s.service.GetCustomerEntitlements(s.GetContext(), "cust_filter", &dto.GetCustomerEntitlementsRequest{
		FeatureIDs: []string{f1.ID},
	})
	s.NoError(err)
	s.Len(resp.Features, 1)
	s.Equal(f1.ID, resp.Features[0].Feature.ID)
}

// TestMeteredEntitlementAggregation verifies metered entitlements are aggregated with usage limits.
func (s *CustomerEntitlementsTestSuite) TestMeteredEntitlementAggregation() {
	s.createCustomer("cust_metered")
	p := s.createPlan("plan_metered")
	f := s.createFeature("feat_metered", types.FeatureTypeMetered)
	limit := int64(1000)
	s.createEntitlement(p.ID, f.ID, types.FeatureTypeMetered, &limit)
	s.createSubscription("sub_metered", "cust_metered", p.ID)

	resp, err := s.service.GetCustomerEntitlements(s.GetContext(), "cust_metered", &dto.GetCustomerEntitlementsRequest{})
	s.NoError(err)
	s.Len(resp.Features, 1)
	s.Equal(f.ID, resp.Features[0].Feature.ID)
	s.NotNil(resp.Features[0].Entitlement.UsageLimit)
	s.Equal(limit, *resp.Features[0].Entitlement.UsageLimit)
}

// TestSubscriptionIDFilterLimitsScope verifies subscription_ids filter only includes matching subs.
func (s *CustomerEntitlementsTestSuite) TestSubscriptionIDFilterLimitsScope() {
	s.createCustomer("cust_subfilter")
	p1 := s.createPlan("plan_sf1")
	p2 := s.createPlan("plan_sf2")
	f1 := s.createFeature("feat_sf1", types.FeatureTypeBoolean)
	f2 := s.createFeature("feat_sf2", types.FeatureTypeBoolean)
	s.createEntitlement(p1.ID, f1.ID, types.FeatureTypeBoolean, nil)
	s.createEntitlement(p2.ID, f2.ID, types.FeatureTypeBoolean, nil)
	sub1 := s.createSubscription("sub_sf1", "cust_subfilter", p1.ID)
	s.createSubscription("sub_sf2", "cust_subfilter", p2.ID)

	resp, err := s.service.GetCustomerEntitlements(s.GetContext(), "cust_subfilter", &dto.GetCustomerEntitlementsRequest{
		SubscriptionIDs: []string{sub1.ID},
	})
	s.NoError(err)
	s.Len(resp.Subscriptions, 1)
	s.Equal(sub1.ID, resp.Subscriptions[0].ID)
	// Only features from sub1's plan should appear
	featureIDs := lo.Map(resp.Features, func(f *dto.AggregatedFeature, _ int) string { return f.Feature.ID })
	s.Contains(featureIDs, f1.ID)
	s.NotContains(featureIDs, f2.ID)
}

// TestInheritedSubscriptionsSkipped verifies inherited subscriptions are excluded.
func (s *CustomerEntitlementsTestSuite) TestInheritedSubscriptionsSkipped() {
	s.createCustomer("cust_inherited")
	p := s.createPlan("plan_inherited")
	f := s.createFeature("feat_inherited", types.FeatureTypeBoolean)
	s.createEntitlement(p.ID, f.ID, types.FeatureTypeBoolean, nil)
	s.createSubscription("sub_parent", "cust_inherited", p.ID)

	// Create an inherited subscription
	inherited := &subscription.Subscription{
		ID:                 "sub_inherited",
		CustomerID:         "cust_inherited",
		PlanID:             p.ID,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeInherited,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), inherited))

	resp, err := s.service.GetCustomerEntitlements(s.GetContext(), "cust_inherited", &dto.GetCustomerEntitlementsRequest{})
	s.NoError(err)
	for _, sub := range resp.Subscriptions {
		s.NotEqual(types.SubscriptionTypeInherited, sub.SubscriptionType)
	}
}
