package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Batch behaviour for AddonChangeService (plan Group D).
//
// These pin the states a single-addon caller cannot reach — a change that both charges and
// credits, several entries under one document, entries on different dates — plus the two
// behaviour changes D2 made deliberately. The single-addon baselines live in
// addon_characterisation_test.go and must stay green alongside these.

func (s *SubscriptionServiceSuite) addonChangeService() AddonChangeService {
	return NewAddonChangeService(s.service.(*subscriptionService).ServiceParams)
}

// attachForRemoval attaches an addon without raising money and records what it was billed, so a
// later removal in the batch has a credit basis to refund against.
func (s *SubscriptionServiceSuite) attachForRemoval(addonID string, billed int64) string {
	sub := s.testData.subscription

	attached, err := s.attachOne(sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(sub.CurrentPeriodStart),
		ProrationBehavior: types.ProrationBehaviorNone,
	}, nil)
	s.Require().NoError(err)

	lineItems := s.addonLineItemsFor(sub.ID, addonID)
	s.Require().Len(lineItems, 1)
	s.recordBilledForLineItem(lineItems[0].ID, decimal.NewFromInt(billed))

	return attached.Association.ID
}

func (s *SubscriptionServiceSuite) addEntry(addonID string, at time.Time) AddonAdd {
	return AddonAdd{Request: &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(at),
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}}
}

// undatedEntry is what the API sends for an ordinary "attach now": no date at all, resolved
// by the change itself.
func (s *SubscriptionServiceSuite) undatedEntry(addonID string) AddonAdd {
	return AddonAdd{Request: &dto.AddAddonToSubscriptionRequest{
		AddonID:           addonID,
		Cadence:           types.AddonCadenceRecurring,
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
	}}
}

func (s *SubscriptionServiceSuite) removeEntry(associationID string, at time.Time) *dto.RemoveAddonRequest {
	return &dto.RemoveAddonRequest{
		AddonAssociationID: associationID,
		ProrationBehavior:  types.ProrationBehaviorCreateProrations,
		EffectiveDate:      lo.ToPtr(at),
	}
}

// -----------------------------------------------------------------------------
// netting — the reason the batch cannot be a loop
// -----------------------------------------------------------------------------

// A swap charges and credits in the same change. It must settle as ONE document for the net,
// not an invoice for the charge plus a wallet credit for the credit.
func (s *SubscriptionServiceSuite) TestAddonBatch_Swap_SettlesAsOneNettedInvoice() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_swap_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_swap_in", decimal.NewFromInt(60), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_swap_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_swap_in", at)},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, at)},
	})
	s.Require().NoError(err)

	quote := config.getQuote()
	s.Require().True(quote.TotalChargeAmount.IsPositive(), "the incoming addon must charge")
	s.Require().True(quote.TotalCreditAmount.IsPositive(), "the outgoing addon must credit")

	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1, "a swap raises exactly one document, never a charge plus a credit")
	s.True(invoices[0].AmountDue.Equal(quote.NetAmount()),
		"the document bills the net, not the gross charge")
	s.Empty(s.prorationCredits(), "a net-positive swap must not also credit the wallet")
}

// The failure the old per-addon path had: a swap that nets to zero used to charge in full and
// credit in full. It must now move no money at all.
func (s *SubscriptionServiceSuite) TestAddonBatch_Swap_NetZero_MovesNoMoney() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_zero_out", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_zero_in", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_zero_out", 30)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_zero_in", at)},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, at)},
	})
	s.Require().NoError(err)

	s.True(config.getQuote().NetAmount().IsZero(),
		"like-for-like at one date nets to zero, got %s", config.getQuote().NetAmount())
	s.Empty(s.oneOffInvoicesFor(sub.ID), "a zero net raises no invoice")
	s.Empty(s.prorationCredits(), "a zero net issues no wallet credit")

	// The change itself still happened.
	s.Len(s.addonLineItemsFor(sub.ID, "addon_zero_in"), 1, "the incoming addon is attached")
}

