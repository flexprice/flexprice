package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type AddonPriceSyncSuite struct {
	testutil.BaseServiceTestSuite
	params ServiceParams
	svc    *addonService
}

func TestAddonPriceSyncSuite(t *testing.T) {
	suite.Run(t, new(AddonPriceSyncSuite))
}

func (s *AddonPriceSyncSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.params = ServiceParams{
		Logger:                   s.GetLogger(),
		Config:                   s.GetConfig(),
		DB:                       s.GetDB(),
		AddonRepo:                s.GetStores().AddonRepo,
		AddonAssociationRepo:     s.GetStores().AddonAssociationRepo,
		AddonPriceSyncRepo:       s.GetStores().AddonPriceSyncRepo,
		PriceRepo:                s.GetStores().PriceRepo,
		SubRepo:                  s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo: s.GetStores().SubscriptionLineItemRepo,
	}
	s.svc = &addonService{ServiceParams: s.params}
}

// --- fixture helpers --------------------------------------------------------

var (
	tPast30 = time.Now().UTC().AddDate(0, 0, -30)
	tPast1  = time.Now().UTC().AddDate(0, 0, -1)
)

func (s *AddonPriceSyncSuite) mkAddon(id string) {
	s.NoError(s.GetStores().AddonRepo.Create(s.GetContext(), &addon.Addon{
		ID:        id,
		Name:      id,
		BaseModel: types.GetDefaultBaseModel(s.GetContext()),
	}))
}

// mkPrice creates a published addon-scoped price. endDate nil means open-ended.
func (s *AddonPriceSyncSuite) mkPrice(id, addonID string, seq int64, ptype types.PriceType, endDate *time.Time, parent string) {
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:                 id,
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           addonID,
		Type:               ptype,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		Sequence:           seq,
		EndDate:            endDate,
		ParentPriceID:      parent,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}))
}

