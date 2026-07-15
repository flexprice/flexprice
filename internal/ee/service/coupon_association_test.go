package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	coupon_domain "github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type CouponAssociationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service  CouponAssociationService
	testData struct {
		customer            *customer.Customer
		plan                *plan.Plan
		subscription        *subscription.Subscription
		price               *price.Price
		lineItem            *subscription.SubscriptionLineItem
		coupon              *coupon_domain.Coupon
		subLevelAssociation *coupon_association.CouponAssociation
		lineItemAssociation *coupon_association.CouponAssociation
	}
}

func TestCouponAssociationService(t *testing.T) {
	suite.Run(t, new(CouponAssociationServiceSuite))
}

func (s *CouponAssociationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.setupService()
	s.setupTestData()
}

func (s *CouponAssociationServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *CouponAssociationServiceSuite) setupService() {
	s.service = NewCouponAssociationService(s.newServiceParams())
}

func (s *CouponAssociationServiceSuite) newServiceParams() ServiceParams {
	return ServiceParams{
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
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	}
}

func (s *CouponAssociationServiceSuite) setupTestData() {
	ctx := s.GetContext()
	now := time.Now().UTC()

	s.testData.customer = &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		ExternalID: "ext_cust_coupon_assoc",
		Name:       "Coupon Association Customer",
		Email:      "coupon-assoc@example.com",
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, s.testData.customer))

	s.testData.plan = &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Coupon Association Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.testData.plan))

	s.testData.price = &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           s.testData.plan.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.testData.price))

	s.testData.subscription = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		PlanID:             s.testData.plan.ID,
		CustomerID:         s.testData.customer.ID,
		StartDate:          now.Add(-30 * 24 * time.Hour),
		CurrentPeriodStart: now.Add(-24 * time.Hour),
		CurrentPeriodEnd:   now.Add(6 * 24 * time.Hour),
		BillingAnchor:      now.Add(-30 * 24 * time.Hour),
		Currency:           "usd",
		BillingCycle:       types.BillingCycleAnniversary,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		SubscriptionStatus: types.SubscriptionStatusActive,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.testData.subscription))

	s.testData.lineItem = &subscription.SubscriptionLineItem{
		ID:              types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		SubscriptionID:  s.testData.subscription.ID,
		CustomerID:      s.testData.customer.ID,
		EntityID:        s.testData.plan.ID,
		EntityType:      types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName: s.testData.plan.Name,
		PriceID:         s.testData.price.ID,
		PriceType:       s.testData.price.Type,
		DisplayName:     "Coupon line item",
		Quantity:        decimal.NewFromInt(1),
		Currency:        s.testData.subscription.Currency,
		BillingPeriod:   s.testData.subscription.BillingPeriod,
		InvoiceCadence:  types.InvoiceCadenceAdvance,
		StartDate:       now.Add(-3 * 24 * time.Hour),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, s.testData.lineItem))

	pct := decimal.NewFromInt(10)
	s.testData.coupon = &coupon_domain.Coupon{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:          "Test Coupon",
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceOnce,
		PercentageOff: &pct,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.testData.coupon.Status = types.StatusPublished
	s.NoError(s.GetStores().CouponRepo.Create(ctx, s.testData.coupon))

	s.testData.subLevelAssociation = &coupon_association.CouponAssociation{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:       s.testData.coupon.ID,
		SubscriptionID: s.testData.subscription.ID,
		StartDate:      now.UTC(),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, s.testData.subLevelAssociation))

	s.testData.lineItemAssociation = &coupon_association.CouponAssociation{
		ID:                     types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON_ASSOCIATION),
		CouponID:               s.testData.coupon.ID,
		SubscriptionID:         s.testData.subscription.ID,
		SubscriptionLineItemID: lo.ToPtr(s.testData.lineItem.ID),
		StartDate:              now.UTC(),
		EnvironmentID:          types.GetEnvironmentID(ctx),
		BaseModel:              types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, s.testData.lineItemAssociation))
}