// When the removal outweighs the addition the batch owes the customer, and that is one wallet
// credit for the net — not a credit for the removal and an invoice for the addition.
func (s *SubscriptionServiceSuite) TestAddonBatch_NetNegative_CreditsWalletOnce() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_neg_out", decimal.NewFromInt(60), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_neg_in", decimal.NewFromInt(10), types.InvoiceCadenceAdvance)
	outgoing := s.attachForRemoval("addon_neg_out", 60)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_neg_in", at)},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, at)},
	})
	s.Require().NoError(err)

	net := config.getQuote().NetAmount()
	s.Require().True(net.IsNegative(), "the removal must outweigh the addition, got %s", net)

	s.Empty(s.oneOffInvoicesFor(sub.ID), "a net credit raises no invoice")

	credits := s.prorationCredits()
	s.Require().Len(credits, 1, "one wallet credit for the net, not one per entry")
	s.True(credits[0].Amount.Equal(net.Abs()), "the wallet is credited the net, not the gross credit")
}

// -----------------------------------------------------------------------------
// several entries, one document
// -----------------------------------------------------------------------------

func (s *SubscriptionServiceSuite) TestAddonBatch_TwoAdds_RaiseOneInvoice() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_two_a", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_two_b", decimal.NewFromInt(50), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_two_a", at),
			s.addEntry("addon_two_b", at),
		},
	})
	s.Require().NoError(err)

	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1, "two adds are one change and bill as one document")
	s.True(invoices[0].AmountDue.Equal(config.getQuote().NetAmount()))
	s.Len(config.getQuote().ChargeLineItems, 2, "each addon keeps its own line on the document")

	s.Len(s.addonLineItemsFor(sub.ID, "addon_two_a"), 1)
	s.Len(s.addonLineItemsFor(sub.ID, "addon_two_b"), 1)
}

// Entries on different dates are priced against their own remaining period, then merged onto one
// document whose window opens at the earliest of them.
func (s *SubscriptionServiceSuite) TestAddonBatch_PerEntryDates_OneDocumentFromTheEarliest() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_date_early", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_date_late", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)

	early := sub.CurrentPeriodStart.Add(5 * 24 * time.Hour)
	late := sub.CurrentPeriodStart.Add(20 * 24 * time.Hour)

	config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_date_late", late),
			s.addEntry("addon_date_early", early),
		},
	})
	s.Require().NoError(err)

	s.True(config.getPeriodStart().Equal(early),
		"the document opens at the earliest entry regardless of request order")

	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1, "different dates still settle as one document")
	s.Require().NotNil(invoices[0].PeriodStart)
	s.True(invoices[0].PeriodStart.Equal(early))

	// Same price, later start ⇒ strictly less charged for the late entry.
	s.Require().Len(config.getQuote().ChargeLineItems, 2)
	byName := lo.GroupBy(config.getQuote().ChargeLineItems, func(li dto.CreateInvoiceLineItemRequest) string {
		return lo.FromPtr(li.PriceID)
	})
	s.True(byName["price_addon_date_late"][0].Amount.LessThan(byName["price_addon_date_early"][0].Amount),
		"a later entry is prorated over a shorter remainder")
}

// -----------------------------------------------------------------------------
// price overrides — the quote must describe what is billed
// -----------------------------------------------------------------------------

// Overrides mint a subscription-scoped price during Persist, after Resolve has quoted. The
// settled document must reflect the override, not the addon's list price.
func (s *SubscriptionServiceSuite) TestAddonAttach_PriceOverride_BillsTheOverriddenAmount() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_override", decimal.NewFromInt(60), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
	listPrice, _, err := s.addonChangeService().Preview(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_override", at)},
	})
	s.Require().NoError(err)
	listQuote := listPrice.getQuote().NetAmount()
	s.Require().True(listQuote.IsPositive())

	_, err = s.attachOne(sub, &dto.AddAddonToSubscriptionRequest{
		AddonID:           "addon_override",
		Cadence:           types.AddonCadenceRecurring,
		StartDate:         lo.ToPtr(at),
		ProrationBehavior: types.ProrationBehaviorCreateProrations,
		OverrideLineItems: []dto.OverrideLineItemRequest{{
			PriceID: "price_addon_override",
			Amount:  lo.ToPtr(decimal.NewFromInt(10)),
		}},
	}, nil)
	s.Require().NoError(err)

	invoices := s.oneOffInvoicesFor(sub.ID)
	s.Require().Len(invoices, 1)
	s.True(invoices[0].AmountDue.LessThan(listQuote),
		"a price overridden from 60 to 10 must bill less than the list quote, billed %s against list %s",
		invoices[0].AmountDue, listQuote)
}

