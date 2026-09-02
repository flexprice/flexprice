package service

// subscription_coupon_test.go — coupons attached at subscription creation, from the price_id the
// caller names through to the discount on the invoice.
//
// Two halves:
//
//   - Resolution: which line items a price_id resolves to, asserted on the coupon associations
//     written at creation. The map used to be built from plan line items before addons or price
//     overrides existed, so an addon price matched nothing — and a miss was silently downgraded to
//     a subscription-level coupon, discounting every charge instead of the one line named.
//
//   - Discount matrix: where the discount actually lands, asserted on a computed invoice. The
//     hard case throughout is a price shared by several line items — one addon attached twice, or
//     grouped-invoicing children on the same plan — where price_id alone cannot tell them apart.

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const (
	couponTestAddonID      = "addon_coupon_shared"
	couponTestAddonPriceID = "price_addon_coupon_shared"
	couponTestChildPlanFee = 30
)

// ─── fixtures ──────────────────────────────────────────────────────────────────────────────────

// seedSharedAddon publishes an addon with one recurring FIXED price. Attaching it more than once,
// or to several subscriptions in a group, puts multiple line items behind this single price.
func (s *SubscriptionServiceSuite) seedSharedAddon() {
	ctx := s.GetContext()

	s.Require().NoError(s.GetStores().AddonRepo.Create(ctx, &addon.Addon{
		ID:        couponTestAddonID,
		LookupKey: couponTestAddonID,
		Name:      "Shared Addon",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}))

	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 couponTestAddonPriceID,
		Amount:             decimal.NewFromInt(15),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           couponTestAddonID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))
}

// seedCouponPercentOff publishes a percentage coupon. maxRedemptions <= 0 leaves it unlimited;
// every association consumes one, so a coupon shared across N subscriptions needs N.
func (s *SubscriptionServiceSuite) seedCouponPercentOff(couponID string, percentOff, maxRedemptions int) *coupon.Coupon {
	ctx := s.GetContext()
	c := &coupon.Coupon{
		ID:            couponID,
		Name:          couponID,
		Type:          types.CouponTypePercentage,
		Cadence:       types.CouponCadenceForever,
		PercentageOff: lo.ToPtr(decimal.NewFromInt(int64(percentOff))),
		CouponCode:    lo.ToPtr(couponID),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
	if maxRedemptions > 0 {
		c.MaxRedemptions = lo.ToPtr(maxRedemptions)
	}
	c.Status = types.StatusPublished
	s.Require().NoError(s.GetStores().CouponRepo.Create(ctx, c))
	return c
}

func (s *SubscriptionServiceSuite) planPriceIDOf(planID string) string {
	prices, err := s.GetStores().PriceRepo.List(s.GetContext(), &types.PriceFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  lo.ToPtr(types.PRICE_ENTITY_TYPE_PLAN),
		EntityIDs:   []string{planID},
	})
	s.Require().NoError(err)
	s.Require().NotEmpty(prices)
	return prices[0].ID
}

func (s *SubscriptionServiceSuite) associationsFor(subscriptionID, couponID string) []*coupon_association.CouponAssociation {
	assocs, err := s.GetStores().CouponAssociationRepo.List(s.GetContext(), &types.CouponAssociationFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{subscriptionID},
		CouponIDs:       []string{couponID},
	})
	s.Require().NoError(err)
	return assocs
}

// createStandaloneSub creates a plain subscription on plan, with optional addon attachments.
func (s *SubscriptionServiceSuite) createStandaloneSub(
	planID string,
	addonCount int,
	coupons []dto.SubscriptionCouponInput,
) (*dto.SubscriptionResponse, error) {
	addons := make([]dto.AddAddonToSubscriptionRequest, 0, addonCount)
	for i := 0; i < addonCount; i++ {
		addons = append(addons, dto.AddAddonToSubscriptionRequest{AddonID: couponTestAddonID})
	}

	return s.service.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             planID,
		StartDate:          lo.ToPtr(s.testData.now),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		SubscriptionCreationConfig: dto.SubscriptionCreationConfig{
			Addons:              addons,
			SubscriptionCoupons: coupons,
		},
	})
}

