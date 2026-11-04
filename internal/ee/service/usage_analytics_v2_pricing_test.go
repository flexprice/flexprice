package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestOverlappingBillingPeriods(t *testing.T) {
	day := func(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }
	monthly := func() *subscription.Subscription {
		return &subscription.Subscription{
			BillingAnchor:      day(time.January, 5),
			BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
			BillingPeriodCount: 1,
			StartDate:          day(time.January, 5),
		}
	}

	tests := []struct {
		name       string
		sub        func() *subscription.Subscription
		start, end time.Time
		want       []types.Period
	}{
		{
			name:  "range inside one period returns the full period",
			sub:   monthly,
			start: day(time.August, 10), end: day(time.August, 20),
			want: []types.Period{{Start: day(time.August, 5), End: day(time.September, 5)}},
		},
		{
			name:  "range across an anchor returns both periods",
			sub:   monthly,
			start: day(time.August, 1), end: day(time.August, 31),
			want: []types.Period{
				{Start: day(time.July, 5), End: day(time.August, 5)},
				{Start: day(time.August, 5), End: day(time.September, 5)},
			},
		},
		{
			name:  "range ending on an anchor stops there",
			sub:   monthly,
			start: day(time.August, 5), end: day(time.September, 5),
			want: []types.Period{{Start: day(time.August, 5), End: day(time.September, 5)}},
		},
		{
			name: "cancelled subscription stops at the cancel date",
			sub: func() *subscription.Subscription {
				s := monthly()
				s.CancelledAt = lo.ToPtr(day(time.August, 20))
				return s
			},
			start: day(time.August, 1), end: day(time.October, 1),
			want: []types.Period{
				{Start: day(time.July, 5), End: day(time.August, 5)},
				{Start: day(time.August, 5), End: day(time.September, 5)},
			},
		},
		{
			name:  "range before the subscription started is empty",
			sub:   monthly,
			start: day(time.January, 1), end: day(time.January, 5),
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := overlappingBillingPeriods(tc.sub(), tc.start, tc.end)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyInvoiceLines(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	d := decimal.RequireFromString
	row := func(lineID, meterID, amount string, meta types.Metadata) dto.CreateInvoiceLineItemRequest {
		r := dto.CreateInvoiceLineItemRequest{
			Amount:      d(amount),
			Quantity:    decimal.NewFromInt(1),
			PeriodStart: lo.ToPtr(start),
			PeriodEnd:   lo.ToPtr(end),
			Metadata:    meta,
		}
		if lineID != "" {
			r.SubscriptionLineItemID = lo.ToPtr(lineID)
			r.MeterID = lo.ToPtr(meterID)
			r.PriceID = lo.ToPtr("price_" + meterID)
		}
		return r
	}

	ai := row("li_ai", "ai", "6000", nil)
	ai.AdjustedEntitlementQuantity = lo.ToPtr(d("1000"))
	ai.EntityType, ai.EntityID = lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)), lo.ToPtr("plan_1")
	// Subscription-level commitment rows carry no line; they are not covered yet.
	planTrueUp := row("", "", "900", types.Metadata{types.MetadataKeyIsCommitmentTrueup: types.MetadataValueTrue})
	planTrueUp.EntityType, planTrueUp.EntityID = lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)), lo.ToPtr("plan_1")

	legacy := row("li_compute", "compute", "10000", nil)
	legacy.CommitmentInfo = &types.CommitmentInfo{
		ComputedCommitmentUtilizedAmount: d("6000"),
		ComputedTrueUpAmount:             d("4000"),
	}

	rows := []dto.CreateInvoiceLineItemRequest{
		ai,
		legacy,
		row("li_gpu", "gpu", "800", types.Metadata{types.MetadataKeyCommitmentLineKind: string(types.CommitmentLineUtilized)}),
		row("li_gpu", "gpu", "200", types.Metadata{types.MetadataKeyCommitmentLineKind: string(types.CommitmentLineOverage)}),
		row("li_tokens", "tokens", "300", types.Metadata{types.MetadataKeyIsOverage: types.MetadataValueTrue}),
		row("li_tokens", "tokens", "700", nil),
		row("", "", "5000", types.Metadata{types.MetadataKeyCommitmentLineKind: string(types.CommitmentLineTrueUp)}),
		planTrueUp,
		row("li_zero", "zero", "0", nil),
	}

	got := classifyInvoiceLines("sub_1", "usd", rows)

	byLine := lo.SliceToMap(got.lines, func(l *pricedLine) (string, *pricedLine) { return l.key.lineItemID, l })
	require.Len(t, byLine, 5, "zero-amount line rows are kept so unsupported meters can be reported")
	require.True(t, byLine["li_zero"].total().IsZero())

	require.Equal(t, "6000", byLine["li_ai"].cost.Usage.String())
	require.Equal(t, "1000", byLine["li_ai"].freeQuantity.String())
	require.Equal(t, "plan_1", byLine["li_ai"].planID)
	require.Empty(t, byLine["li_ai"].addonID)

	compute := byLine["li_compute"].cost
	require.Equal(t, "6000", compute.CommitmentUtilized.String())
	require.Equal(t, "4000", compute.TrueUp.String())
	require.True(t, compute.Usage.IsZero())
	require.Equal(t, "10000", byLine["li_compute"].total().String())

	gpu := byLine["li_gpu"].cost
	require.Equal(t, "800", gpu.CommitmentUtilized.String())
	require.Equal(t, "200", gpu.Overage.String())

	tokens := byLine["li_tokens"].cost
	require.Equal(t, "700", tokens.Usage.String())
	require.Equal(t, "300", tokens.Overage.String())

	total := decimal.Zero
	for _, l := range got.lines {
		total = total.Add(l.total())
	}
	require.Equal(t, "18000", total.String(), "every linked amount lands on its line; rows without a line are skipped")
}