// -----------------------------------------------------------------------------
// grant windows — what a per-addon pass got wrong
// -----------------------------------------------------------------------------

// Two addons feeding one feature, swapped in a single change: the removal and the addition are
// resolved in one pass, so exactly one window is live afterwards. A per-addon pass closed the
// successor the other had just opened.
func (s *SubscriptionServiceSuite) TestAddonBatch_SwapOnOneFeature_LeavesOneLiveGrantWindow() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_batch_swap")
	s.seedFullFeaturedAddon("addon_grant_out", "ent_grant_out", featureID, 30, 400)
	s.seedFullFeaturedAddon("addon_grant_in", "ent_grant_in", featureID, 30, 900)
	outgoing := s.attachForRemoval("addon_grant_out", 30)

	// Mark the outgoing addon's window evaluated. An unevaluated window is deleted rather than
	// closed, which would leave nothing for the successor to tile onto.
	opened := s.liveRow(featureID)
	s.Require().NotNil(opened)
	opened.LastComputedAt = lo.ToPtr(s.testData.now.Add(-time.Hour))
	s.Require().NoError(s.GetStores().EntitlementGrantRepo.UpdateSnapshot(ctx, opened))

	// Immediate: a future-dated swap on a funded feature is rejected, because the successor's
	// quota would be fixed now while the predecessor kept accruing usage until the boundary.
	at := s.testData.now
	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_grant_in", at)},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, at)},
	})
	s.Require().NoError(err)

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().GreaterOrEqual(len(rows), 2,
		"the swap must cut a window and open a successor, or there is nothing to tile")

	// Tiled means no two windows are live over the same instant — the failure a per-addon pass
	// produced when one entry closed the successor another had just opened.
	s.assertTiled(featureID)
	s.False(rows[len(rows)-1].ValidTo.Before(s.periodEnd()),
		"the successor window must run to the period end")
}

// -----------------------------------------------------------------------------
// preview
// -----------------------------------------------------------------------------

func (s *SubscriptionServiceSuite) TestAddonBatch_Preview_QuotesWithoutWriting() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_preview_a", decimal.NewFromInt(30), types.InvoiceCadenceAdvance)
	s.seedFixedPriceAddon("addon_preview_b", decimal.NewFromInt(40), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(12 * 24 * time.Hour)
	req := AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_preview_a", at),
			s.addEntry("addon_preview_b", at),
		},
	}

	previewed, _, err := s.addonChangeService().Preview(ctx, req)
	s.Require().NoError(err)
	s.Require().True(previewed.getQuote().NetAmount().IsPositive())

	s.Empty(s.addonLineItemsFor(sub.ID, "addon_preview_a"), "preview writes no line items")
	s.Empty(s.addonLineItemsFor(sub.ID, "addon_preview_b"))
	s.Empty(s.oneOffInvoicesFor(sub.ID), "preview raises no invoice")
	s.Empty(s.grantsFromAddon("addon_preview_a"), "preview materialises no credit grants")

	executed, _, err := s.addonChangeService().Execute(ctx, req)
	s.Require().NoError(err)
	s.True(executed.getQuote().NetAmount().Equal(previewed.getQuote().NetAmount()),
		"execute bills exactly what preview quoted")
}

// -----------------------------------------------------------------------------
// request validation
// -----------------------------------------------------------------------------

func (s *SubscriptionServiceSuite) TestAddonBatch_Validate_RejectsUnusableRequests() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()
	svc := s.addonChangeService()

	s.Run("no subscription", func() {
		_, err := svc.Resolve(ctx, AddonChangeRequest{
			Adds: []AddonAdd{s.addEntry("addon_missing", sub.CurrentPeriodStart)},
		})
		s.Error(err)
	})

	s.Run("empty batch", func() {
		_, err := svc.Resolve(ctx, AddonChangeRequest{Subscription: sub})
		s.Error(err)
	})

	s.Run("add without a request", func() {
		_, err := svc.Resolve(ctx, AddonChangeRequest{
			Subscription: sub,
			Adds:         []AddonAdd{{}},
		})
		s.Error(err)
	})
}

