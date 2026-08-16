package service

// billing_grouped_invoicing_coupon_test.go — a grouped-invoicing parent merges its children's
// line items onto one invoice. Coupons the children carry have to make the same trip: before this,
// PrepareSubscriptionInvoiceRequest appended childReq.LineItems and dropped childReq.LineItemCoupons,
// so a discount attached to a child's line item was persisted as an association and then silently
// ignored at billing time.
//
// The setup is deliberately the hard case: both children run the same plan price, so price ID alone
// cannot tell their line items apart. Only subscription_line_item_id can, which is why the coupons
// must survive the merge carrying it.

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type BillingGroupedInvoicingCouponSuite struct {
	testutil.BaseServiceTestSuite
	svc BillingService

	periodStart time.Time
	periodEnd   time.Time
	price       *price.Price
	parent      *subscription.Subscription
}

func TestBillingGroupedInvoicingCoupon(t *testing.T) {
	suite.Run(t, new(BillingGroupedInvoicingCouponSuite))
}

func (s *BillingGroupedInvoicingCouponSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.svc = NewBillingService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
		PlanRepo:                 s.GetStores().PlanRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		EventRepo:                s.GetStores().EventRepo,
		MeterRepo:                s.GetStores().MeterRepo,
		CustomerRepo:             s.GetStores().CustomerRepo,
		InvoiceRepo:              s.GetStores().InvoiceRepo,
		EntitlementRepo:          s.GetStores().EntitlementRepo,
		EnvironmentRepo:          s.GetStores().EnvironmentRepo,
		FeatureRepo:              s.GetStores().FeatureRepo,
		TenantRepo:               s.GetStores().TenantRepo,
		UserRepo:                 s.GetStores().UserRepo,
		AuthRepo:                 s.GetStores().AuthRepo,
		WalletRepo:               s.GetStores().WalletRepo,
		PaymentRepo:              s.GetStores().PaymentRepo,
		CouponAssociationRepo:    s.GetStores().CouponAssociationRepo,
		CouponRepo:               s.GetStores().CouponRepo,
		CouponApplicationRepo:    s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:     s.GetStores().AddonAssociationRepo,
		TaxRateRepo:              s.GetStores().TaxRateRepo,
		TaxAssociationRepo:       s.GetStores().TaxAssociationRepo,
		TaxAppliedRepo:           s.GetStores().TaxAppliedRepo,
		SettingsRepo:             s.GetStores().SettingsRepo,
		EventPublisher:           s.GetPublisher(),
		WebhookPublisher:         s.GetWebhookPublisher(),
		ProrationCalculator:      s.GetCalculator(),
		AlertLogsRepo:            s.GetStores().AlertLogsRepo,
		MeterUsageRepo:           s.GetStores().MeterUsageRepo,
	})
	s.seedParentAndPlan()
}

func (s *BillingGroupedInvoicingCouponSuite) seedParentAndPlan() {
	ctx := s.GetContext()
	s.BaseServiceTestSuite.ClearStores()

	s.periodStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.periodEnd = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	pl := &plan.Plan{ID: "plan_gic", Name: "Grouped Plan", BaseModel: types.GetDefaultBaseModel(ctx)}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, pl))

	// One price shared by parent and both children — price ID cannot disambiguate their line items.
	s.price = &price.Price{
		ID:                 "price_gic",
		Amount:             decimal.NewFromInt(100),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           pl.ID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, s.price))

	s.parent = s.seedSubscription("parent", types.SubscriptionTypeParent, nil)
}

// seedSubscription creates a customer, a subscription and one fixed line item on the shared price.
func (s *BillingGroupedInvoicingCouponSuite) seedSubscription(
	name string,
	subType types.SubscriptionType,
	parentID *string,
) *subscription.Subscription {
	ctx := s.GetContext()

	cust := &customer.Customer{
		ID:         "cust_gic_" + name,
		ExternalID: "ext_gic_" + name,
		Name:       name,
		BaseModel:  types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CustomerRepo.Create(ctx, cust))

	sub := &subscription.Subscription{
		ID:                   "sub_gic_" + name,
		CustomerID:           cust.ID,
		PlanID:               "plan_gic",
		Currency:             "usd",
		SubscriptionStatus:   types.SubscriptionStatusActive,
		SubscriptionType:     subType,
		ParentSubscriptionID: parentID,
		CurrentPeriodStart:   s.periodStart,
		CurrentPeriodEnd:     s.periodEnd,
		BillingAnchor:        s.periodStart,
		StartDate:            s.periodStart,
		BillingCadence:       types.BILLING_CADENCE_RECURRING,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		EnvironmentID:        types.GetEnvironmentID(ctx),
		BaseModel:            types.GetDefaultBaseModel(ctx),
	}
	li := &subscription.SubscriptionLineItem{
		ID:                 "sli_gic_" + name,
		SubscriptionID:     sub.ID,
		CustomerID:         cust.ID,
		EntityID:           "plan_gic",
		EntityType:         types.SubscriptionLineItemEntityTypePlan,
		PlanDisplayName:    "Grouped Plan",
		PriceID:            s.price.ID,
		PriceType:          types.PRICE_TYPE_FIXED,
		DisplayName:        name + " seat",
		Quantity:           decimal.NewFromInt(1),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		StartDate:          s.periodStart,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.CreateWithLineItems(ctx, sub, []*subscription.SubscriptionLineItem{li}))
	sub.LineItems = []*subscription.SubscriptionLineItem{li}
	return sub
}

