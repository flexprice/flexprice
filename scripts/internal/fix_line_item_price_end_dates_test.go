package internal

import (
	"bytes"
	"encoding/csv"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFixLineItemPriceEndDates(t *testing.T) {
	periodStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cutover := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	ownEnd := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		dryRun     bool
		wantAction string
		wantStored time.Time
	}{
		{name: "dry run writes nothing", dryRun: true, wantAction: "dry_run", wantStored: time.Time{}},
		{name: "apply sets the price end date", dryRun: false, wantAction: "updated", wantStored: cutover},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := testutil.SetupContext()
			envID := types.GetEnvironmentID(ctx)

			planStore := testutil.NewInMemoryPlanStore()
			priceStore := testutil.NewInMemoryPriceStore()
			subStore := testutil.NewInMemorySubscriptionStore()
			lineItemStore := testutil.NewInMemorySubscriptionLineItemStore()
			subStore.SetLineItemStore(lineItemStore)

			require.NoError(t, planStore.Create(ctx, &plan.Plan{
				ID: "plan_1", Name: "Plan 1", EnvironmentID: envID, BaseModel: types.GetDefaultBaseModel(ctx),
			}))
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
				{ID: "sub_active", SubscriptionStatus: types.SubscriptionStatusActive},
				{ID: "sub_cancelled", SubscriptionStatus: types.SubscriptionStatusCancelled},
			} {
				sub.PlanID = "plan_1"
				sub.StartDate = periodStart
				sub.CurrentPeriodStart = periodStart
				sub.EnvironmentID = envID
				sub.BaseModel = types.GetDefaultBaseModel(ctx)
				require.NoError(t, subStore.Create(ctx, sub))
			}

			for _, item := range []*subscription.SubscriptionLineItem{
				{ID: "li_no_end", SubscriptionID: "sub_active", PriceID: "price_ending"},
				{ID: "li_has_end", SubscriptionID: "sub_active", PriceID: "price_ending", EndDate: ownEnd},
				{ID: "li_open_price", SubscriptionID: "sub_active", PriceID: "price_open"},
				{ID: "li_cancelled_sub", SubscriptionID: "sub_cancelled", PriceID: "price_ending"},
			} {
				item.EntityType = types.SubscriptionLineItemEntityTypePlan
				item.EntityID = "plan_1"
				item.StartDate = periodStart
				item.EnvironmentID = envID
				item.BaseModel = types.GetDefaultBaseModel(ctx)
				require.NoError(t, lineItemStore.Create(ctx, item))
			}

			script := &fixLineItemPriceEndDatesScript{
				planRepo:         planStore,
				priceRepo:        priceStore,
				subscriptionRepo: subStore,
				lineItemRepo:     lineItemStore,
			}
			var buf bytes.Buffer
			writer := csv.NewWriter(&buf)
			require.NoError(t, script.run(ctx, writer, types.DefaultUserID, tt.dryRun))
			writer.Flush()

			rows, err := csv.NewReader(&buf).ReadAll()
			require.NoError(t, err)
			actions := make(map[string]string)
			for _, row := range rows[1:] {
				actions[row[4]] = row[9]
			}
			require.Equal(t, map[string]string{
				"li_no_end":  tt.wantAction,
				"li_has_end": "skipped_has_end_date",
			}, actions)

			stored := func(id string) time.Time {
				item, err := lineItemStore.Get(ctx, id)
				require.NoError(t, err)
				return item.EndDate
			}
			require.Equal(t, tt.wantStored, stored("li_no_end"))
			require.Equal(t, ownEnd, stored("li_has_end"))
			require.True(t, stored("li_open_price").IsZero())
			require.True(t, stored("li_cancelled_sub").IsZero())
		})
	}
}