// The same addon attached twice in one change is legal — associations are per attach, and the
// schema does not make (subscription, addon) unique.
func (s *SubscriptionServiceSuite) TestAddonBatch_SameAddonTwice_IsAllowed() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.seedFixedPriceAddon("addon_twice", decimal.NewFromInt(20), types.InvoiceCadenceAdvance)

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	config, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_twice", at),
			s.addEntry("addon_twice", at),
		},
	})
	s.Require().NoError(err)

	s.Len(config.getAttaches(), 2, "each entry gets its own association")
	s.NotEqual(config.getAttaches()[0].getAssociation().ID, config.getAttaches()[1].getAssociation().ID)
	s.Len(s.addonLineItemsFor(sub.ID, "addon_twice"), 2, "each association brings its own line item")
	s.Require().Len(s.oneOffInvoicesFor(sub.ID), 1, "still one document for the change")
}

// Re-attaching the same addon in one change must not cancel the grants that change just
// created. Cancellations target by addon id, which both the departing and arriving
// associations share, so the order Apply runs them in is load-bearing.
func (s *SubscriptionServiceSuite) TestAddonBatch_SameAddonReattached_KeepsTheNewCreditGrant() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	s.setupCreditGrantAddon("addon_reattach", 100, types.CreditGrantCadenceRecurring)
	outgoing := s.attachForRemoval("addon_reattach", 0)
	s.Require().Len(s.grantsFromAddon("addon_reattach"), 1, "the first attach materialises one grant")

	at := sub.CurrentPeriodStart.Add(10 * 24 * time.Hour)
	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_reattach", at)},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, at)},
	})
	s.Require().NoError(err)

	live := 0
	for _, g := range s.grantsFromAddon("addon_reattach") {
		if g.EndDate == nil || g.EndDate.After(at) {
			live++
		}
	}
	s.Require().Equal(1, live, "the re-attached addon keeps exactly one grant funding the rest of the cycle")
}

// -----------------------------------------------------------------------------
// future-dating guards
// -----------------------------------------------------------------------------
//
// A future-dated change closes a window now but fixes the successor's quota from the
// predecessor's usage as it stands now, while the predecessor keeps accruing until the
// boundary. Closing nothing is exactly when that gap cannot exist, so that — not the date —
// is what the guard tests.

// markEvaluated stamps the feature's live window as measured, so a close carries it forward
// rather than taking the delete-and-respan branch.
func (s *SubscriptionServiceSuite) markEvaluated(featureID string) {
	row := s.liveRow(featureID)
	s.Require().NotNil(row)
	row.LastComputedAt = lo.ToPtr(s.testData.now.Add(-time.Hour))
	s.Require().NoError(s.GetStores().EntitlementGrantRepo.UpdateSnapshot(s.GetContext(), row))
}

func (s *SubscriptionServiceSuite) TestAddonBatch_FutureAddOntoFundedFeature_IsRejected() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_future_funded")
	s.seedFullFeaturedAddon("addon_funded_first", "ent_funded_first", featureID, 30, 400)
	s.seedFullFeaturedAddon("addon_funded_second", "ent_funded_second", featureID, 30, 900)
	s.attachForRemoval("addon_funded_first", 30)
	s.markEvaluated(featureID)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_funded_second", s.testData.now.Add(10*24*time.Hour))},
	})
	s.Require().Error(err, "the live window would be cut at a boundary its quota is fixed before")
}

func (s *SubscriptionServiceSuite) TestAddonBatch_FutureAddOntoFreshFeature_IsAllowed() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_future_fresh")
	s.seedFullFeaturedAddon("addon_fresh", "ent_fresh", featureID, 30, 900)

	at := s.testData.now.Add(10 * 24 * time.Hour)
	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_fresh", at)},
	})
	s.Require().NoError(err, "nothing is live on the feature, so nothing is cut")

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1)
	s.True(rows[0].ValidFrom.Equal(at), "the window opens on its own date, not today")
}