func (s *CouponAssociationServiceSuite) TestListCouponAssociations_InvalidExpand() {
	ctx := s.GetContext()
	filter := types.NewCouponAssociationFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}
	filter.Expand = lo.ToPtr("plan")

	_, err := s.service.ListCouponAssociations(ctx, filter)
	s.Error(err)
}

func (s *CouponAssociationServiceSuite) TestListCouponAssociations_ExpandCoupon() {
	ctx := s.GetContext()
	filter := types.NewCouponAssociationFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}
	filter.Expand = lo.ToPtr("coupon")

	resp, err := s.service.ListCouponAssociations(ctx, filter)
	s.NoError(err)
	s.Require().NotNil(resp)
	s.GreaterOrEqual(len(resp.Items), 2)

	for _, item := range resp.Items {
		s.Require().NotNil(item.Coupon)
		s.Equal(s.testData.coupon.ID, item.Coupon.ID)
		s.Equal(s.testData.coupon.Name, item.Coupon.Name)
	}
}

func (s *CouponAssociationServiceSuite) TestListCouponAssociations_ExpandSubscriptionLineItems() {
	ctx := s.GetContext()
	filter := types.NewCouponAssociationFilter()
	filter.SubscriptionIDs = []string{s.testData.subscription.ID}
	filter.Expand = lo.ToPtr("subscription_line_items")

	resp, err := s.service.ListCouponAssociations(ctx, filter)
	s.NoError(err)
	s.Require().NotNil(resp)

	var subLevelItem, lineItemLevelItem bool
	for _, item := range resp.Items {
		if item.ID == s.testData.subLevelAssociation.ID {
			subLevelItem = true
			s.Nil(item.SubscriptionLineItem)
		}
		if item.ID == s.testData.lineItemAssociation.ID {
			lineItemLevelItem = true
			s.Require().NotNil(item.SubscriptionLineItem)
			s.Equal(s.testData.lineItem.ID, item.SubscriptionLineItem.ID)
		}
	}
	s.True(subLevelItem)
	s.True(lineItemLevelItem)
}

// TestApplyCouponsToSubscription_RedemptionLimitReached is the service-layer
// regression test for the coupon redemption race-condition fix: it confirms
// that when a coupon has already hit max_redemptions, applying it again
// surfaces a validation-class error (4xx) rather than ErrInternal (which
// would incorrectly surface as a 500). Goes through ApplyCouponsToSubscription
// — the only caller of the unexported createCouponAssociation — since that's
// the real path every coupon application takes; there's no route that calls
// coupon-association creation directly.
func (s *CouponAssociationServiceSuite) TestApplyCouponsToSubscription_RedemptionLimitReached() {
	ctx := s.GetContext()

	maxRedemptions := 1
	pct := decimal.NewFromInt(15)
	limitedCoupon := &coupon_domain.Coupon{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:           "Limited Coupon",
		Type:           types.CouponTypePercentage,
		Cadence:        types.CouponCadenceOnce,
		PercentageOff:  &pct,
		MaxRedemptions: &maxRedemptions,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	limitedCoupon.Status = types.StatusPublished
	s.NoError(s.GetStores().CouponRepo.Create(ctx, limitedCoupon))

	firstCoupons := []dto.SubscriptionCouponRequest{
		{CouponID: limitedCoupon.ID, StartDate: time.Now().UTC()},
	}
	err := s.service.ApplyCouponsToSubscription(ctx, s.testData.subscription, firstCoupons)
	s.NoError(err, "first redemption should succeed under max_redemptions=1")

	secondCoupons := []dto.SubscriptionCouponRequest{
		{CouponID: limitedCoupon.ID, StartDate: time.Now().UTC()},
	}
	err = s.service.ApplyCouponsToSubscription(ctx, s.testData.subscription, secondCoupons)
	s.Require().Error(err, "second redemption must be rejected — coupon already at max_redemptions")
	s.True(ierr.IsValidation(err), "expected a validation-class error, not ErrInternal, got: %v", err)
}