// couponTestGroup is the hierarchy the group cases share: a parent on the seat plan carrying the
// addon twice, and two children on a different plan carrying it once each. Every addon line item
// across all three subscriptions sits behind one price.
type couponTestGroup struct {
	parentID string
	childIDs []string
}

func (s *SubscriptionServiceSuite) createCouponTestGroup(
	suffix string,
	parentCoupons []dto.SubscriptionCouponInput,
	childCoupons []dto.SubscriptionCouponInput,
) *couponTestGroup {
	seatPlan := s.setupSeatFeePlan()
	childPlan := s.seedFixedPricePlan("plan_coupon_child_"+suffix, decimal.NewFromInt(couponTestChildPlanFee), 0)

	ws1 := s.seedChildCustomer("ext_coupon_ws1_" + suffix)
	ws2 := s.seedChildCustomer("ext_coupon_ws2_" + suffix)

	childReq := func(externalID string) dto.GroupedInvoicingChildRequest {
		return dto.GroupedInvoicingChildRequest{
			PlanID:             childPlan.ID,
			ExternalCustomerID: externalID,
			SubscriptionCreationConfig: dto.SubscriptionCreationConfig{
				Addons:              []dto.AddAddonToSubscriptionRequest{{AddonID: couponTestAddonID}},
				SubscriptionCoupons: childCoupons,
			},
		}
	}

	resp, err := s.service.CreateSubscription(s.GetContext(), dto.CreateSubscriptionRequest{
		CustomerID:         s.testData.customer.ID,
		PlanID:             seatPlan.ID,
		StartDate:          lo.ToPtr(s.testData.now),
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		CollectionMethod:   lo.ToPtr(types.CollectionMethodSendInvoice),
		SubscriptionCreationConfig: dto.SubscriptionCreationConfig{
			Addons: []dto.AddAddonToSubscriptionRequest{
				{AddonID: couponTestAddonID},
				{AddonID: couponTestAddonID},
			},
			SubscriptionCoupons: parentCoupons,
		},
		Inheritance: &dto.SubscriptionInheritanceConfig{
			GroupedInvoicingChildrenToCreate: []dto.GroupedInvoicingChildRequest{
				childReq(ws1.ExternalID),
				childReq(ws2.ExternalID),
			},
		},
	})
	s.Require().NoError(err)

	children := s.groupedChildrenOf(resp.ID)
	s.Require().Len(children, 2)

	return &couponTestGroup{
		parentID: resp.ID,
		childIDs: lo.Map(children, func(c *subscription.Subscription, _ int) string { return c.ID }),
	}
}

// invoiceFor bills a subscription for its current period and returns the computed invoice.
func (s *SubscriptionServiceSuite) invoiceFor(subID string) *dto.InvoiceResponse {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, subID)
	s.Require().NoError(err)

	inv, _, err := s.createInvoiceService().CreateSubscriptionInvoice(ctx, &dto.CreateSubscriptionInvoiceRequest{
		SubscriptionID: sub.ID,
		PeriodStart:    sub.CurrentPeriodStart,
		PeriodEnd:      sub.CurrentPeriodEnd,
		ReferencePoint: types.ReferencePointPeriodStart,
	}, nil, types.InvoiceFlowManual, false)
	s.Require().NoError(err)
	s.Require().NotNil(inv)
	return inv
}

func linesForPrice(inv *dto.InvoiceResponse, priceID string) []*dto.InvoiceLineItemResponse {
	return lo.Filter(inv.LineItems, func(li *dto.InvoiceLineItemResponse, _ int) bool {
		return lo.FromPtr(li.PriceID) == priceID
	})
}

func totalDiscountOn(li *dto.InvoiceLineItemResponse) decimal.Decimal {
	return li.LineItemDiscount.Add(li.InvoiceLevelDiscount)
}