func (s *SubscriptionServiceSuite) TestAddonBatch_FutureRemoveOfLastConfig_IsAllowed() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_future_last")
	s.seedFullFeaturedAddon("addon_only", "ent_only", featureID, 30, 400)
	outgoing := s.attachForRemoval("addon_only", 30)
	s.markEvaluated(featureID)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Removes: []*dto.RemoveAddonRequest{
			s.removeEntry(outgoing, s.testData.now.Add(10*24*time.Hour)),
		},
	})
	s.Require().NoError(err, "the last config leaving cuts nothing — the window runs out")
}

func (s *SubscriptionServiceSuite) TestAddonBatch_FutureRemoveWithSurvivors_IsRejected() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_future_survivors")
	s.seedFullFeaturedAddon("addon_leaving", "ent_leaving", featureID, 30, 400)
	s.seedFullFeaturedAddon("addon_staying", "ent_staying", featureID, 30, 900)
	outgoing := s.attachForRemoval("addon_leaving", 30)
	s.attachForRemoval("addon_staying", 30)
	s.markEvaluated(featureID)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Removes: []*dto.RemoveAddonRequest{
			s.removeEntry(outgoing, s.testData.now.Add(10*24*time.Hour)),
		},
	})
	s.Require().Error(err, "survivors re-key the pooled row, which carries a quota fixed too early")
}

// An immediate entry sharing a change with a future-dated one still cuts its window at the
// later date, so the guard has to judge the change rather than the entry.
func (s *SubscriptionServiceSuite) TestAddonBatch_ImmediateEntryDraggedByFutureEntry_IsRejected() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	fundedID := s.seedGrantFeature("feat_drag_funded")
	freshID := s.seedGrantFeature("feat_drag_fresh")
	s.seedFullFeaturedAddon("addon_drag_first", "ent_drag_first", fundedID, 30, 400)
	s.seedFullFeaturedAddon("addon_drag_now", "ent_drag_now", fundedID, 30, 500)
	s.seedFullFeaturedAddon("addon_drag_later", "ent_drag_later", freshID, 30, 900)
	s.attachForRemoval("addon_drag_first", 30)
	s.markEvaluated(fundedID)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_drag_now", s.testData.now),
			s.addEntry("addon_drag_later", s.testData.now.Add(10*24*time.Hour)),
		},
	})
	s.Require().Error(err, "the whole change cuts at the later date, dragging the immediate entry")
}

// Different features carry independent windows, so two fresh ones may land on their own dates.
func (s *SubscriptionServiceSuite) TestAddonBatch_TwoFreshFeaturesDifferentDates_IsAllowed() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	firstID := s.seedGrantFeature("feat_indep_first")
	secondID := s.seedGrantFeature("feat_indep_second")
	s.seedFullFeaturedAddon("addon_indep_now", "ent_indep_now", firstID, 30, 400)
	s.seedFullFeaturedAddon("addon_indep_later", "ent_indep_later", secondID, 30, 900)

	later := s.testData.now.Add(10 * 24 * time.Hour)
	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_indep_now", s.testData.now),
			s.addEntry("addon_indep_later", later),
		},
	})
	s.Require().NoError(err)

	second := s.sortedGrantsForFeature(secondID)
	s.Require().Len(second, 1)
	s.True(second[0].ValidFrom.Equal(later), "each feature keeps its own date when nothing is cut")
}

// One pooled row carries one coefficient, so two addons feeding a feature must share an instant.
func (s *SubscriptionServiceSuite) TestAddonBatch_TwoAddsOnOneFeatureDifferentDates_IsRejected() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_pool_clash")
	s.seedFullFeaturedAddon("addon_pool_early", "ent_pool_early", featureID, 30, 400)
	s.seedFullFeaturedAddon("addon_pool_late", "ent_pool_late", featureID, 30, 900)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_pool_early", s.testData.now.Add(5*24*time.Hour)),
			s.addEntry("addon_pool_late", s.testData.now.Add(10*24*time.Hour)),
		},
	})
	s.Require().Error(err, "the later addon's quota would be granted from the earlier one's date")
}

