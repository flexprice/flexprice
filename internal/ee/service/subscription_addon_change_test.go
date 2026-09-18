package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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
	ctx := s.GetContext()
	sub := s.testData.subscription

	attached, err := s.service.(*subscriptionService).attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
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
	subSvc := s.service.(*subscriptionService)
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

	_, err = subSvc.attachAddon(ctx, sub, &dto.AddAddonToSubscriptionRequest{
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

	at := sub.CurrentPeriodStart.Add(15 * 24 * time.Hour)
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
