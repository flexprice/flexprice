package internal

import (
	"bytes"
	"context"
	"encoding/csv"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// storeUnendedLineItemFinder applies the ent finder's conditions to the in-memory stores and records each call.
type storeUnendedLineItemFinder struct {
	lineItems     *testutil.InMemorySubscriptionLineItemStore
	subscriptions *testutil.InMemorySubscriptionStore
	findLimits    []int
	countCalls    []string
}

func (f *storeUnendedLineItemFinder) matching(ctx context.Context, planID, priceID string) ([]*subscription.SubscriptionLineItem, error) {
	items, err := f.lineItems.List(ctx, &types.SubscriptionLineItemFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		PriceIDs:    []string{priceID},
	})
	if err != nil {
		return nil, err
	}
	var matched []*subscription.SubscriptionLineItem
	for _, item := range items {
		if item.Status != types.StatusPublished || !item.EndDate.IsZero() {
			continue
		}
		sub, err := f.subscriptions.Get(ctx, item.SubscriptionID)
		if err != nil {
			return nil, err
		}
		if sub.PlanID == planID && lo.Contains([]types.SubscriptionStatus{
			types.SubscriptionStatusActive, types.SubscriptionStatusTrialing,
		}, sub.SubscriptionStatus) {
			matched = append(matched, item)
		}
	}
	return matched, nil
}

func (f *storeUnendedLineItemFinder) Find(ctx context.Context, planID, priceID string, limit int) ([]*subscription.SubscriptionLineItem, error) {
	f.findLimits = append(f.findLimits, limit)
	matched, err := f.matching(ctx, planID, priceID)
	if err != nil {
		return nil, err
	}
	return lo.Slice(matched, 0, limit), nil
}

func (f *storeUnendedLineItemFinder) Count(ctx context.Context, planID, priceID string) (int, error) {
	f.countCalls = append(f.countCalls, priceID)
	matched, err := f.matching(ctx, planID, priceID)
	return len(matched), err
}