// Two undated adds are simultaneous by definition: the pooling guard must read them as one
// date, not as two clocks read moments apart.
func (s *SubscriptionServiceSuite) TestAddonBatch_TwoUndatedAddsOnOneFeature_Pool() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_pool_undated")
	s.seedFullFeaturedAddon("addon_undated_a", "ent_undated_a", featureID, 30, 400)
	s.seedFullFeaturedAddon("addon_undated_b", "ent_undated_b", featureID, 30, 900)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.undatedEntry("addon_undated_a"),
			s.undatedEntry("addon_undated_b"),
		},
	})
	s.Require().NoError(err)

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "both addons pool into one row")
}

// The same addon twice is the documented way to buy several of it, and both instances land on
// the same feature.
func (s *SubscriptionServiceSuite) TestAddonBatch_SameGrantAddonTwiceUndated_Pools() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_pool_twice")
	s.seedFullFeaturedAddon("addon_twice", "ent_twice", featureID, 30, 400)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.undatedEntry("addon_twice"), s.undatedEntry("addon_twice")},
	})
	s.Require().NoError(err)
}

func (s *SubscriptionServiceSuite) TestAddonBatch_TwoAddsOnOneFeatureSameDate_IsAllowed() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_pool_agree")
	s.seedFullFeaturedAddon("addon_agree_a", "ent_agree_a", featureID, 30, 400)
	s.seedFullFeaturedAddon("addon_agree_b", "ent_agree_b", featureID, 30, 900)

	at := s.testData.now.Add(5 * 24 * time.Hour)
	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_agree_a", at),
			s.addEntry("addon_agree_b", at),
		},
	})
	s.Require().NoError(err)

	rows := s.sortedGrantsForFeature(featureID)
	s.Require().Len(rows, 1, "both addons pool into one row")
	s.True(rows[0].ValidFrom.Equal(at))
}

// -----------------------------------------------------------------------------
// metered reset-period compatibility
// -----------------------------------------------------------------------------