func discountedLineIDs(inv *dto.InvoiceResponse) []string {
	return lo.FilterMap(inv.LineItems, func(li *dto.InvoiceLineItemResponse, _ int) (string, bool) {
		return li.ID, totalDiscountOn(li).GreaterThan(decimal.Zero)
	})
}

// ─── resolution: which line items a price_id names ─────────────────────────────────────────────

// TestSubscriptionCoupon_PlanPriceResolvesToItsLineItem guards the path that always worked: a plan
// price must keep resolving to its line item now that the map is rebuilt from persisted line items
// rather than the plan-derived ones.
func (s *SubscriptionServiceSuite) TestSubscriptionCoupon_PlanPriceResolvesToItsLineItem() {
	seatPlan := s.setupSeatFeePlan()
	c := s.seedCouponPercentOff("coupon_plan_price", 100, 0)

	resp, err := s.createStandaloneSub(seatPlan.ID, 0, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(s.planPriceIDOf(seatPlan.ID))},
	})
	s.Require().NoError(err)

	assocs := s.associationsFor(resp.ID, c.ID)
	s.Require().Len(assocs, 1)
	s.NotNil(assocs[0].SubscriptionLineItemID, "a plan price_id must scope the coupon to its line item")
}

// TestSubscriptionCoupon_AddonPriceResolvesToEveryAttachment covers the case a price_id cannot
// disambiguate: one addon attached twice yields two line items behind one price. SubscriptionCouponInput
// can only name a price, so singling out one of them is not expressible — refusing would leave the
// shape with no way to state its discount at all, so the coupon covers both.
func (s *SubscriptionServiceSuite) TestSubscriptionCoupon_AddonPriceResolvesToEveryAttachment() {
	seatPlan := s.setupSeatFeePlan()
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_addon_twice", 100, 0)

	resp, err := s.createStandaloneSub(seatPlan.ID, 2, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)},
	})
	s.Require().NoError(err)

	addonLineItems := s.addonLineItemsFor(resp.ID, couponTestAddonID)
	s.Require().Len(addonLineItems, 2, "both attachments should produce their own line item")

	assocs := s.associationsFor(resp.ID, c.ID)
	s.Require().Len(assocs, 2, "one association per matching line item")
	s.ElementsMatch(
		lo.Map(addonLineItems, func(li *subscription.SubscriptionLineItem, _ int) string { return li.ID }),
		lo.Map(assocs, func(a *coupon_association.CouponAssociation, _ int) string {
			return lo.FromPtr(a.SubscriptionLineItemID)
		}),
		"every line item behind the price must be covered, none twice",
	)
}

// TestSubscriptionCoupon_ChildAddonPriceScopesToEachChild is the shape the feature exists for: two
// grouped children, the same addon on both, one coupon discounting that addon on each.
func (s *SubscriptionServiceSuite) TestSubscriptionCoupon_ChildAddonPriceScopesToEachChild() {
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_child_addon", 100, 0)

	group := s.createCouponTestGroup("scope", nil, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)},
	})

	for _, childID := range group.childIDs {
		addonLineItems := s.addonLineItemsFor(childID, couponTestAddonID)
		s.Require().Len(addonLineItems, 1, "child %s should carry one addon line item", childID)

		assocs := s.associationsFor(childID, c.ID)
		s.Require().Len(assocs, 1, "child %s should hold exactly one association", childID)
		s.Equal(
			addonLineItems[0].ID,
			lo.FromPtr(assocs[0].SubscriptionLineItemID),
			"the coupon must target this child's own addon line item, not fall back to subscription level",
		)
	}

	s.Empty(s.associationsFor(group.parentID, c.ID),
		"the parent must not be associated with a coupon only its children carry")
}

// TestSubscriptionCoupon_UnknownPriceIDIsRejected pins the behaviour change: an unresolvable
// price_id used to be logged and downgraded to a subscription-level coupon, quietly discounting
// every charge instead of one line. A caller naming a price that isn't there is now told.
func (s *SubscriptionServiceSuite) TestSubscriptionCoupon_UnknownPriceIDIsRejected() {
	seatPlan := s.setupSeatFeePlan()
	c := s.seedCouponPercentOff("coupon_unknown_price", 100, 0)

	_, err := s.createStandaloneSub(seatPlan.ID, 0, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr("price_does_not_exist")},
	})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
}

