package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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
		s.Equal(previewCreatedLineItemID, li.ID, "preview reports no real line item IDs")
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
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_single_ca", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	result, err := s.service.(*subscriptionService).attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           "addon_single_ca",
		Cadence:           types.AddonCadenceRecurring,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		ChangeAt:          lo.ToPtr(types.ScheduleTypePeriodEnd),
	}, nil)
	s.Require().NoError(err)

	s.Require().Len(result.GetCreatedLineItems(), 1)
	s.True(result.GetCreatedLineItems()[0].StartDate.Equal(sub.CurrentPeriodEnd))
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
