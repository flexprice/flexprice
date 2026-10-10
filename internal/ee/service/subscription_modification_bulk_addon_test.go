package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// The `addons` modify type. Batch behaviour is pinned in subscription_addon_change_test.go;
// these cover the dispatch layer — mapping, response shape, preview/execute agreement.

func (s *SubscriptionServiceSuite) modificationService() SubscriptionModificationService {
	return NewSubscriptionModificationService(s.service.(*subscriptionService).ServiceParams)
}

func (s *SubscriptionServiceSuite) bulkAddonRequest(params *dto.SubModifyBulkAddonParams) dto.ExecuteSubscriptionModifyRequest {
	return dto.ExecuteSubscriptionModifyRequest{
		Type:            dto.SubscriptionModifyTypeAddon,
		BulkAddonParams: params,
	}
}

func (s *SubscriptionServiceSuite) modifyAdd(addonID string, at time.Time) *dto.AddAddonToSubscriptionRequest {
	return &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		StartDate:         lo.ToPtr(at),
	}
}

func (s *SubscriptionServiceSuite) modifyAddAt(addonID string, changeAt types.ScheduleType) *dto.AddAddonToSubscriptionRequest {
	return &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ChangeAt:          lo.ToPtr(changeAt),
	}
}

func (s *SubscriptionServiceSuite) modifyRemove(associationID string, at time.Time) *dto.RemoveAddonRequest {
	return &dto.RemoveAddonRequest{
		AddonAssociationID: associationID,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		EffectiveDate:      lo.ToPtr(at),
	}
}

func (s *SubscriptionServiceSuite) changedLineItemsByAction(
	resp *dto.SubscriptionModifyResponse,
	action dto.ChangedLineItemAction,
) []dto.ChangedLineItem {
	return lo.Filter(resp.ChangedResources.LineItems, func(li dto.ChangedLineItem, _ int) bool {
		return li.ChangeAction == action
	})
}

func (s *SubscriptionServiceSuite) TestExecuteBulkAddonModification_Swap_OneNettedInvoice() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_mod_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_mod_in", decimal.NewFromInt(60), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_mod_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	resp, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds:    []*dto.AddAddonToSubscriptionRequest{s.modifyAdd("addon_mod_in", at)},
		Removes: []*dto.RemoveAddonRequest{s.modifyRemove(outgoing, at)},
	}))
	s.Require().NoError(err)

	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1, "a swap settles as one document through the modify endpoint too")
	s.Empty(s.prorationCredits(), "a net-positive swap must not also credit the wallet")

	s.Require().Len(resp.ChangedResources.Invoices, 1)
	s.Len(s.changedLineItemsByAction(resp, dto.ChangedLineItemActionCreated), 1, "the incoming addon is reported")
	s.Len(s.changedLineItemsByAction(resp, dto.ChangedLineItemActionEnded), 1, "the outgoing addon is reported")
	s.Nil(resp.CheckoutSession, "pay-later carries no checkout session")

	s.Len(s.addonLineItemsFor(sub.ID, "addon_mod_in"), 1)
}

// The single-addon path stamps one date on every ended item, misreporting a batch whose
// entries land on different days.
func (s *SubscriptionServiceSuite) TestExecuteBulkAddonModification_PerEntryDates_EndedItemsCarryTheirOwnDate() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_mod_d1", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_mod_d2", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	first := s.attachForRemoval("addon_mod_d1", 30)
	second := s.attachForRemoval("addon_mod_d2", 30)

	early := sub.CurrentPeriodStart.Add(5 * 24 * time.Hour)
	late := sub.CurrentPeriodStart.Add(20 * 24 * time.Hour)

	resp, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Removes: []*dto.RemoveAddonRequest{
			s.modifyRemove(first, early),
			s.modifyRemove(second, late),
		},
	}))
	s.Require().NoError(err)

	ended := s.changedLineItemsByAction(resp, dto.ChangedLineItemActionEnded)
	s.Require().Len(ended, 2)

	dates := lo.Map(ended, func(li dto.ChangedLineItem, _ int) time.Time { return lo.FromPtr(li.EndDate) })
	s.True(lo.SomeBy(dates, func(d time.Time) bool { return d.Equal(early) }), "one item ends on the early date")
	s.True(lo.SomeBy(dates, func(d time.Time) bool { return d.Equal(late) }), "the other ends on the late date")
}