// TestSubscriptionCoupon_WithoutRedemptionHeadroomIsRejected covers the cost of covering every
// attachment: each line item becomes its own association and consumes its own redemption, so a
// single-use coupon cannot cover an addon attached twice. CouponValidationService re-reads the
// coupon before each association, so the second is refused there.
func (s *SubscriptionServiceSuite) TestSubscriptionCoupon_WithoutRedemptionHeadroomIsRejected() {
	seatPlan := s.setupSeatFeePlan()
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_headroom_one", 100, 1)

	_, err := s.createStandaloneSub(seatPlan.ID, 2, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)},
	})
	s.Require().Error(err)
	s.True(ierr.IsValidation(err), "expected a validation error, got %v", err)
	s.Contains(err.Error(), "redemption", "the error must name the redemption shortfall")
}

// TestSubscriptionCoupon_WithHeadroomForEveryChildSucceeds is the counterpart: the same coupon
// across two grouped children needs two redemptions, and having exactly that many is enough.
func (s *SubscriptionServiceSuite) TestSubscriptionCoupon_WithHeadroomForEveryChildSucceeds() {
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_headroom_two", 100, 2)

	group := s.createCouponTestGroup("headroom", nil, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)},
	})
	s.Require().Len(group.childIDs, 2)

	updated, err := s.GetStores().CouponRepo.Get(s.GetContext(), c.ID)
	s.NoError(err)
	s.Equal(2, updated.TotalRedemptions, "one redemption per child association")
}

// TestSubscriptionCoupon_AssociationResponseCarriesCouponDetails guards the payload, not the
// discount. CouponAssociationResponse.Coupon and the embedded domain model's Coupon both marshal
// to "coupon" and the outer field wins, so a nil outer field silently discards a coupon the
// repository always eager-loads — leaving clients to render raw IDs with no name, code or amount.
// Asserted without expand=coupon, which is how the subscription detail endpoint calls it.
func (s *SubscriptionServiceSuite) TestSubscriptionCoupon_AssociationResponseCarriesCouponDetails() {
	seatPlan := s.setupSeatFeePlan()
	c := s.seedCouponPercentOff("coupon_response_shape", 25, 0)

	resp, err := s.createStandaloneSub(seatPlan.ID, 0, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(s.planPriceIDOf(seatPlan.ID))},
	})
	s.Require().NoError(err)

	couponAssociationService := NewCouponAssociationService(ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		CouponAssociationRepo:    s.GetStores().CouponAssociationRepo,
		CouponRepo:               s.GetStores().CouponRepo,
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
	})

	filter := types.NewCouponAssociationFilter()
	filter.SubscriptionIDs = []string{resp.ID}
	listed, err := couponAssociationService.ListCouponAssociations(s.GetContext(), filter)
	s.Require().NoError(err)
	s.Require().Len(listed.Items, 1)

	got := listed.Items[0]
	s.Require().NotNil(got.Coupon, "coupon must be present without an explicit expand")
	s.Equal(c.ID, got.Coupon.ID)
	s.Equal(c.Name, got.Coupon.Name, "name renders in the UI's Coupon Name column")
	s.Require().NotNil(got.Coupon.CouponCode)
	s.Equal(*c.CouponCode, *got.Coupon.CouponCode)
	s.Require().NotNil(got.Coupon.PercentageOff, "discount column needs the amount")
	s.True(decimal.NewFromInt(25).Equal(*got.Coupon.PercentageOff))
}

// ─── discount matrix: where the discount lands on the invoice ──────────────────────────────────