// mkSubOverridePrice creates a subscription-scoped override price tracing to root.
func (s *AddonPriceSyncSuite) mkSubOverridePrice(id, subID, root string, seq int64) {
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:                 id,
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_SUBSCRIPTION,
		EntityID:           subID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		Sequence:           seq,
		ParentPriceID:      root,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *AddonPriceSyncSuite) mkSub(id string, status types.SubscriptionStatus) {
	s.NoError(s.GetStores().SubscriptionRepo.Create(s.GetContext(), &subscription.Subscription{
		ID:                 id,
		CustomerID:         "cust-1",
		SubscriptionStatus: status,
		StartDate:          tPast30,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *AddonPriceSyncSuite) mkAssoc(id, addonID, subID string, syncedSeq int64, status types.AddonStatus, endDate *time.Time) {
	s.NoError(s.GetStores().AddonAssociationRepo.Create(s.GetContext(), &addonassociation.AddonAssociation{
		ID:                  id,
		AddonID:             addonID,
		EntityID:            subID,
		EntityType:          types.AddonAssociationEntityTypeSubscription,
		AddonStatus:         status,
		EndDate:             endDate,
		SyncedPriceSequence: syncedSeq,
		BaseModel:           types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *AddonPriceSyncSuite) mkLineItem(id, addonID, subID, assocID, priceID string, start, end time.Time) {
	s.NoError(s.GetStores().SubscriptionLineItemRepo.Create(s.GetContext(), &subscription.SubscriptionLineItem{
		ID:                 id,
		SubscriptionID:     subID,
		CustomerID:         "cust-1",
		EntityID:           addonID,
		EntityType:         types.SubscriptionLineItemEntityTypeAddon,
		PriceID:            priceID,
		PriceType:          types.PRICE_TYPE_USAGE,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		StartDate:          start,
		EndDate:            end,
		AddonAssociationID: lo.ToPtr(assocID),
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}))
}

func (s *AddonPriceSyncSuite) lineItems(subID string) []*subscription.SubscriptionLineItem {
	f := types.NewNoLimitSubscriptionLineItemFilter()
	f.SubscriptionIDs = []string{subID}
	f.EntityType = lo.ToPtr(types.SubscriptionLineItemEntityTypeAddon)
	f.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	items, err := s.GetStores().SubscriptionLineItemRepo.List(s.GetContext(), f)
	s.NoError(err)
	return items
}

// activePriceIDs returns the set of price ids with a live (zero end_date) line item for the sub.
func (s *AddonPriceSyncSuite) activePriceIDs(subID string) map[string]bool {
	out := map[string]bool{}
	for _, li := range s.lineItems(subID) {
		if li.EndDate.IsZero() {
			out[li.PriceID] = true
		}
	}
	return out
}

// endedPriceIDs returns the set of price ids with an ended (non-zero end_date) line item for the sub.
func (s *AddonPriceSyncSuite) endedPriceIDs(subID string) map[string]bool {
	out := map[string]bool{}
	for _, li := range s.lineItems(subID) {
		if !li.EndDate.IsZero() {
			out[li.PriceID] = true
		}
	}
	return out
}

func (s *AddonPriceSyncSuite) watermark(assocID string) int64 {
	aa, err := s.GetStores().AddonAssociationRepo.GetByID(s.GetContext(), assocID)
	s.NoError(err)
	return aa.SyncedPriceSequence
}

// --- tests ------------------------------------------------------------------

// TC-1: override a usage charge on add-on A → both subscriptions holding A reconcile.
func (s *AddonPriceSyncSuite) TestOverrideUsageCharge_SyncsAllHolders() {
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "") // overridden (ended)
	s.mkPrice("a-p2", "addon-a", 3, types.PRICE_TYPE_USAGE, nil, "a-p1")          // successor
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkSub("sub-b", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkAssoc("aa-b", "addon-a", "sub-b", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-a1", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, time.Time{})
	s.mkLineItem("li-b1", "addon-a", "sub-b", "aa-b", "a-p1", tPast30, time.Time{})

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	s.Equal(2, resp.Summary.LineItemsCreated)
	s.Equal(2, resp.Summary.LineItemsTerminated)

	for _, sub := range []string{"sub-a", "sub-b"} {
		s.True(s.activePriceIDs(sub)["a-p2"], "successor active on %s", sub)
		s.True(s.endedPriceIDs(sub)["a-p1"], "old charge ended on %s", sub)
		s.False(s.activePriceIDs(sub)["a-p1"], "old charge no longer active on %s", sub)
	}
	s.EqualValues(3, s.watermark("aa-a"))
	s.EqualValues(3, s.watermark("aa-b"))
}

// Variant 1: a charge already has a future-dated timeline on the subscription (an
// item ending in the future for the old price, a live item for the current price).
// A later update plus re-sync must extend the timeline, not duplicate it: the new
// price gets one item, the already-ended item is untouched, and the live item is
// end-dated at its price's end date.
func (s *AddonPriceSyncSuite) TestFutureDatedTimeline_ExtendedNotDuplicated() {
	tFut1 := time.Now().UTC().AddDate(0, 0, 10)
	tFut2 := time.Now().UTC().AddDate(0, 0, 20)

	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 1, types.PRICE_TYPE_USAGE, lo.ToPtr(tFut1), "")     // root, ends in the future
	s.mkPrice("a-p2", "addon-a", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tFut2), "a-p1") // successor, now also ended (future)
	s.mkPrice("a-p3", "addon-a", 3, types.PRICE_TYPE_USAGE, nil, "a-p1")             // current, open-ended
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	// Pre-existing timeline: p1 item already ended at tFut1, p2 item still live.
	s.mkLineItem("li-p1", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, tFut1)
	s.mkLineItem("li-p2", "addon-a", "sub-a", "aa-a", "a-p2", tFut1, time.Time{})

	_, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)

	// Exactly one line item per price: the timeline is extended, never duplicated.
	counts := map[string]int{}
	var p2Item *subscription.SubscriptionLineItem
	for _, li := range s.lineItems("sub-a") {
		counts[li.PriceID]++
		if li.PriceID == "a-p2" {
			p2Item = li
		}
	}
	s.Equal(1, counts["a-p1"], "already-ended item not duplicated")
	s.Equal(1, counts["a-p2"], "live item not duplicated")
	s.Equal(1, counts["a-p3"], "current price gets exactly one new item")

	// p1 was already ended at tFut1 and must be left untouched; p3 is the new active item.
	s.True(s.endedPriceIDs("sub-a")["a-p1"])
	s.True(s.activePriceIDs("sub-a")["a-p3"])

	// The live p2 item is end-dated at GREATEST(item.start = tFut1, price.end = tFut2) = tFut2.
	s.Require().NotNil(p2Item)
	s.True(p2Item.EndDate.Equal(tFut2), "live item end-dated at its price's end date, got %s", p2Item.EndDate)

	s.EqualValues(3, s.watermark("aa-a"))
}

// The subscription has its own future-dated override of the charge ($1 now, $5 from day 5,
// a subscription-scoped price). The addon's root is then changed ($1 -> $10) and synced.
// Because the subscription overrode this charge, the addon's new price is NOT pushed: no new
// line item is created, and the existing override timeline is left untouched.
func (s *AddonPriceSyncSuite) TestFutureDatedSubscriptionOverride_AddonChangeSkipped() {
	tFut := time.Now().UTC().AddDate(0, 0, 5)

	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 1, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "") // root, ended by the addon change
	s.mkPrice("a-p2", "addon-a", 2, types.PRICE_TYPE_USAGE, nil, "a-p1")          // addon's new price ($10)
	s.mkSubOverridePrice("sub-ovr", "sub-a", "a-p1", 3)                           // subscription override ($5), traces to root
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	// Existing timeline from the manual edit: original item ends at day 5, override item starts then.
	s.mkLineItem("li-root", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, tFut)
	s.mkLineItem("li-ovr", "addon-a", "sub-a", "aa-a", "sub-ovr", tFut, time.Time{})

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)

	// The addon's new price is skipped because the subscription overrode this charge.
	s.Equal(0, resp.Summary.LineItemsCreated, "override preserved: no new line item")
	s.Equal(0, resp.Summary.LineItemsTerminated, "nothing terminated")
	s.False(s.activePriceIDs("sub-a")["a-p2"], "the addon's $10 price must not be pushed onto an overridden subscription")

	// The existing timeline is untouched.
	s.True(s.endedPriceIDs("sub-a")["a-p1"], "original item keeps its day-5 end date")
	s.True(s.activePriceIDs("sub-a")["sub-ovr"], "the override item is left live")

	// The association is still stamped so it is not rediscovered as behind.
	s.EqualValues(2, s.watermark("aa-a"))
}

// An addon cancelled at period end (cancelled status, future end date) is still billing,
// so a price change must reach it: the old line is shortened to the ended price and a new
// line is created capped at the cancellation date, with no overlap.
func (s *AddonPriceSyncSuite) TestCancelAtPeriodEnd_ShortensOldAndCapsNew() {
	tEnd := time.Now().UTC().AddDate(0, 0, 16) // cancellation date, in the future

	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 1, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "") // root, ended by the addon change
	s.NoError(s.GetStores().PriceRepo.Create(s.GetContext(), &price.Price{
		ID:                 "a-p2",
		Amount:             decimal.NewFromInt(1),
		Currency:           "usd",
		EntityType:         types.PRICE_ENTITY_TYPE_ADDON,
		EntityID:           "addon-a",
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		Sequence:           2,
		ParentPriceID:      "a-p1",
		StartDate:          lo.ToPtr(tPast1), // successor starts where the root ended
		BaseModel:          types.GetDefaultBaseModel(s.GetContext()),
	}))

	s.mkSub("sub-c", types.SubscriptionStatusActive)
	// Cancel at period end: cancelled status, future end date, still billing until then.
	s.mkAssoc("aa-c", "addon-a", "sub-c", 0, types.AddonStatusCancelled, lo.ToPtr(tEnd))
	// Old line already end-dated at the cancellation date (set when the cancel was scheduled).
	s.mkLineItem("li-old", "addon-a", "sub-c", "aa-c", "a-p1", tPast30, tEnd)

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	s.Equal(1, resp.Summary.LineItemsCreated, "new price line created for the still-billing cancelled addon")
	s.Equal(1, resp.Summary.LineItemsTerminated, "old line shortened to the ended price")

	var oldItem, newItem *subscription.SubscriptionLineItem
	for _, li := range s.lineItems("sub-c") {
		switch li.PriceID {
		case "a-p1":
			oldItem = li
		case "a-p2":
			newItem = li
		}
	}
	s.Require().NotNil(oldItem)
	s.Require().NotNil(newItem)
	// Old line shortened from the cancellation date back to the price's end, so it does
	// not overlap the new line.
	s.True(oldItem.EndDate.Equal(tPast1), "old line ends at the ended price, got %s", oldItem.EndDate)
	// New line capped at the cancellation date so it never bills past the cancellation.
	s.True(newItem.EndDate.Equal(tEnd), "new line capped at the association end, got %s", newItem.EndDate)
	s.EqualValues(2, s.watermark("aa-c"))
}

// TC-2: add a new usage charge on add-on A → a line item is created on every holder.
func (s *AddonPriceSyncSuite) TestNewUsageCharge_CreatedOnAllHolders() {
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 1, types.PRICE_TYPE_USAGE, nil, "")
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkSub("sub-b", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkAssoc("aa-b", "addon-a", "sub-b", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-a1", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, time.Time{})
	s.mkLineItem("li-b1", "addon-a", "sub-b", "aa-b", "a-p1", tPast30, time.Time{})

	// New usage charge added.
	s.mkPrice("a-p3", "addon-a", 5, types.PRICE_TYPE_USAGE, nil, "")

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	s.Equal(2, resp.Summary.LineItemsCreated)
	s.Equal(0, resp.Summary.LineItemsTerminated)

	for _, sub := range []string{"sub-a", "sub-b"} {
		s.True(s.activePriceIDs(sub)["a-p1"], "existing charge still active on %s", sub)
		s.True(s.activePriceIDs(sub)["a-p3"], "new charge created on %s", sub)
	}
}

// TC-3: changing add-on B reconciles only its holders; add-on A's subs are untouched.
func (s *AddonPriceSyncSuite) TestCrossAddonIsolation() {
	// add-on A on sub-a and sub-b; add-on B on sub-b only.
	s.mkAddon("addon-a")
	s.mkAddon("addon-b")
	s.mkPrice("a-p1", "addon-a", 1, types.PRICE_TYPE_USAGE, nil, "")
	s.mkPrice("b-p1", "addon-b", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "") // B overridden
	s.mkPrice("b-p2", "addon-b", 3, types.PRICE_TYPE_USAGE, nil, "b-p1")
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkSub("sub-b", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkAssoc("aa-ab", "addon-a", "sub-b", 1, types.AddonStatusActive, nil)
	s.mkAssoc("aa-bb", "addon-b", "sub-b", 2, types.AddonStatusActive, nil)
	s.mkLineItem("li-a", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, time.Time{})
	s.mkLineItem("li-ab", "addon-a", "sub-b", "aa-ab", "a-p1", tPast30, time.Time{})
	s.mkLineItem("li-bb", "addon-b", "sub-b", "aa-bb", "b-p1", tPast30, time.Time{})

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-b")
	s.NoError(err)
	s.Equal(1, resp.Summary.LineItemsCreated)
	s.Equal(1, resp.Summary.LineItemsTerminated)

	// add-on A untouched everywhere.
	s.True(s.activePriceIDs("sub-a")["a-p1"])
	s.True(s.activePriceIDs("sub-b")["a-p1"])
	s.EqualValues(1, s.watermark("aa-a"))
	s.EqualValues(1, s.watermark("aa-ab"))
	// add-on B reconciled on sub-b.
	s.True(s.activePriceIDs("sub-b")["b-p2"])
	s.True(s.endedPriceIDs("sub-b")["b-p1"])
	s.EqualValues(3, s.watermark("aa-bb"))
}

// TC-4: fixed charges are never synced; a new fixed charge creates no line item.
func (s *AddonPriceSyncSuite) TestFixedCharge_NotSynced() {
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 1, types.PRICE_TYPE_USAGE, nil, "")
	s.mkPrice("a-f1", "addon-a", 2, types.PRICE_TYPE_FIXED, nil, "") // existing fixed
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-a1", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, time.Time{})

	// Add a new usage charge AND a new fixed charge.
	s.mkPrice("a-p3", "addon-a", 5, types.PRICE_TYPE_USAGE, nil, "")
	s.mkPrice("a-f2", "addon-a", 6, types.PRICE_TYPE_FIXED, nil, "")

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	// Only the usage charge is created; neither fixed charge is.
	s.Equal(1, resp.Summary.LineItemsCreated)
	s.True(s.activePriceIDs("sub-a")["a-p3"])
	s.False(s.activePriceIDs("sub-a")["a-f1"])
	s.False(s.activePriceIDs("sub-a")["a-f2"])
}

// TC-5: multi-charge add-on, one charge overridden → only that charge's line item is replaced.
func (s *AddonPriceSyncSuite) TestMultiCharge_OnlyChangedReplaced() {
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "") // overridden
	s.mkPrice("a-p2", "addon-a", 3, types.PRICE_TYPE_USAGE, nil, "a-p1")          // successor
	s.mkPrice("a-q1", "addon-a", 1, types.PRICE_TYPE_USAGE, nil, "")              // unchanged charge
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-p1", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, time.Time{})
	s.mkLineItem("li-q1", "addon-a", "sub-a", "aa-a", "a-q1", tPast30, time.Time{})

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	s.Equal(1, resp.Summary.LineItemsCreated)
	s.Equal(1, resp.Summary.LineItemsTerminated)

	active := s.activePriceIDs("sub-a")
	s.True(active["a-p2"], "successor active")
	s.True(active["a-q1"], "unchanged charge still active")
	s.False(active["a-p1"], "overridden charge no longer active")
	s.True(s.endedPriceIDs("sub-a")["a-p1"])
	s.False(s.endedPriceIDs("sub-a")["a-q1"], "unchanged charge not ended")
}

// TC-6: a subscription that overrode the charge keeps its override; others get the successor.
func (s *AddonPriceSyncSuite) TestPerSubscriptionOverride_Preserved() {
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "") // catalog, overridden
	s.mkPrice("a-p2", "addon-a", 3, types.PRICE_TYPE_USAGE, nil, "a-p1")          // successor
	s.mkSubOverridePrice("ov-a", "sub-a", "a-p1", 4)                              // sub-a overrides the charge
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkSub("sub-b", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkAssoc("aa-b", "addon-a", "sub-b", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-a-ov", "addon-a", "sub-a", "aa-a", "ov-a", tPast30, time.Time{}) // sub-a line item on its override
	s.mkLineItem("li-b1", "addon-a", "sub-b", "aa-b", "a-p1", tPast30, time.Time{})   // sub-b on catalog

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)

	// sub-a keeps its override, no successor line item, nothing terminated for it.
	s.True(s.activePriceIDs("sub-a")["ov-a"], "override still active")
	s.False(s.activePriceIDs("sub-a")["a-p2"], "successor NOT pushed onto overriding sub")
	// sub-b gets the successor and loses the old catalog charge.
	s.True(s.activePriceIDs("sub-b")["a-p2"])
	s.True(s.endedPriceIDs("sub-b")["a-p1"])
	s.Equal(1, resp.Summary.LineItemsCreated)
}

// TC-7: re-running the sync is a no-op once associations are stamped.
func (s *AddonPriceSyncSuite) TestIdempotent() {
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "")
	s.mkPrice("a-p2", "addon-a", 3, types.PRICE_TYPE_USAGE, nil, "a-p1")
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-a1", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, time.Time{})

	first, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	s.Equal(1, first.Summary.LineItemsCreated)
	s.Equal(1, first.Summary.LineItemsTerminated)

	second, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	s.Equal(0, second.Summary.LineItemsCreated)
	s.Equal(0, second.Summary.LineItemsTerminated)
}