func (s *SubscriptionServiceSuite) TestPreviewBulkAddonModification_WritesNothingAndQuotesTheExecutedNet() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_mod_p1", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_mod_p2", decimal.NewFromInt(40), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(12 * 24 * time.Hour)
	req := s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{
			s.modifyAdd("addon_mod_p1", at),
			s.modifyAdd("addon_mod_p2", at),
		},
	})

	previewed, err := s.modificationService().Preview(ctx, sub.ID, req)
	s.Require().NoError(err)

	s.Empty(s.addonLineItemsFor(sub.ID, "addon_mod_p1"), "preview writes no line items")
	s.Empty(s.oneOffInvoicesFor(sub.ID), "preview raises no invoice")
	s.Require().Len(previewed.ChangedResources.Invoices, 1)
	s.Len(s.changedLineItemsByAction(previewed, dto.ChangedLineItemActionCreated), 2)
	for _, li := range previewed.ChangedResources.LineItems {
		s.Equal(previewCreatedID, li.ID, "preview reports no real line item IDs")
	}

	executed, err := s.modificationService().Execute(ctx, sub.ID, req)
	s.Require().NoError(err)

	s.Require().Len(executed.ChangedResources.Invoices, 1)
	s.True(lo.FromPtr(executed.ChangedResources.Invoices[0].Invoice).AmountDue.
		Equal(lo.FromPtr(previewed.ChangedResources.Invoices[0].Invoice).AmountDue),
		"execute bills exactly what preview quoted")
}

func (s *SubscriptionServiceSuite) TestExecuteBulkAddonModification_Rollback_LeavesNothingBehind() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_mod_ok", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	_, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{
			s.modifyAdd("addon_mod_ok", at),
			s.modifyAdd("addon_does_not_exist", at),
		},
	}))
	s.Require().Error(err)

	s.Empty(s.addonLineItemsFor(sub.ID, "addon_mod_ok"),
		"the first attach must roll back with the batch, not persist alone")
	s.Empty(s.oneOffInvoicesFor(sub.ID), "a failed batch raises no invoice")

	// The association is written before the line items, so a batch that rolled back its
	// line items but left the association behind would still read as attached.
	assocFilter := types.NewNoLimitAddonAssociationFilter()
	assocFilter.EntityIDs = []string{sub.ID}
	associations, listErr := s.GetStores().AddonAssociationRepo.List(ctx, assocFilter)
	s.Require().NoError(listErr)
	s.Empty(associations, "a failed batch leaves no addon association behind")
}

func (s *SubscriptionServiceSuite) TestBulkAddonModification_InvalidRequestRejected() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	_, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{}))
	s.Error(err, "a batch with no entries is rejected")

	_, err = s.modificationService().Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeAddon,
	})
	s.Error(err, "type addons without addons_params is rejected")
}

// change_at is resolved once per batch, so two immediate entries land on the same date and
// prorate in one pass instead of splitting into two documents.
func (s *SubscriptionServiceSuite) TestExecuteBulkAddonModification_ChangeAtImmediate_SharesOneDate() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_ca_a", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_ca_b", decimal.NewFromInt(40), types.InvoiceCadenceAdvance)

	_, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{
			s.modifyAddAt("addon_ca_a", types.ScheduleTypeImmediate),
			s.modifyAddAt("addon_ca_b", types.ScheduleTypeImmediate),
		},
	}))
	s.Require().NoError(err)

	s.Require().Len(s.oneOffInvoicesFor(sub.ID), 1, "two immediate entries settle as one document")
	s.Len(s.addonLineItemsFor(sub.ID, "addon_ca_a"), 1)
	s.Len(s.addonLineItemsFor(sub.ID, "addon_ca_b"), 1)
}