func (s *BillingGroupedInvoicingCouponSuite) seedCoupon(id string) *coupon.Coupon {
	ctx := s.GetContext()
	c := &coupon.Coupon{
		ID:            id,
		Name:          id,
		Type:          types.CouponTypePercentage,
		PercentageOff: lo.ToPtr(decimal.NewFromInt(100)),
		Cadence:       types.CouponCadenceForever,
		Currency:      "usd",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().CouponRepo.Create(ctx, c))
	return c
}

// associate attaches c to sub, scoped to lineItemID when non-empty (subscription-level otherwise).
func (s *BillingGroupedInvoicingCouponSuite) associate(id string, c *coupon.Coupon, subID, lineItemID string) {
	ctx := s.GetContext()
	assoc := &coupon_association.CouponAssociation{
		ID:             id,
		CouponID:       c.ID,
		SubscriptionID: subID,
		StartDate:      s.periodStart,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		Coupon:         c,
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	if lineItemID != "" {
		assoc.SubscriptionLineItemID = lo.ToPtr(lineItemID)
	}
	s.NoError(s.GetStores().CouponAssociationRepo.Create(ctx, assoc))
}

func (s *BillingGroupedInvoicingCouponSuite) prepareParentRequest() *dto.CreateInvoiceRequest {
	req, err := s.svc.PrepareSubscriptionInvoiceRequest(s.GetContext(), &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   s.parent,
		PeriodStart:    s.periodStart,
		PeriodEnd:      s.periodEnd,
		ReferencePoint: types.ReferencePointPeriodEnd,
	})
	s.NoError(err)
	s.Require().NotNil(req)
	return req
}

// TestChildLineItemCouponsMergeOntoParentInvoice is the core case: one coupon on the same shared
// price across two children must arrive as two distinct line-item coupons, each naming its own
// child's subscription line item. Keying by price alone would collapse them into one.
func (s *BillingGroupedInvoicingCouponSuite) TestChildLineItemCouponsMergeOntoParentInvoice() {
	childA := s.seedSubscription("child_a", types.SubscriptionTypeGroupedInvoicing, &s.parent.ID)
	childB := s.seedSubscription("child_b", types.SubscriptionTypeGroupedInvoicing, &s.parent.ID)

	c := s.seedCoupon("coupon_gic_shared")
	s.associate("assoc_gic_a", c, childA.ID, childA.LineItems[0].ID)
	s.associate("assoc_gic_b", c, childB.ID, childB.LineItems[0].ID)

	req := s.prepareParentRequest()

	s.Require().Len(req.LineItems, 3, "parent line item plus one per child")
	s.Require().Len(req.LineItemCoupons, 2, "both children's line-item coupons must survive the merge")

	targeted := lo.Map(req.LineItemCoupons, func(lic dto.InvoiceLineItemCoupon, _ int) string {
		s.Equal(c.ID, lic.CouponID)
		s.Equal(s.price.ID, lic.LineItemID, "price ID is kept as the legacy fallback key")
		return lo.FromPtr(lic.SubscriptionLineItemID)
	})
	s.ElementsMatch(
		[]string{childA.LineItems[0].ID, childB.LineItems[0].ID},
		targeted,
		"each coupon must name its own child's line item, not collapse onto one",
	)
}

// TestChildSubscriptionLevelCouponsAreNotMerged pins a known gap. An invoice-level coupon
// discounts the whole invoice, so merging a child's would reach the parent's and the siblings'
// charges; fanning it across the child's lines would multiply a fixed amount_off by the line
// count. Supporting it needs invoice coupons to name the lines they may touch — deliberately out
// of scope, so the coupon is left behind rather than applied to the wrong charges.
func (s *BillingGroupedInvoicingCouponSuite) TestChildSubscriptionLevelCouponsAreNotMerged() {
	child := s.seedSubscription("child_sub_level", types.SubscriptionTypeGroupedInvoicing, &s.parent.ID)

	c := s.seedCoupon("coupon_gic_sub_level")
	s.associate("assoc_gic_sub_level", c, child.ID, "")

	req := s.prepareParentRequest()

	s.Empty(req.InvoiceCoupons, "a child's subscription-level coupon must not discount the parent invoice")
	s.Empty(req.LineItemCoupons, "and must not be fanned out across the child's line items either")
}

// TestParentOwnCouponsSurviveChildMerge guards the merge against clobbering: appending the
// children's coupons must extend the parent's own selection, not replace it.
func (s *BillingGroupedInvoicingCouponSuite) TestParentOwnCouponsSurviveChildMerge() {
	child := s.seedSubscription("child_alongside", types.SubscriptionTypeGroupedInvoicing, &s.parent.ID)

	parentCoupon := s.seedCoupon("coupon_gic_parent")
	s.associate("assoc_gic_parent", parentCoupon, s.parent.ID, s.parent.LineItems[0].ID)

	childCoupon := s.seedCoupon("coupon_gic_child")
	s.associate("assoc_gic_child", childCoupon, child.ID, child.LineItems[0].ID)

	req := s.prepareParentRequest()

	// Each coupon stays on the line item its association named, even though parent and child bill
	// the same price here — a coupon does not spread across the group on price alone.
	s.Require().Len(req.LineItemCoupons, 2)
	targetByCoupon := lo.SliceToMap(req.LineItemCoupons, func(lic dto.InvoiceLineItemCoupon) (string, string) {
		return lic.CouponID, lo.FromPtr(lic.SubscriptionLineItemID)
	})
	s.Equal(s.parent.LineItems[0].ID, targetByCoupon[parentCoupon.ID], "parent's own line-item coupon must be retained")
	s.Equal(child.LineItems[0].ID, targetByCoupon[childCoupon.ID], "child's line-item coupon must be appended")
}