// TC-8: one-time, cancelled, and non-active-subscription associations are skipped.
func (s *AddonPriceSyncSuite) TestScopeExclusions() {
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "")
	s.mkPrice("a-p2", "addon-a", 3, types.PRICE_TYPE_USAGE, nil, "a-p1")

	// one-time association (EndDate set) on an active sub.
	s.mkSub("sub-onetime", types.SubscriptionStatusActive)
	s.mkAssoc("aa-onetime", "addon-a", "sub-onetime", 1, types.AddonStatusActive, lo.ToPtr(tPast1))
	s.mkLineItem("li-onetime", "addon-a", "sub-onetime", "aa-onetime", "a-p1", tPast30, tPast1)

	// cancelled association on an active sub.
	s.mkSub("sub-cancelled", types.SubscriptionStatusActive)
	s.mkAssoc("aa-cancelled", "addon-a", "sub-cancelled", 1, types.AddonStatusCancelled, nil)
	s.mkLineItem("li-cancelled", "addon-a", "sub-cancelled", "aa-cancelled", "a-p1", tPast30, time.Time{})

	// active association on a cancelled subscription.
	s.mkSub("sub-inactive", types.SubscriptionStatusCancelled)
	s.mkAssoc("aa-inactive", "addon-a", "sub-inactive", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-inactive", "addon-a", "sub-inactive", "aa-inactive", "a-p1", tPast30, time.Time{})

	resp, err := s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)
	s.Equal(0, resp.Summary.LineItemsCreated)
	s.Equal(0, resp.Summary.LineItemsTerminated)

	s.False(s.activePriceIDs("sub-cancelled")["a-p2"])
	s.False(s.activePriceIDs("sub-inactive")["a-p2"])
	s.EqualValues(1, s.watermark("aa-cancelled"))
	s.EqualValues(1, s.watermark("aa-inactive"))
}

