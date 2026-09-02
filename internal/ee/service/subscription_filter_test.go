package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
)

// mkPrice builds a minimal *dto.PriceResponse for filter tests.
func mkPrice(id string, period types.BillingPeriod, count int) *dto.PriceResponse {
	return &dto.PriceResponse{
		Price: &price.Price{
			ID:                 id,
			Currency:           "usd",
			BillingPeriod:      period,
			BillingPeriodCount: count,
		},
	}
}

func mkSub(period types.BillingPeriod, count int) *subscription.Subscription {
	return &subscription.Subscription{
		Currency:           "usd",
		BillingPeriod:      period,
		BillingPeriodCount: count,
	}
}

func idsOf(ps []*dto.PriceResponse) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Price.ID)
	}
	return out
}

func TestFilterValidPricesForSubscription_IncludeIDsSemantics(t *testing.T) {
	// Plan prices: one exact-cadence (quarterly), one divisor (monthly), one
	// currency-mismatch, one ONETIME, one non-divisor (annual, on a quarterly sub).
	prices := []*dto.PriceResponse{
		mkPrice("q1", types.BILLING_PERIOD_QUARTER, 1),      // exact match to sub
		mkPrice("m1", types.BILLING_PERIOD_MONTHLY, 1),      // divisor (fan-out)
		mkPrice("m2", types.BILLING_PERIOD_MONTHLY, 1),      // divisor #2
		mkPrice("ot", types.BILLING_PERIOD_ONETIME, 1),      // always compatible
		mkPrice("y1", types.BILLING_PERIOD_ANNUAL, 1),       // non-divisor for quarter
	}
	// Currency mismatch — should always be dropped regardless of includeIDs.
	badCurrency := mkPrice("bc", types.BILLING_PERIOD_QUARTER, 1)
	badCurrency.Price.Currency = "eur"
	prices = append(prices, badCurrency)

	sub := mkSub(types.BILLING_PERIOD_QUARTER, 1)

	tests := []struct {
		name       string
		includeIDs *[]string
		want       []string // expected ids in the result (order not asserted)
	}{
		{
			name:       "nil include list — strict-equal default: only quarterly + ONETIME",
			includeIDs: nil,
			want:       []string{"q1", "ot"},
		},
		{
			name:       "empty include list — attach nothing (not even ONETIME)",
			includeIDs: &[]string{},
			want:       []string{},
		},
		{
			name:       "single compatible id (divisor) — exactly that id",
			includeIDs: &[]string{"m1"},
			want:       []string{"m1"},
		},
		{
			name:       "subset of compatible ids (mix of exact + divisor)",
			includeIDs: &[]string{"q1", "m2"},
			want:       []string{"q1", "m2"},
		},
		{
			name:       "all-compatible-ids explicit — same as if we listed them",
			includeIDs: &[]string{"q1", "m1", "m2", "ot"},
			want:       []string{"q1", "m1", "m2", "ot"},
		},
		{
			name:       "include list containing incompatible id — filter drops it defensively",
			includeIDs: &[]string{"q1", "y1"},
			// y1 is annual (12mo) on quarterly (3mo); 3%12 != 0. Filter drops
			// it silently — the upstream validateIncludePriceIDs is expected
			// to reject the whole request BEFORE reaching this filter.
			want: []string{"q1"},
		},
		{
			name:       "include list with unknown id — filter drops it defensively",
			includeIDs: &[]string{"q1", "does_not_exist"},
			want:       []string{"q1"},
		},
		{
			name:       "ONETIME id in explicit include list — attached",
			includeIDs: &[]string{"ot"},
			want:       []string{"ot"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := idsOf(filterValidPricesForSubscription(prices, sub, tc.includeIDs))
			assertSameIDs(t, tc.want, got)
		})
	}
}

func TestFilterValidPricesForSubscription_StrictEqualDefaultDoesNotFanOut(t *testing.T) {
	// Regression guard for the release-note breaking change: a plan with only
	// monthly prices under a quarterly sub attaches ZERO plan prices when
	// include_price_ids is omitted (must be opted in explicitly).
	prices := []*dto.PriceResponse{
		mkPrice("m1", types.BILLING_PERIOD_MONTHLY, 1),
		mkPrice("m2", types.BILLING_PERIOD_MONTHLY, 1),
	}
	sub := mkSub(types.BILLING_PERIOD_QUARTER, 1)

	got := filterValidPricesForSubscription(prices, sub, nil)
	if len(got) != 0 {
		t.Fatalf("strict-equal default must not fan out monthly prices on quarterly sub; got %v",
			idsOf(got))
	}

	// Same plan, same sub, but with explicit opt-in — both monthly prices attach.
	got = filterValidPricesForSubscription(prices, sub, &[]string{"m1", "m2"})
	assertSameIDs(t, []string{"m1", "m2"}, idsOf(got))
}