// end_of_period resolves to the subscription's period end, which is outside the current
// period, so the entry contributes no proration charge.
func (s *SubscriptionServiceSuite) TestExecuteBulkAddonModification_ChangeAtPeriodEnd_ChargesNothingNow() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_ca_end", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	resp, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{s.modifyAddAt("addon_ca_end", types.ScheduleTypePeriodEnd)},
	}))
	s.Require().NoError(err)

	created := s.changedLineItemsByAction(resp, dto.ChangedLineItemActionCreated)
	s.Require().Len(created, 1)
	s.True(lo.FromPtr(created[0].StartDate).Equal(sub.CurrentPeriodEnd),
		"the attach starts at the period end, not now")
	s.Empty(s.oneOffInvoicesFor(sub.ID), "a period-end attach bills nothing in the current period")
}

// change_at works on the single-addon path too: both reach the same Resolve.
func (s *SubscriptionServiceSuite) TestAttachAddon_ChangeAtPeriodEnd_StartsAtPeriodEnd() {
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_single_ca", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	result, err := s.attachOne(sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           "addon_single_ca",
		Cadence:           types.AddonCadenceRecurring,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ChangeAt:          lo.ToPtr(types.ScheduleTypePeriodEnd),
	}, nil)
	s.Require().NoError(err)

	s.Require().Len(result.CreatedLineItems, 1)
	s.True(result.CreatedLineItems[0].StartDate.Equal(sub.CurrentPeriodEnd))
	s.Empty(s.oneOffInvoicesFor(sub.ID), "a period-end attach bills nothing in the current period")
}

// The resolved request must not write back into the caller's DTO.
func (s *SubscriptionServiceSuite) TestBulkAddonModification_ChangeAt_DoesNotMutateTheRequest() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_ca_pure", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	add := s.modifyAddAt("addon_ca_pure", types.ScheduleTypeImmediate)
	_, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{add},
	}))
	s.Require().NoError(err)

	s.Nil(add.StartDate, "resolution happens on a copy")
	s.Require().NotNil(add.ChangeAt)
	s.Equal(types.ScheduleTypeImmediate, *add.ChangeAt)
}

// -----------------------------------------------------------------------------
// changed addon associations
// -----------------------------------------------------------------------------
//
// The association id is the only handle a caller has on what an attach created: removing it
// later names it. Without it a client has to list the subscription's associations and guess.

func (s *SubscriptionServiceSuite) changedAssociationsByAction(
	resp *dto.SubscriptionModifyResponse,
	action dto.ChangedAddonAssociationAction,
) []dto.ChangedAddonAssociation {
	return lo.Filter(resp.ChangedResources.AddonAssociations,
		func(a dto.ChangedAddonAssociation, _ int) bool { return a.ChangeAction == action })
}

func (s *SubscriptionServiceSuite) TestBulkAddonModification_Execute_ReportsTheCreatedAssociation() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_assoc_add", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	resp, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{s.modifyAdd("addon_assoc_add", at)},
	}))
	s.Require().NoError(err)

	created := s.changedAssociationsByAction(resp, dto.ChangedAddonAssociationActionCreated)
	s.Require().Len(created, 1)
	s.Equal("addon_assoc_add", created[0].AddonID)
	s.Equal(types.AddonStatusActive, created[0].AddonStatus)

	// The reported id must name the row that was actually written, or a caller cannot remove it.
	stored, err := s.GetStores().AddonAssociationRepo.GetByID(ctx, created[0].ID)
	s.Require().NoError(err)
	s.Equal("addon_assoc_add", stored.AddonID)
	s.Equal(sub.ID, stored.EntityID)
}