// 1. Standalone subscription, coupon on its plan charge.
func (s *SubscriptionServiceSuite) TestDiscountMatrix_1_StandalonePlanCharge() {
	seatPlan := s.setupSeatFeePlan()
	c := s.seedCouponPercentOff("coupon_matrix_1", 100, 0)
	planPriceID := s.planPriceIDOf(seatPlan.ID)

	resp, err := s.createStandaloneSub(seatPlan.ID, 0, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(planPriceID)},
	})
	s.Require().NoError(err)

	inv := s.invoiceFor(resp.ID)
	planLines := linesForPrice(inv, planPriceID)
	s.Require().Len(planLines, 1)
	s.True(planLines[0].Amount.Equal(planLines[0].LineItemDiscount),
		"plan charge should be fully discounted: amount %s, discount %s",
		planLines[0].Amount, planLines[0].LineItemDiscount)
}

// 2. Standalone subscription, two addons on one price, coupon sent once — both lines discounted.
// Without matching on subscription_line_item_id both coupons would collapse onto whichever line
// won the price lookup, double-discounting it and leaving the other at full price.
func (s *SubscriptionServiceSuite) TestDiscountMatrix_2_StandaloneBothAddonsFromOneCoupon() {
	seatPlan := s.setupSeatFeePlan()
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_matrix_2", 100, 0)

	resp, err := s.createStandaloneSub(seatPlan.ID, 2, []dto.SubscriptionCouponInput{
		{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)},
	})
	s.Require().NoError(err)

	inv := s.invoiceFor(resp.ID)
	addonLines := linesForPrice(inv, couponTestAddonPriceID)
	s.Require().Len(addonLines, 2, "both addon attachments should be charged")
	for _, li := range addonLines {
		s.True(li.Amount.Equal(li.LineItemDiscount),
			"one coupon on the price must cover both addon lines: line %s amount %s discount %s",
			li.ID, li.Amount, li.LineItemDiscount)
	}
}

// 3. Parent-level price coupon stays on the parent's own line items. It does not cascade to the
// children sharing that price — a child is discounted by attaching the coupon to that child, which
// keeps the blast radius of a parent-level coupon explicit rather than growing as children are added.
func (s *SubscriptionServiceSuite) TestDiscountMatrix_3_ParentPriceCouponStaysOnTheParent() {
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_matrix_3", 100, 0)

	group := s.createCouponTestGroup("s3",
		[]dto.SubscriptionCouponInput{{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)}},
		nil,
	)

	inv := s.invoiceFor(group.parentID)
	addonLines := linesForPrice(inv, couponTestAddonPriceID)
	s.Require().Len(addonLines, 4, "two addon lines on the parent plus one per child")

	for _, li := range addonLines {
		if lo.FromPtr(li.SubscriptionID) == group.parentID {
			s.True(li.Amount.Equal(li.LineItemDiscount),
				"the parent's own addon lines must be discounted: line %s amount %s discount %s",
				li.ID, li.Amount, li.LineItemDiscount)
			continue
		}
		s.True(totalDiscountOn(li).IsZero(),
			"a child's addon line must be untouched by the parent's coupon: line %s (sub %s) discount %s",
			li.ID, lo.FromPtr(li.SubscriptionID), totalDiscountOn(li))
	}
}

// 4. Child-level price coupon reaches that child's line and nothing else — the parent and the
// sibling bill the same addon price, so only subscription_line_item_id keeps them apart.
func (s *SubscriptionServiceSuite) TestDiscountMatrix_4_ChildPriceCouponStaysOnThatChild() {
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_matrix_4", 100, 0)

	group := s.createCouponTestGroup("s4", nil,
		[]dto.SubscriptionCouponInput{{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)}},
	)

	inv := s.invoiceFor(group.parentID)

	for _, li := range linesForPrice(inv, couponTestAddonPriceID) {
		owner := lo.FromPtr(li.SubscriptionID)
		if owner == group.parentID {
			s.True(li.LineItemDiscount.IsZero(),
				"the parent's own addon lines must be untouched by a child's coupon: line %s discount %s",
				li.ID, li.LineItemDiscount)
			continue
		}
		s.True(li.Amount.Equal(li.LineItemDiscount),
			"each child's addon line must be discounted by its own coupon: line %s (sub %s) amount %s discount %s",
			li.ID, owner, li.Amount, li.LineItemDiscount)
	}
}

