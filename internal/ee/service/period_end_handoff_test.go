package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPeriodEndHandoff(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	item := func(id, meterID string, from, to time.Time) *subscription.SubscriptionLineItem {
		return &subscription.SubscriptionLineItem{ID: id, MeterID: meterID, PriceType: types.PRICE_TYPE_USAGE, StartDate: from, EndDate: to}
	}
	onlySignedSum := func(i *subscription.SubscriptionLineItem) bool { return i.MeterID == "meter_net" }

	tests := []struct {
		name           string
		items          []*subscription.SubscriptionLineItem
		wantSuperseded []string
		wantStart      map[string]time.Time
	}{
		{
			name:      "single line item keeps its own window",
			items:     []*subscription.SubscriptionLineItem{item("li_a", "meter_net", start, time.Time{})},
			wantStart: map[string]time.Time{"li_a": start},
		},
		{
			name:           "a change hands the whole window to the new line item",
			items:          []*subscription.SubscriptionLineItem{item("li_old", "meter_net", start, mid), item("li_new", "meter_net", mid, time.Time{})},
			wantSuperseded: []string{"li_old"},
			wantStart:      map[string]time.Time{"li_new": start},
		},
		{
			name:      "overlapping line items are concurrent charges and both bill",
			items:     []*subscription.SubscriptionLineItem{item("li_plan", "meter_net", start, time.Time{}), item("li_addon", "meter_net", mid, time.Time{})},
			wantStart: map[string]time.Time{"li_plan": start, "li_addon": mid},
		},
		{
			name:      "other meters are untouched",
			items:     []*subscription.SubscriptionLineItem{item("li_old", "meter_sum", start, mid), item("li_new", "meter_sum", mid, time.Time{})},
			wantStart: map[string]time.Time{"li_old": start, "li_new": mid},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newPeriodEndHandoff(tt.items, onlySignedSum, start, end)
			for _, it := range tt.items {
				superseded := false
				for _, id := range tt.wantSuperseded {
					superseded = superseded || id == it.ID
				}
				require.Equal(t, superseded, h.IsSuperseded(it.ID), "superseded %s", it.ID)
				if want, ok := tt.wantStart[it.ID]; ok {
					require.Equal(t, want, h.UsageStart(it, start), "usage start %s", it.ID)
				}
			}
		})
	}
}