func (s *SubscriptionServiceSuite) TestBulkAddonModification_Execute_ReportsTheEndedAssociation() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_assoc_remove", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_assoc_remove", 30)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	resp, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Removes: []*dto.RemoveAddonRequest{s.modifyRemove(outgoing, at)},
	}))
	s.Require().NoError(err)

	ended := s.changedAssociationsByAction(resp, dto.ChangedAddonAssociationActionEnded)
	s.Require().Len(ended, 1)
	s.Equal(outgoing, ended[0].ID, "a removal names the row it ended")
	s.Equal(types.AddonStatusCancelled, ended[0].AddonStatus)
	s.Require().NotNil(ended[0].EndDate)
	s.True(ended[0].EndDate.Equal(at))
}

func (s *SubscriptionServiceSuite) TestBulkAddonModification_Swap_ReportsBothDirections() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_assoc_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_assoc_in", decimal.NewFromInt(60), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_assoc_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	resp, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds:    []*dto.AddAddonToSubscriptionRequest{s.modifyAdd("addon_assoc_in", at)},
		Removes: []*dto.RemoveAddonRequest{s.modifyRemove(outgoing, at)},
	}))
	s.Require().NoError(err)

	s.Len(s.changedAssociationsByAction(resp, dto.ChangedAddonAssociationActionCreated), 1)
	s.Len(s.changedAssociationsByAction(resp, dto.ChangedAddonAssociationActionEnded), 1)
}

// A preview writes no association, so the id it reports must not look removable. A preview
// removal names a row that does exist, and keeps its real id.
func (s *SubscriptionServiceSuite) TestBulkAddonModification_Preview_MasksTheUnwrittenID() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_assoc_preview", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_assoc_preview", 30)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	resp, err := s.modificationService().Preview(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds:    []*dto.AddAddonToSubscriptionRequest{s.modifyAdd("addon_assoc_preview", at)},
		Removes: []*dto.RemoveAddonRequest{s.modifyRemove(outgoing, at)},
	}))
	s.Require().NoError(err)

	created := s.changedAssociationsByAction(resp, dto.ChangedAddonAssociationActionCreated)
	s.Require().Len(created, 1)
	s.Equal(previewCreatedID, created[0].ID)

	ended := s.changedAssociationsByAction(resp, dto.ChangedAddonAssociationActionEnded)
	s.Require().Len(ended, 1)
	s.Equal(outgoing, ended[0].ID, "the removed row is real, so its id is real")
}

// -----------------------------------------------------------------------------
// coupons on an attach
// -----------------------------------------------------------------------------

func seedPercentCoupon(ctx context.Context, r *require.Assertions, stores testutil.Stores, code string, pct int64, maxRedemptions *int) *coupon.Coupon {
	c := &coupon.Coupon{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COUPON),
		Name:           code,
		CouponCode:     lo.ToPtr(code),
		Type:           types.CouponTypePercentage,
		Cadence:        types.CouponCadenceForever,
		PercentageOff:  lo.ToPtr(decimal.NewFromInt(pct)),
		MaxRedemptions: maxRedemptions,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	c.Status = types.StatusPublished
	r.NoError(stores.CouponRepo.Create(ctx, c))
	return c
}

func couponRedemptions(ctx context.Context, r *require.Assertions, stores testutil.Stores, couponID string) int {
	c, err := stores.CouponRepo.Get(ctx, couponID)
	r.NoError(err)
	return c.TotalRedemptions
}

func subscriptionCouponAssociations(ctx context.Context, r *require.Assertions, stores testutil.Stores, subID string) []*coupon_association.CouponAssociation {
	filter := types.NewNoLimitCouponAssociationFilter()
	filter.SubscriptionIDs = []string{subID}
	associations, err := stores.CouponAssociationRepo.List(ctx, filter)
	r.NoError(err)
	return associations
}

func (s *SubscriptionServiceSuite) modifyAddWithCoupon(addonID string, at time.Time, coupons ...dto.SubscriptionCouponInput) *dto.AddAddonToSubscriptionRequest {
	req := s.modifyAdd(addonID, at)
	req.Coupons = coupons
	return req
}