// TestDiscountMatrix_4c_RecalculationKeepsChildDiscounts covers PUT /invoices/:id with
// apply_discount, which wipes every coupon application on the invoice and rebuilds from the
// associations standing at that moment.
//
// On a grouped parent that rebuild has to see the children too. Resolving only the parent does not
// merely skip their discounts — the wipe already removed them, so a correctly discounted invoice
// silently gains back the children's charges while their coupons stay attached.
func (s *SubscriptionServiceSuite) TestDiscountMatrix_4c_RecalculationKeepsChildDiscounts() {
	ctx := s.GetContext()
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_matrix_4c", 100, 0)

	group := s.createCouponTestGroup("s4c", nil,
		[]dto.SubscriptionCouponInput{{CouponCode: *c.CouponCode, PriceID: lo.ToPtr(couponTestAddonPriceID)}},
	)

	inv := s.invoiceFor(group.parentID)
	discountBefore := inv.TotalDiscount
	s.Require().True(discountBefore.Equal(decimal.NewFromInt(30)),
		"both children's addons discounted up front, got %s", discountBefore)

	// Recalculation only runs on drafts, and the opening invoice is finalized as it is created.
	// Draft grouped-parent invoices are real — a checkout parent stays draft until payment — so
	// put it back to draft rather than skip the case.
	stored, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	stored.InvoiceStatus = types.InvoiceStatusDraft
	s.Require().NoError(s.GetStores().InvoiceRepo.Update(ctx, stored))

	_, err = s.createInvoiceService().UpdateInvoice(ctx, inv.ID, dto.UpdateInvoiceRequest{ApplyDiscount: true})
	s.Require().NoError(err)

	after, err := s.GetStores().InvoiceRepo.Get(ctx, inv.ID)
	s.Require().NoError(err)
	s.True(discountBefore.Equal(after.TotalDiscount),
		"recalculation must not drop the children's discounts: before %s, after %s",
		discountBefore, after.TotalDiscount)

	lineFilter := types.NewNoLimitInvoiceLineItemFilter()
	lineFilter.InvoiceIDs = []string{inv.ID}
	lines, err := s.GetStores().InvoiceLineItemRepo.List(ctx, lineFilter)
	s.Require().NoError(err)

	for _, li := range lines {
		if lo.FromPtr(li.PriceID) != couponTestAddonPriceID || lo.FromPtr(li.SubscriptionID) == group.parentID {
			continue
		}
		s.True(li.Amount.Equal(li.LineItemDiscount),
			"child addon line %s must still be discounted after recalculation: amount %s discount %s",
			li.ID, li.Amount, li.LineItemDiscount)
	}
}

// 5. A child's subscription-level coupon does nothing today — a known, deliberate gap.
//
// It has no line item to anchor to, and an invoice-level coupon discounts the entire invoice, so
// merging it onto the parent's would reach the parent's and the siblings' charges. Confining it
// needs invoice coupons to carry the set of lines they may touch, plus a distributor that lets
// scopes compose. Discounting nothing is the safe failure: it under-discounts visibly instead of
// quietly discounting charges the coupon was never meant to reach. Price-scoped coupons (cases 3
// and 4) are the supported way to discount a child.
func (s *SubscriptionServiceSuite) TestDiscountMatrix_5_ChildSubscriptionLevelCouponIsNotApplied() {
	s.seedSharedAddon()
	c := s.seedCouponPercentOff("coupon_matrix_5", 50, 0)

	group := s.createCouponTestGroup("s5", nil,
		[]dto.SubscriptionCouponInput{{CouponCode: *c.CouponCode}},
	)

	// The association is still recorded against each child — only the billing side declines it.
	for _, childID := range group.childIDs {
		s.Len(s.associationsFor(childID, c.ID), 1, "child %s should still hold the association", childID)
	}

	inv := s.invoiceFor(group.parentID)
	s.Empty(discountedLineIDs(inv), "no line may be discounted by a child's subscription-level coupon")
	s.True(inv.TotalDiscount.IsZero(), "invoice discount should be zero, got %s", inv.TotalDiscount)
}