// TC-9: sync status reports the unsynced count before a sync and zero after.
func (s *AddonPriceSyncSuite) TestSyncStatus() {
	// No usage prices → synced.
	s.mkAddon("addon-empty")
	st, err := s.svc.addonPriceSyncStatus(s.GetContext(), "addon-empty")
	s.NoError(err)
	s.True(st.Synced)
	s.EqualValues(0, st.UnsyncedAssociationCount)

	// A behind association → not synced.
	s.mkAddon("addon-a")
	s.mkPrice("a-p1", "addon-a", 2, types.PRICE_TYPE_USAGE, lo.ToPtr(tPast1), "")
	s.mkPrice("a-p2", "addon-a", 3, types.PRICE_TYPE_USAGE, nil, "a-p1")
	s.mkSub("sub-a", types.SubscriptionStatusActive)
	s.mkAssoc("aa-a", "addon-a", "sub-a", 1, types.AddonStatusActive, nil)
	s.mkLineItem("li-a1", "addon-a", "sub-a", "aa-a", "a-p1", tPast30, time.Time{})

	st, err = s.svc.addonPriceSyncStatus(s.GetContext(), "addon-a")
	s.NoError(err)
	s.False(st.Synced)
	s.EqualValues(1, st.UnsyncedAssociationCount)
	s.EqualValues(3, st.CurrentSequence)

	_, err = s.svc.SyncAddonPrices(s.GetContext(), "addon-a")
	s.NoError(err)

	st, err = s.svc.addonPriceSyncStatus(s.GetContext(), "addon-a")
	s.NoError(err)
	s.True(st.Synced)
	s.EqualValues(0, st.UnsyncedAssociationCount)
}