// seedSecondAddonPrice gives an addon from seedFixedPriceAddon a second fixed price.
func (s *SubscriptionServiceSuite) seedSecondAddonPrice(addonID string, amount decimal.Decimal) string {
	ctx := s.GetContext()
	priceID := "price_" + addonID + "_b"
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, &price.Price{
		ID:                 priceID,
		Amount:             amount,
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               types.PRICE_TYPE_FIXED,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceAdvance,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}))
	return priceID
}

func approxEqual(a, b decimal.Decimal) bool {
	return a.Sub(b).Abs().LessThanOrEqual(decimal.NewFromFloat(0.01))
}

func (s *SubscriptionServiceSuite) TestBulkAddonModification_Coupon_PreviewDiscountsAndWritesNothing() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_cpn_preview", decimal.NewFromInt(100), types.InvoiceCadenceAdvance)
	c := seedPercentCoupon(ctx, s.Require(), s.GetStores(), "CPN_PREVIEW", 50, nil)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	resp, err := s.modificationService().Preview(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{
			s.modifyAddWithCoupon("addon_cpn_preview", at, dto.SubscriptionCouponInput{CouponCode: "CPN_PREVIEW"}),
		},
	}))
	s.Require().NoError(err)

	s.Require().Len(resp.ChangedResources.Invoices, 1)
	inv := resp.ChangedResources.Invoices[0].Invoice
	s.Require().NotNil(inv)
	s.True(inv.Subtotal.IsPositive())
	s.True(approxEqual(inv.TotalDiscount, inv.Subtotal.Div(decimal.NewFromInt(2))),
		"discount %s should be half of subtotal %s", inv.TotalDiscount, inv.Subtotal)
	s.True(approxEqual(inv.AmountDue, inv.Subtotal.Sub(inv.TotalDiscount)),
		"amount due %s should be the discounted subtotal", inv.AmountDue)

	s.Empty(subscriptionCouponAssociations(ctx, s.Require(), s.GetStores(), sub.ID), "preview writes no association")
	s.Zero(couponRedemptions(ctx, s.Require(), s.GetStores(), c.ID), "preview redeems nothing")
	s.Empty(s.oneOffInvoicesFor(sub.ID))
}

func (s *SubscriptionServiceSuite) TestBulkAddonModification_Coupon_PayLaterFansOutPerPrice() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_cpn_fan", decimal.NewFromInt(100), types.InvoiceCadenceAdvance)
	s.seedSecondAddonPrice("addon_cpn_fan", decimal.NewFromInt(40))
	c := seedPercentCoupon(ctx, s.Require(), s.GetStores(), "CPN_FAN", 50, nil)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	_, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{
			s.modifyAddWithCoupon("addon_cpn_fan", at, dto.SubscriptionCouponInput{CouponCode: "CPN_FAN"}),
		},
	}))
	s.Require().NoError(err)

	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1)
	inv := invoices[0]
	s.True(inv.Subtotal.IsPositive())
	s.True(approxEqual(inv.TotalDiscount, inv.Subtotal.Div(decimal.NewFromInt(2))),
		"discount %s should be half of subtotal %s", inv.TotalDiscount, inv.Subtotal)
	s.True(approxEqual(inv.Total, inv.Subtotal.Sub(inv.TotalDiscount)),
		"total %s should be subtotal - discount", inv.Total)
	s.True(approxEqual(inv.AmountDue, inv.Total), "amount due %s should equal total %s", inv.AmountDue, inv.Total)

	lineItems := s.addonLineItemsFor(sub.ID, "addon_cpn_fan")
	s.Require().Len(lineItems, 2)
	lineItemIDs := lo.Map(lineItems, func(li *subscription.SubscriptionLineItem, _ int) string { return li.ID })

	associations := subscriptionCouponAssociations(ctx, s.Require(), s.GetStores(), sub.ID)
	s.Require().Len(associations, 2, "one association per addon price")
	for _, a := range associations {
		s.Equal(c.ID, a.CouponID)
		s.Require().NotNil(a.SubscriptionLineItemID)
		s.Contains(lineItemIDs, *a.SubscriptionLineItemID)
	}
	s.NotEqual(*associations[0].SubscriptionLineItemID, *associations[1].SubscriptionLineItemID)
	s.Equal(2, couponRedemptions(ctx, s.Require(), s.GetStores(), c.ID), "one redemption per association, none for the invoice")

	appFilter := types.NewNoLimitCouponApplicationFilter()
	appFilter.InvoiceIDs = []string{inv.ID}
	applications, err := s.GetStores().CouponApplicationRepo.List(ctx, appFilter)
	s.Require().NoError(err)
	s.Require().Len(applications, 2)
	associationIDs := lo.Map(associations, func(a *coupon_association.CouponAssociation, _ int) string { return a.ID })
	for _, app := range applications {
		s.Contains(associationIDs, app.CouponAssociationID, "the invoice application points at the created association")
	}
}