func TestFixLineItemPriceEndDates(t *testing.T) {
	periodStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cutover := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	futureStart := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	ownEnd := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	setup := func(t *testing.T) (context.Context, *testutil.InMemorySubscriptionLineItemStore, *storeUnendedLineItemFinder, *fixLineItemPriceEndDatesScript) {
		ctx := testutil.SetupContext()
		envID := types.GetEnvironmentID(ctx)

		planStore := testutil.NewInMemoryPlanStore()
		priceStore := testutil.NewInMemoryPriceStore()
		subStore := testutil.NewInMemorySubscriptionStore()
		lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()

		for _, p := range []*plan.Plan{{ID: "plan_1", Name: "Plan 1"}, {ID: "plan_2", Name: "Plan 2"}} {
			p.EnvironmentID = envID
			p.BaseModel = types.GetDefaultBaseModel(ctx)
			require.NoError(t, planStore.Create(ctx, p))
		}
		for _, p := range []*price.Price{
			{ID: "price_ending", StartDate: &periodStart, EndDate: &cutover},
			{ID: "price_open", StartDate: &periodStart},
		} {
			p.EntityType = types.PRICE_ENTITY_TYPE_PLAN
			p.EntityID = "plan_1"
			p.EnvironmentID = envID
			p.BaseModel = types.GetDefaultBaseModel(ctx)
			require.NoError(t, priceStore.Create(ctx, p))
		}

		for _, sub := range []*subscription.Subscription{
			{ID: "sub_active", PlanID: "plan_1", SubscriptionStatus: types.SubscriptionStatusActive},
			{ID: "sub_trialing", PlanID: "plan_1", SubscriptionStatus: types.SubscriptionStatusTrialing},
			{ID: "sub_cancelled", PlanID: "plan_1", SubscriptionStatus: types.SubscriptionStatusCancelled},
			{ID: "sub_other_plan", PlanID: "plan_2", SubscriptionStatus: types.SubscriptionStatusActive},
		} {
			sub.StartDate = periodStart
			sub.CurrentPeriodStart = periodStart
			sub.EnvironmentID = envID
			sub.BaseModel = types.GetDefaultBaseModel(ctx)
			require.NoError(t, subStore.Create(ctx, sub))
		}

		for i, item := range []*subscription.SubscriptionLineItem{
			{ID: "li_no_end", SubscriptionID: "sub_active", PriceID: "price_ending"},
			{ID: "li_has_end", SubscriptionID: "sub_active", PriceID: "price_ending", EndDate: ownEnd},
			{ID: "li_future_start", SubscriptionID: "sub_active", PriceID: "price_ending", StartDate: futureStart},
			{ID: "li_trialing_sub", SubscriptionID: "sub_trialing", PriceID: "price_ending"},
			{ID: "li_open_price", SubscriptionID: "sub_active", PriceID: "price_open"},
			{ID: "li_cancelled_sub", SubscriptionID: "sub_cancelled", PriceID: "price_ending"},
			{ID: "li_other_plan", SubscriptionID: "sub_other_plan", PriceID: "price_ending"},
		} {
			item.EntityType = types.SubscriptionLineItemEntityTypePlan
			item.EntityID = "plan_1"
			if item.StartDate.IsZero() {
				item.StartDate = periodStart
			}
			item.EnvironmentID = envID
			item.BaseModel = types.GetDefaultBaseModel(ctx)
			// Distinct created times keep the in-memory store's batch order stable
			item.CreatedAt = periodStart.Add(time.Duration(i) * time.Minute)
			require.NoError(t, lineItemStore.Create(ctx, item))
		}

		finder := &storeUnendedLineItemFinder{lineItems: lineItemStore, subscriptions: subStore}
		script := &fixLineItemPriceEndDatesScript{
			planRepo:     planStore,
			priceRepo:    priceStore,
			lineItemRepo: lineItemStore,
			finder:       finder,
			// One line item per batch, so the loop has to run until nothing is left
			batchSize: 1,
		}
		return ctx, lineItemStore, finder, script
	}

	runReport := func(t *testing.T, ctx context.Context, script *fixLineItemPriceEndDatesScript, dryRun bool) ([][]string, error) {
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		runErr := script.run(ctx, writer, types.DefaultUserID, dryRun)
		writer.Flush()
		rows, err := csv.NewReader(&buf).ReadAll()
		require.NoError(t, err)
		return rows[1:], runErr
	}

	stored := func(t *testing.T, ctx context.Context, store *testutil.InMemorySubscriptionLineItemStore, id string) time.Time {
		item, err := store.Get(ctx, id)
		require.NoError(t, err)
		return item.EndDate
	}

	t.Run("dry run counts per price and writes nothing", func(t *testing.T) {
		ctx, lineItemStore, finder, script := setup(t)

		rows, err := runReport(t, ctx, script, true)
		require.NoError(t, err)
		require.Equal(t, [][]string{
			{"Plan 1", "plan_1", "price_ending", periodStart.Format(time.RFC3339), cutover.Format(time.RFC3339), "3"},
		}, rows)
		require.Equal(t, []string{"price_ending"}, finder.countCalls)
		require.Empty(t, finder.findLimits)

		for _, id := range []string{"li_no_end", "li_future_start", "li_trialing_sub", "li_cancelled_sub", "li_other_plan", "li_open_price"} {
			require.True(t, stored(t, ctx, lineItemStore, id).IsZero(), id)
		}
		require.Equal(t, ownEnd, stored(t, ctx, lineItemStore, "li_has_end"))
	})

	t.Run("apply fixes batch by batch until none are left", func(t *testing.T) {
		ctx, lineItemStore, finder, script := setup(t)

		rows, err := runReport(t, ctx, script, false)
		require.NoError(t, err)
		actions := make(map[string]string)
		for _, row := range rows {
			actions[row[4]] = row[9]
		}
		require.Len(t, rows, 3)
		require.Equal(t, map[string]string{
			"li_no_end":       "updated",
			"li_trialing_sub": "updated",
			"li_future_start": "updated",
		}, actions)
		// Three batches of one, then an empty read that ends the loop
		require.Equal(t, []int{1, 1, 1, 1}, finder.findLimits)
		require.Empty(t, finder.countCalls)

		require.Equal(t, cutover, stored(t, ctx, lineItemStore, "li_no_end"))
		require.Equal(t, cutover, stored(t, ctx, lineItemStore, "li_trialing_sub"))
		// A line starting after the price ended is clamped to its own start
		require.Equal(t, futureStart, stored(t, ctx, lineItemStore, "li_future_start"))
		require.Equal(t, ownEnd, stored(t, ctx, lineItemStore, "li_has_end"))
		for _, id := range []string{"li_open_price", "li_cancelled_sub", "li_other_plan"} {
			require.True(t, stored(t, ctx, lineItemStore, id).IsZero(), id)
		}

		rerun, err := runReport(t, ctx, script, false)
		require.NoError(t, err)
		require.Empty(t, rerun)
	})

}
