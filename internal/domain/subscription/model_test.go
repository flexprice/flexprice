package subscription

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

// TestHasMixedBillingPeriods implements PRD Appendix E.2.1 HasMixedBillingPeriods test cases.
func TestHasMixedBillingPeriods(t *testing.T) {
	makeItems := func(periods ...types.BillingPeriod) []*SubscriptionLineItem {
		items := make([]*SubscriptionLineItem, len(periods))
		for i, p := range periods {
			items[i] = &SubscriptionLineItem{BillingPeriod: p}
		}
		return items
	}

	tests := []struct {
		name     string
		periods  []types.BillingPeriod
		expected bool
		reason   string
	}{
		{"empty", nil, false, "Empty"},
		{"single_M", []types.BillingPeriod{types.BILLING_PERIOD_MONTHLY}, false, "Single item"},
		{"M_M", []types.BillingPeriod{types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_MONTHLY}, false, "All same"},
		{"M_M_M", []types.BillingPeriod{types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_MONTHLY}, false, "All same"},
		{"M_Q", []types.BillingPeriod{types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_QUARTER}, true, "Different"},
		{"M_Q_H", []types.BillingPeriod{types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_QUARTER, types.BILLING_PERIOD_HALF_YEAR}, true, "Multiple different"},
		{"Q_Q_Q", []types.BillingPeriod{types.BILLING_PERIOD_QUARTER, types.BILLING_PERIOD_QUARTER, types.BILLING_PERIOD_QUARTER}, false, "All same"},
		{"M_M_Q", []types.BillingPeriod{types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_QUARTER}, true, "Two same, one different"},
		{"A_A", []types.BillingPeriod{types.BILLING_PERIOD_ANNUAL, types.BILLING_PERIOD_ANNUAL}, false, "All same"},
		{"M_A", []types.BillingPeriod{types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_ANNUAL}, true, "Min and max"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := makeItems(tt.periods...)
			got := hasMixedBillingPeriods(items)
			if got != tt.expected {
				t.Errorf("hasMixedBillingPeriods(%v) = %v, want %v (%s)", tt.periods, got, tt.expected, tt.reason)
			}
			// Also test via Subscription when we have at least one item
			if len(items) > 0 {
				sub := &Subscription{LineItems: items}
				gotSub := sub.HasMixedBillingPeriods()
				if gotSub != tt.expected {
					t.Errorf("sub.HasMixedBillingPeriods() = %v, want %v", gotSub, tt.expected)
				}
			}
		})
	}
}

// TestHasMixedBillingPeriods_nil_safe ensures nil/empty subscription is safe.
func TestHasMixedBillingPeriods_nil_safe(t *testing.T) {
	sub := &Subscription{LineItems: nil}
	if sub.HasMixedBillingPeriods() {
		t.Error("nil LineItems should return false")
	}
	sub.LineItems = []*SubscriptionLineItem{}
	if sub.HasMixedBillingPeriods() {
		t.Error("empty LineItems should return false")
	}
}