func (s *SubscriptionServiceSuite) TestBulkAddonModification_Coupon_RejectedAtQuote() {
	type tc struct {
		name  string
		input func(addonID string) dto.SubscriptionCouponInput
		maxed bool
	}
	cases := []tc{
		{
			name: "price_id_not_on_addon",
			input: func(addonID string) dto.SubscriptionCouponInput {
				return dto.SubscriptionCouponInput{CouponCode: "CPN_REJECT_PRICE", PriceID: lo.ToPtr("price_not_on_addon")}
			},
		},
		{
			name: "max_redemptions_reached",
			input: func(addonID string) dto.SubscriptionCouponInput {
				return dto.SubscriptionCouponInput{CouponCode: "CPN_REJECT_MAXED"}
			},
			maxed: true,
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			ctx := s.GetContext()
			sub := s.monthlyPeriodSubscription()
			addonID := "addon_cpn_" + tc.name
			s.seedFixedPriceAddon(addonID, decimal.NewFromInt(100), types.InvoiceCadenceAdvance)

			input := tc.input(addonID)
			var maxRedemptions *int
			if tc.maxed {
				maxRedemptions = lo.ToPtr(1)
			}
			c := seedPercentCoupon(ctx, s.Require(), s.GetStores(), input.CouponCode, 50, maxRedemptions)
			if tc.maxed {
				c.TotalRedemptions = 1
				s.Require().NoError(s.GetStores().CouponRepo.Update(ctx, c))
			}

			at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
			req := s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
				Adds: []*dto.AddAddonToSubscriptionRequest{s.modifyAddWithCoupon(addonID, at, input)},
			})

			_, err := s.modificationService().Preview(ctx, sub.ID, req)
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)

			_, err = s.modificationService().Execute(ctx, sub.ID, req)
			s.Require().Error(err)
			s.True(ierr.IsValidation(err), "got %v", err)

			s.Empty(s.addonLineItemsFor(sub.ID, addonID))
			s.Empty(s.oneOffInvoicesFor(sub.ID))
			s.Empty(subscriptionCouponAssociations(ctx, s.Require(), s.GetStores(), sub.ID))
		})
	}
}

