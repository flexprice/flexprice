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

func TestAnalyticsV2BillingPeriods(t *testing.T) {
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
			got, err := analyticsV2BillingPeriods(tc.sub(), tc.start, tc.end)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyAnalyticsV2InvoiceLines(t *testing.T) {
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

	got := classifyAnalyticsV2InvoiceLines("sub_1", "usd", rows)

	byLine := lo.SliceToMap(got.lines, func(l *analyticsV2LineCost) (string, *analyticsV2LineCost) { return l.key.lineItemID, l })
	require.Len(t, byLine, 4, "zero-amount rows are skipped")

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

	require.Len(t, got.unattributed, 2)
	require.Equal(t, "plan_1", got.unattributed[1].charge.PlanID, "plan true-up keeps its plan")
	for _, u := range got.unattributed {
		require.Equal(t, "sub_1", u.subscriptionID)
		require.Equal(t, dto.AnalyticsV2ReasonUnusedCommitment, u.charge.Reason)
		require.True(t, u.charge.Cost.Equal(u.charge.Breakdown.TrueUp))
	}

	total := decimal.Zero
	for _, l := range got.lines {
		total = total.Add(l.total())
	}
	for _, u := range got.unattributed {
		total = total.Add(u.charge.Cost)
	}
	require.Equal(t, "23900", total.String(), "every billed amount lands on a line or in unattributed")
}

func TestSplitAnalyticsV2Excluded(t *testing.T) {
	sumMeter := &meter.Meter{ID: "m_sum", Aggregation: meter.Aggregation{Type: types.AggregationSum}}
	maxMeter := &meter.Meter{ID: "m_max", Aggregation: meter.Aggregation{Type: types.AggregationMax}}
	uniqueMeter := &meter.Meter{ID: "m_users", Aggregation: meter.Aggregation{Type: types.AggregationCountUnique}}
	line := func(id, meterID, cost string) *analyticsV2LineCost {
		return &analyticsV2LineCost{
			key:     analyticsV2LineKey{lineItemID: id},
			meterID: meterID,
			priceID: "p_" + meterID,
			cost:    dto.AnalyticsV2Cost{Usage: decimal.RequireFromString(cost)},
		}
	}

	kept, excluded := splitAnalyticsV2Excluded(
		[]*analyticsV2LineCost{
			line("l1", "m_sum", "100"),
			line("l2", "m_max", "50"),
			line("l3", "m_users", "30"),
			line("l4", "m_users", "20"),
			line("l5", "m_unknown", "5"),
		},
		map[string]*meter.Meter{"m_sum": sumMeter, "m_max": maxMeter, "m_users": uniqueMeter},
		map[string]*price.Price{},
	)

	require.Equal(t, []string{"l1", "l2"}, lo.Map(kept, func(l *analyticsV2LineCost, _ int) string { return l.key.lineItemID }))
	require.Len(t, excluded, 2)
	require.Equal(t, "m_users", excluded[0].MeterID)
	require.Equal(t, types.AggregationCountUnique, excluded[0].Aggregation)
	require.Equal(t, "50", excluded[0].Cost.String(), "cost is summed per meter")
	require.Equal(t, "m_unknown", excluded[1].MeterID, "a line whose meter is missing is excluded, not dropped")
}

func TestRejectAnalyticsV2Families(t *testing.T) {
	require.NoError(t, rejectAnalyticsV2Families([]*subscription.Subscription{{ID: "s", SubscriptionType: types.SubscriptionTypeStandalone}}))
	for _, st := range []types.SubscriptionType{types.SubscriptionTypeParent, types.SubscriptionTypeInherited} {
		err := rejectAnalyticsV2Families([]*subscription.Subscription{{ID: "s", SubscriptionType: st}})
		require.Error(t, err)
		require.True(t, ierr.IsValidation(err))
	}
}