func TestFilterValidPricesForSubscription_DefaultIsPeriodOnlyEqualityMatchingMain(t *testing.T) {
	// Historical (pre-multi-cadence) behavior on main used period-only equality
	// and did NOT compare billing_period_count. Preserving that keeps this PR
	// non-breaking for callers who omit include_price_ids. MONTHLY×3 sub +
	// MONTHLY×1 price → attached by default (period-equal). Callers who want
	// the strict count-aware behavior must opt in via include_price_ids.
	prices := []*dto.PriceResponse{
		mkPrice("m1", types.BILLING_PERIOD_MONTHLY, 1), // period matches, count differs — attached under historical rule
		mkPrice("m3", types.BILLING_PERIOD_MONTHLY, 3), // exact period match
		mkPrice("q1", types.BILLING_PERIOD_QUARTER, 1), // different period → not attached even though effective months match
	}
	sub := mkSub(types.BILLING_PERIOD_MONTHLY, 3)

	got := idsOf(filterValidPricesForSubscription(prices, sub, nil))
	assertSameIDs(t, []string{"m1", "m3"}, got)
}

func TestFilterAddonPricesForSubscription_PreservesCompatSemantics(t *testing.T) {
	// Regression guard: addon path must keep divisor-compat acceptance so
	// pre-existing addon tests (monthly-on-annual, etc.) still work.
	prices := []*dto.PriceResponse{
		mkPrice("m1", types.BILLING_PERIOD_MONTHLY, 1),  // divisor of annual
		mkPrice("q1", types.BILLING_PERIOD_QUARTER, 1),  // divisor of annual
		mkPrice("h1", types.BILLING_PERIOD_HALF_YEAR, 1), // divisor of annual
		mkPrice("ot", types.BILLING_PERIOD_ONETIME, 1),
	}
	sub := mkSub(types.BILLING_PERIOD_ANNUAL, 1)

	got := idsOf(filterAddonPricesForSubscription(prices, sub))
	assertSameIDs(t, []string{"m1", "q1", "h1", "ot"}, got)
}

func TestValidateIncludePriceIDs_UnknownAndIncompatible(t *testing.T) {
	svc := &subscriptionService{}
	sub := mkSub(types.BILLING_PERIOD_QUARTER, 1)
	planPrices := []*dto.PriceResponse{
		mkPrice("m1", types.BILLING_PERIOD_MONTHLY, 1),
		mkPrice("q1", types.BILLING_PERIOD_QUARTER, 1),
		mkPrice("y1", types.BILLING_PERIOD_ANNUAL, 1), // incompatible with quarterly
		mkPrice("ot", types.BILLING_PERIOD_ONETIME, 1),
	}

	// All valid -> nil
	if err := svc.validateIncludePriceIDs("plan_test", sub, []string{"m1", "q1", "ot"}, planPrices); err != nil {
		t.Fatalf("expected no error for valid ids, got: %v", err)
	}

	// Contains unknown id -> error mentions the id
	err := svc.validateIncludePriceIDs("plan_test", sub, []string{"m1", "does_not_exist"}, planPrices)
	if err == nil {
		t.Fatal("expected error for unknown id")
	}
	if !containsAll(err.Error(), "does_not_exist") {
		t.Fatalf("error should name the unknown id; got: %v", err)
	}

	// Contains incompatible id -> error mentions the id + sub cadence
	err = svc.validateIncludePriceIDs("plan_test", sub, []string{"m1", "y1"}, planPrices)
	if err == nil {
		t.Fatal("expected error for incompatible id")
	}
	if !containsAll(err.Error(), "y1") {
		t.Fatalf("error should name the incompatible id; got: %v", err)
	}

	// Mix of unknown + incompatible -> reports both
	err = svc.validateIncludePriceIDs("plan_test", sub, []string{"m1", "does_not_exist", "y1"}, planPrices)
	if err == nil {
		t.Fatal("expected error for mixed unknown+incompatible")
	}
	if !containsAll(err.Error(), "does_not_exist", "y1") {
		t.Fatalf("error should name both offending ids; got: %v", err)
	}
}

func TestValidateIncludePriceIDs_WrongCurrency(t *testing.T) {
	svc := &subscriptionService{}
	sub := mkSub(types.BILLING_PERIOD_MONTHLY, 1)
	// Sub currency is "usd" (see mkSub). Build a plan price with a mismatched
	// currency but otherwise cadence-compatible.
	planPrices := []*dto.PriceResponse{
		mkPrice("m_usd", types.BILLING_PERIOD_MONTHLY, 1),
	}
	planPrices = append(planPrices, &dto.PriceResponse{
		Price: &price.Price{
			ID:                 "m_eur",
			Currency:           "eur",
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
		},
	})

	// Same-currency id passes.
	if err := svc.validateIncludePriceIDs("plan_test", sub, []string{"m_usd"}, planPrices); err != nil {
		t.Fatalf("expected no error for same-currency id, got: %v", err)
	}

	// Mismatched-currency id is caught explicitly with its own bucket in details.
	err := svc.validateIncludePriceIDs("plan_test", sub, []string{"m_eur"}, planPrices)
	if err == nil {
		t.Fatal("expected error for mismatched-currency id")
	}
	if !containsAll(err.Error(), "m_eur", "wrong_currency") {
		t.Fatalf("error should name the wrong-currency id and category; got: %v", err)
	}
}

func assertSameIDs(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("id count mismatch: want %v, got %v", want, got)
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}
	for _, id := range got {
		if _, ok := wantSet[id]; !ok {
			t.Fatalf("unexpected id %q in result; want %v got %v", id, want, got)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