// The discount comes off the charge before the credit is netted, so it can flip a charge into a credit.
func (s *SubscriptionServiceSuite) TestBulkAddonModification_Coupon_DiscountAppliedBeforeNetting() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_cpn_net_out", decimal.NewFromInt(80), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_cpn_net_in", decimal.NewFromInt(100), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_cpn_net_out", 80)
	seedPercentCoupon(ctx, s.Require(), s.GetStores(), "CPN_NET", 50, nil)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	add := s.addEntry("addon_cpn_net_in", at)
	add.Request.Coupons = []dto.SubscriptionCouponInput{{CouponCode: "CPN_NET"}}
	config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{add},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, at)},
	})
	s.Require().NoError(err)

	quote := config.getQuote()
	s.Require().True(quote.TotalChargeAmount.GreaterThan(quote.TotalCreditAmount),
		"undiscounted, the batch would have charged")
	s.True(approxEqual(quote.TotalDiscountAmount, quote.TotalChargeAmount.Div(decimal.NewFromInt(2))))
	net := quote.TotalChargeAmount.Sub(quote.TotalDiscountAmount).Sub(quote.TotalCreditAmount)
	s.Require().True(net.IsNegative(), "the discount flips the net, got %s", net)
	s.True(quote.NetAmount().Equal(net))

	s.Empty(s.oneOffInvoicesFor(sub.ID), "a discounted net credit raises no invoice")
	credits := s.prorationCredits()
	s.Require().Len(credits, 1)
	s.True(credits[0].Amount.Equal(net.Abs()), "credit %s should be charge - discount - credit = %s", credits[0].Amount, net.Abs())
	s.Len(subscriptionCouponAssociations(ctx, s.Require(), s.GetStores(), sub.ID), 1, "the coupon still attaches to the new addon")
}

// Removing a discounted addon credits against what was paid, not the list price.
func (s *SubscriptionServiceSuite) TestBulkAddonModification_Coupon_RemovalCreditsTheNetCharged() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	s.seedFixedPriceAddon("addon_cpn_basis", decimal.NewFromInt(100), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_cpn_basis_control", decimal.NewFromInt(100), types.InvoiceCadenceAdvance)
	seedPercentCoupon(ctx, s.Require(), s.GetStores(), "CPN_BASIS", 50, nil)

	start := sub.CurrentPeriodStart
	_, err := s.modificationService().Execute(ctx, sub.ID, s.bulkAddonRequest(&dto.SubModifyBulkAddonParams{
		Adds: []*dto.AddAddonToSubscriptionRequest{
			s.modifyAddWithCoupon("addon_cpn_basis", start, dto.SubscriptionCouponInput{CouponCode: "CPN_BASIS"}),
			s.modifyAdd("addon_cpn_basis_control", start),
		},
	}))
	s.Require().NoError(err)

	associationFor := func(addonID string) string {
		filter := types.NewNoLimitAddonAssociationFilter()
		filter.EntityIDs = []string{sub.ID}
		filter.AddonIDs = []string{addonID}
		rows, err := s.GetStores().AddonAssociationRepo.List(ctx, filter)
		s.Require().NoError(err)
		s.Require().Len(rows, 1)
		return rows[0].ID
	}

	at := sub.CurrentPeriodStart.Add(5 * 24 * time.Hour)
	removeCredit := func(addonID string) decimal.Decimal {
		config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
			Subscription: sub,
			Removes:      []*dto.RemoveAddonRequest{s.removeEntry(associationFor(addonID), at)},
		})
		s.Require().NoError(err)
		return config.getQuote().TotalCreditAmount
	}

	discountedItems := s.addonLineItemsFor(sub.ID, "addon_cpn_basis")
	s.Require().Len(discountedItems, 1)
	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1)
	inv, err := s.GetStores().InvoiceRepo.Get(ctx, invoices[0].ID)
	s.Require().NoError(err)
	netCharged := decimal.Zero
	for _, li := range inv.LineItems {
		if lo.FromPtr(li.SubscriptionLineItemID) == discountedItems[0].ID {
			netCharged = netCharged.Add(li.Amount.Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount))
		}
	}
	s.Require().True(approxEqual(netCharged, decimal.NewFromInt(50)), "the attach charged %s net", netCharged)

	// The prorated list credit (~83.87) exceeds what was paid, so the credit is capped at the net charged.
	discounted := removeCredit("addon_cpn_basis")
	control := removeCredit("addon_cpn_basis_control")
	s.True(control.GreaterThan(netCharged), "the undiscounted control credits the prorated list, got %s", control)
	s.True(approxEqual(discounted, netCharged),
		"discounted addon credited %s, expected the net charged %s", discounted, netCharged)
}