// seedSharedMeteredFeature registers one metered feature two addons can both claim.
func (s *SubscriptionServiceSuite) seedSharedMeteredFeature(featureID string) {
	s.Require().NoError(s.GetStores().FeatureRepo.Create(s.GetContext(), &feature.Feature{
		ID:        featureID,
		Name:      featureID,
		Type:      types.FeatureTypeMetered,
		MeterID:   s.testData.meters.apiCalls.ID,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *SubscriptionServiceSuite) compatOf(req AddonChangeRequest) error {
	return newSubscriptionGrantService(s.service.(*subscriptionService).ServiceParams).
		validateEntitlementCompatibility(s.GetContext(), GrantChangeRequest{
			Sub: req.Subscription,
			Incoming: lo.Map(req.Adds, func(a AddonAdd, _ int) GrantSource {
				at := lo.FromPtr(a.Request.StartDate)
				return GrantSource{
					AddonID:       a.Request.AddonID,
					ChangeType:    grantChangeTypeFor(req.Subscription, at),
					EffectiveDate: at,
					RequestedDate: at,
				}
			}),
			Removed: lo.Map(req.Removes, func(r *dto.RemoveAddonRequest, _ int) GrantSource {
				at := lo.FromPtr(r.EffectiveDate)
				return GrantSource{
					AddonID:       s.addonIDOfAssociation(r.AddonAssociationID),
					ChangeType:    grantChangeTypeFor(req.Subscription, at),
					EffectiveDate: at,
					RequestedDate: at,
				}
			}),
		})
}

func (s *SubscriptionServiceSuite) addonIDOfAssociation(associationID string) string {
	assoc, err := s.GetStores().AddonAssociationRepo.GetByID(s.GetContext(), associationID)
	s.Require().NoError(err)
	return assoc.AddonID
}

// The case the old above-the-spine guard could not express: A leaves and B arrives on the
// same feature in one change, so their disagreeing reset periods never coexist.
// A removal scheduled for the period end still measures its feature until then, so an addon
// that measures it differently cannot arrive before it leaves.
func (s *SubscriptionServiceSuite) TestAddonCompat_PeriodEndRemovalStillHoldsItsFeature() {
	sub := s.testData.subscription
	featureID := "feat_periodend_reset"
	s.seedSharedMeteredFeature(featureID)
	s.seedMeteredAddon("addon_pe_out", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.seedMeteredAddon("addon_pe_in", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_ANNUAL)
	outgoing := s.attachForRemoval("addon_pe_out", 0)

	err := s.compatOf(AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_pe_in", s.testData.now)},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, sub.CurrentPeriodEnd)},
	})
	s.Require().Error(err, "the outgoing addon measures the feature until the period end")
	s.ErrorContains(err, "reset period")
}

// A period-end entry opens no window this cycle, so it cannot collide with an immediate one.
func (s *SubscriptionServiceSuite) TestAddonBatch_PeriodEndAddBesideImmediateAdd_IsAllowed() {
	ctx := s.GetContext()
	sub := s.monthlyPeriodSubscription()

	featureID := s.seedGrantFeature("feat_pool_periodend")
	s.seedFullFeaturedAddon("addon_pe_now", "ent_pe_now", featureID, 30, 400)
	s.seedFullFeaturedAddon("addon_pe_later", "ent_pe_later", featureID, 30, 900)

	_, _, err := s.addonChangeService().Execute(ctx, AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.undatedEntry("addon_pe_now"),
			s.addEntry("addon_pe_later", sub.CurrentPeriodEnd),
		},
	})
	s.Require().NoError(err)
}

func (s *SubscriptionServiceSuite) TestAddonBatch_SwapWithDifferentResetPeriods_IsAllowed() {
	sub := s.testData.subscription
	featureID := "feat_swap_reset"
	s.seedSharedMeteredFeature(featureID)
	s.seedMeteredAddon("addon_reset_out", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.seedMeteredAddon("addon_reset_in", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_ANNUAL)
	outgoing := s.attachForRemoval("addon_reset_out", 0)

	s.NoError(s.compatOf(AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_reset_in", s.testData.now)},
		Removes:      []*dto.RemoveAddonRequest{s.removeEntry(outgoing, s.testData.now)},
	}), "the departing addon's period cannot conflict with the arriving one")
}

// Adding onto a feature an addon still holds is a genuine conflict.
func (s *SubscriptionServiceSuite) TestAddonBatch_AddConflictingWithSurvivor_IsRejected() {
	sub := s.testData.subscription
	featureID := "feat_survivor_reset"
	s.seedSharedMeteredFeature(featureID)
	s.seedMeteredAddon("addon_survivor", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.seedMeteredAddon("addon_intruder", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_ANNUAL)
	s.attachForRemoval("addon_survivor", 0)

	err := s.compatOf(AddonChangeRequest{
		Subscription: sub,
		Adds:         []AddonAdd{s.addEntry("addon_intruder", s.testData.now)},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "reset period")
}

// Two adds can disagree with each other even when nothing on the subscription objects —
// which only a validator folding them one at a time can see.
func (s *SubscriptionServiceSuite) TestAddonBatch_TwoAddsDisagreeingWithEachOther_IsRejected() {
	sub := s.testData.subscription
	featureID := "feat_mutual_reset"
	s.seedSharedMeteredFeature(featureID)
	s.seedMeteredAddon("addon_mutual_monthly", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.seedMeteredAddon("addon_mutual_annual", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_ANNUAL)

	err := s.compatOf(AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_mutual_monthly", s.testData.now),
			s.addEntry("addon_mutual_annual", s.testData.now),
		},
	})
	s.Require().Error(err)
	s.Contains(err.Error(), "reset period")
}

func (s *SubscriptionServiceSuite) TestAddonBatch_TwoAddsAgreeingOnOneFeature_IsAllowed() {
	sub := s.testData.subscription
	featureID := "feat_mutual_agree"
	s.seedSharedMeteredFeature(featureID)
	s.seedMeteredAddon("addon_agree_monthly_a", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)
	s.seedMeteredAddon("addon_agree_monthly_b", featureID, types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY)

	s.NoError(s.compatOf(AddonChangeRequest{
		Subscription: sub,
		Adds: []AddonAdd{
			s.addEntry("addon_agree_monthly_a", s.testData.now),
			s.addEntry("addon_agree_monthly_b", s.testData.now),
		},
	}))
}