func TestIsSupportedPricing(t *testing.T) {
	agg := func(a types.AggregationType) *meter.Meter {
		return &meter.Meter{Aggregation: meter.Aggregation{Type: a}}
	}
	for _, a := range []types.AggregationType{types.AggregationSum, types.AggregationCount, types.AggregationSumWithMultiplier} {
		require.True(t, isSupportedPricing(agg(a), nil), a)
	}
	for _, a := range []types.AggregationType{types.AggregationMax, types.AggregationCountUnique, types.AggregationLatest, types.AggregationAvg} {
		require.False(t, isSupportedPricing(agg(a), nil), a)
	}
	require.False(t, isSupportedPricing(nil, nil), "a line whose meter is missing is left out")
	daily := &price.Price{BucketSize: types.WindowSizeDay}
	require.True(t, isSupportedPricing(agg(types.AggregationMax), daily), "bucketed MAX is covered")
}

func TestRejectSubscriptionFamilies(t *testing.T) {
	require.NoError(t, rejectSubscriptionFamilies([]*subscription.Subscription{{ID: "s", SubscriptionType: types.SubscriptionTypeStandalone}}))
	for _, st := range []types.SubscriptionType{types.SubscriptionTypeParent, types.SubscriptionTypeInherited} {
		err := rejectSubscriptionFamilies([]*subscription.Subscription{{ID: "s", SubscriptionType: st}})
		require.Error(t, err)
		require.True(t, ierr.IsValidation(err))
	}
}

func TestSetPriceUnit(t *testing.T) {
	credits := func(id, unit, rate string) *price.Price {
		r := decimal.RequireFromString(rate)
		return &price.Price{ID: id, PriceUnitType: types.PRICE_UNIT_TYPE_CUSTOM, PriceUnit: lo.ToPtr(unit), ConversionRate: &r}
	}
	line := func(priceID, cost string) *pricedLine {
		return &pricedLine{priceID: priceID, cost: dto.AnalyticsV2Cost{Usage: decimal.RequireFromString(cost)}}
	}
	prices := map[string]*price.Price{
		"p_mac": credits("p_mac", "mac", "0.0015"), "p_mac2": credits("p_mac2", "mac", "0.0015"),
		"p_other_rate": credits("p_other_rate", "mac", "0.002"), "p_usd": {ID: "p_usd", PriceUnitType: types.PRICE_UNIT_TYPE_FIAT},
	}
	ac := &analyticsCtx{currency: "usd", prices: prices}

	// A $0 fiat line (e.g. a free sessions meter) doesn't stop costs being shown in credits.
	ac.setPriceUnit([]*pricedLine{line("p_mac", "1"), line("p_mac2", "2"), line("p_usd", "0")})
	require.Equal(t, "mac", ac.priceUnit)
	require.Equal(t, "398", ac.priceUnitCost(decimal.RequireFromString("0.597002985")).String(), "converted before rounding to cents")

	for name, lines := range map[string][]*pricedLine{
		"billed fiat line": {line("p_mac", "1"), line("p_usd", "1")},
		"two rates":        {line("p_mac", "1"), line("p_other_rate", "1")},
	} {
		ac := &analyticsCtx{currency: "usd", prices: prices}
		ac.setPriceUnit(lines)
		require.Empty(t, ac.priceUnit, name)
		require.Nil(t, ac.priceUnitCost(decimal.NewFromInt(1)), name)
	}
}
