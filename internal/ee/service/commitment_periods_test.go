package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func monthlySub(start time.Time) *subscription.Subscription {
	return &subscription.Subscription{
		ID:                 "subs_1",
		StartDate:          start,
		BillingAnchor:      start,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		Timezone:           "UTC",
		Currency:           "usd",
	}
}

// mulFn values capacity at a flat rate.
func mulFn(rate decimal.Decimal) func(decimal.Decimal) decimal.Decimal {
	return func(q decimal.Decimal) decimal.Decimal { return q.Mul(rate) }
}

func TestCommitmentPeriods(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		c           *commitment.Commitment
		amountOf    func(decimal.Decimal) decimal.Decimal
		bucket      types.WindowSize
		wantPeriods int
		wantFirst   string
		wantTotal   string
	}{
		{
			name: "P1 amount, aligned 12 months",
			c: &commitment.Commitment{
				Type:        types.COMMITMENT_TYPE_AMOUNT,
				Value:       decimal.NewFromInt(100),
				Measurement: types.CommitmentMeasurementBillingPeriod,
				Timezone:    "UTC",
				StartDate:   jan,
				EndDate:     lo.ToPtr(jan.AddDate(1, 0, 0)),
			},
			amountOf:    mulFn(decimal.NewFromInt(1)),
			wantPeriods: 12,
			wantFirst:   "100",
			wantTotal:   "1200",
		},
		{
			name: "P2 mid-period start on 15 Jan is prorated",
			c: &commitment.Commitment{
				Type:        types.COMMITMENT_TYPE_AMOUNT,
				Value:       decimal.NewFromInt(100),
				Measurement: types.CommitmentMeasurementBillingPeriod,
				Timezone:    "UTC",
				StartDate:   time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC),
				EndDate:     lo.ToPtr(jan.AddDate(1, 0, 0)),
			},
			amountOf:    mulFn(decimal.NewFromInt(1)),
			wantPeriods: 12,
			wantFirst:   "54.83870968",
			wantTotal:   "1154.83870968",
		},
		{
			name: "P3 quantity per minute, August only",
			c: &commitment.Commitment{
				Type:        types.COMMITMENT_TYPE_QUANTITY,
				Value:       decimal.NewFromInt(8),
				Measurement: types.CommitmentMeasurementWindow,
				Timezone:    "UTC",
				StartDate:   time.Date(2027, 8, 1, 0, 0, 0, 0, time.UTC),
				EndDate:     lo.ToPtr(time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC)),
			},
			amountOf:    mulFn(decimal.RequireFromString("0.048")),
			bucket:      types.WindowSizeMinute,
			wantPeriods: 1,
			wantFirst:   "17141.76",
			wantTotal:   "17141.76",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			periods, err := commitmentPeriods(tc.c, monthlySub(jan), tc.amountOf, tc.bucket, tc.c.StartDate, *tc.c.EndDate)
			require.NoError(t, err)
			require.Len(t, periods, tc.wantPeriods)
			require.True(
				t,
				periods[0].Amount.Round(8).Equal(decimal.RequireFromString(tc.wantFirst)),
				"first=%s",
				periods[0].Amount,
			)
			require.True(
				t,
				commitmentValue(periods).Round(8).Equal(decimal.RequireFromString(tc.wantTotal)),
				"total=%s",
				commitmentValue(periods),
			)
		})
	}
}

func TestCommitmentPeriods_ActiveWindowsIST(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	start := time.Date(2026, 8, 25, 0, 0, 0, 0, ist)
	end := time.Date(2026, 9, 1, 0, 0, 0, 0, ist)
	c := &commitment.Commitment{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Value:       decimal.NewFromInt(32),
		Measurement: types.CommitmentMeasurementWindow,
		Timezone:    "Asia/Kolkata",
		ActiveWindows: types.ActiveWindows{
			{
				Start: types.Bucket{Hour: 9},
				End:   types.Bucket{Hour: 21},
			},
		},
		StartDate: start.UTC(),
		EndDate:   lo.ToPtr(end.UTC()),
	}

	sub := monthlySub(time.Date(2026, 8, 1, 0, 0, 0, 0, ist).UTC())
	sub.Timezone = "Asia/Kolkata"

	periods, err := commitmentPeriods(
		c,
		sub,
		mulFn(decimal.RequireFromString("0.0491667")),
		types.WindowSizeMinute,
		c.StartDate,
		*c.EndDate,
	)
	require.NoError(t, err)
	require.Len(t, periods, 1)
	require.Equal(t, int64(7*12*60), periods[0].ActiveBuckets) // 7 days × 12 h × 60 min
}

func TestCommitmentPeriods_ProratesAgainstTheFullBillingPeriod(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	jan15 := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)

	endingSub := monthlySub(jan)
	endingSub.EndDate = &jan15

	calendarSub := monthlySub(jan15)
	calendarSub.BillingAnchor = feb

	tests := []struct {
		name      string
		sub       *subscription.Subscription
		start     time.Time
		end       time.Time
		wantFirst string
	}{
		{
			name:      "term ends with the subscription on 15 Jan",
			sub:       endingSub,
			start:     jan,
			end:       jan15,
			wantFirst: "45.16129032",
		},
		{
			name:      "calendar subscription starts mid-period on 15 Jan",
			sub:       calendarSub,
			start:     jan15,
			end:       feb,
			wantFirst: "54.83870968",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &commitment.Commitment{
				Type:        types.COMMITMENT_TYPE_AMOUNT,
				Value:       decimal.NewFromInt(100),
				Measurement: types.CommitmentMeasurementBillingPeriod,
				Timezone:    "UTC",
				StartDate:   tc.start,
				EndDate:     lo.ToPtr(tc.end),
			}

			periods, err := commitmentPeriods(c, tc.sub, mulFn(decimal.NewFromInt(1)), "", c.StartDate, *c.EndDate)
			require.NoError(t, err)
			require.Len(t, periods, 1)
			require.True(
				t,
				periods[0].Amount.Round(8).Equal(decimal.RequireFromString(tc.wantFirst)),
				"first=%s",
				periods[0].Amount,
			)
		})
	}
}

func TestCommitmentPeriods_SkipsBucketsBeforeStart(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &commitment.Commitment{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Value:       decimal.NewFromInt(1),
		Measurement: types.CommitmentMeasurementWindow,
		Timezone:    "UTC",
		StartDate:   time.Date(2027, 1, 31, 12, 30, 0, 0, time.UTC),
		EndDate:     lo.ToPtr(time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)),
	}

	periods, err := commitmentPeriods(
		c,
		monthlySub(jan),
		mulFn(decimal.NewFromInt(1)),
		types.WindowSizeHour,
		c.StartDate,
		*c.EndDate,
	)
	require.NoError(t, err)
	require.Len(t, periods, 1)
	require.Equal(t, int64(11), periods[0].ActiveBuckets) // 13:00 to 23:00; the 12:00 bucket starts before the term
}

func TestCommitmentPeriods_BucketStraddlingAPeriodBoundaryCountsOnce(t *testing.T) {
	// Weekly (Monday) buckets on a monthly subscription: the bucket starting Mon 26 Jan 2026
	// runs into February. It belongs to January; February's period must not count it again.
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &commitment.Commitment{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Value:       decimal.NewFromInt(1),
		Measurement: types.CommitmentMeasurementWindow,
		Timezone:    "UTC",
		StartDate:   jan,
		EndDate:     lo.ToPtr(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
	}

	periods, err := commitmentPeriods(c, monthlySub(jan), mulFn(decimal.NewFromInt(1)), types.WindowSizeWeek, c.StartDate, *c.EndDate)
	require.NoError(t, err)
	require.Len(t, periods, 2)
	require.Equal(t, int64(4), periods[0].ActiveBuckets) // Mondays 5, 12, 19, 26 Jan; 29 Dec starts before the term
	require.Equal(t, int64(4), periods[1].ActiveBuckets) // Mondays 2, 9, 16, 23 Feb; 26 Jan already counted in January
	require.True(t, commitmentValue(periods).Equal(decimal.NewFromInt(8)))
}

func TestCommitmentPeriods_CountsPartialEdgeBuckets(t *testing.T) {
	// A term starting mid-minute skips the straddling bucket; one ending mid-minute keeps its last.
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &commitment.Commitment{
		Type:        types.COMMITMENT_TYPE_QUANTITY,
		Value:       decimal.NewFromInt(1),
		Measurement: types.CommitmentMeasurementWindow,
		Timezone:    "UTC",
		StartDate:   jan.Add(10*time.Hour + 30*time.Second),
		EndDate:     lo.ToPtr(jan.Add(12*time.Hour + 10*time.Second)),
	}

	periods, err := commitmentPeriods(c, monthlySub(jan), mulFn(decimal.NewFromInt(1)), types.WindowSizeMinute, c.StartDate, *c.EndDate)
	require.NoError(t, err)
	require.Len(t, periods, 1)
	// 10:01 through 12:00 inclusive: the 10:00 bucket starts before the term, 12:00 starts before its end.
	require.Equal(t, int64(120), periods[0].ActiveBuckets)
}

func TestClipCommitmentPeriod_PartialFloorsSumToTheRoundedWhole(t *testing.T) {
	apr := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	// halfThenHalfRate charges 1 per unit for the first 50 and 0.50 after, so 100 units cost 75.
	halfThenHalfRate := func(q decimal.Decimal) decimal.Decimal {
		first := decimal.Min(q, decimal.NewFromInt(50))
		return first.Add(q.Sub(first).Mul(decimal.RequireFromString("0.5")))
	}
	day := func(start time.Time, d int) time.Time { return start.AddDate(0, 0, d-1) }

	tests := []struct {
		name     string
		typ      types.CommitmentType
		value    string
		amountOf func(decimal.Decimal) decimal.Decimal
		month    time.Time
		bounds   []time.Time // invoice edges inside the month
		want     []string
	}{
		{
			name:     "tiered halves bill the first 50 units at 1 and the next 50 at 0.50",
			typ:      types.COMMITMENT_TYPE_QUANTITY,
			value:    "100",
			amountOf: halfThenHalfRate,
			month:    apr,
			bounds:   []time.Time{day(apr, 16)},
			want:     []string{"50", "25"},
		},
		{
			name:     "a one cent period split evenly bills the cent once",
			typ:      types.COMMITMENT_TYPE_AMOUNT,
			value:    "0.01",
			amountOf: identityAmount,
			month:    apr,
			bounds:   []time.Time{day(apr, 16)},
			want:     []string{"0.01", "0"},
		},
		{
			name:     "flat halves of a 31 day month keep their prorated shares",
			typ:      types.COMMITMENT_TYPE_AMOUNT,
			value:    "100",
			amountOf: identityAmount,
			month:    aug,
			bounds:   []time.Time{day(aug, 16)},
			want:     []string{"48.39", "51.61"},
		},
		{
			name:     "thirds of 100 sum to exactly 100",
			typ:      types.COMMITMENT_TYPE_AMOUNT,
			value:    "100",
			amountOf: identityAmount,
			month:    apr,
			bounds:   []time.Time{day(apr, 11), day(apr, 21)},
			want:     []string{"33.33", "33.34", "33.33"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			end := tc.month.AddDate(0, 1, 0)
			c := &commitment.Commitment{
				Type:        tc.typ,
				Value:       decimal.RequireFromString(tc.value),
				Currency:    "usd",
				Measurement: types.CommitmentMeasurementBillingPeriod,
				Timezone:    "UTC",
				StartDate:   tc.month,
				EndDate:     lo.ToPtr(end),
			}
			periods, err := commitmentPeriods(c, monthlySub(tc.month), tc.amountOf, "", c.StartDate, *c.EndDate)
			require.NoError(t, err)
			require.Len(t, periods, 1)

			edges := append(append([]time.Time{tc.month}, tc.bounds...), end)
			sum := decimal.Zero
			for i, want := range tc.want {
				part := clipCommitmentPeriod(c, periods[0], edges[i], edges[i+1], tc.amountOf, "", tc.month)
				require.True(t, part.Amount.Equal(decimal.RequireFromString(want)), "part %d: want %s, got %s", i, want, part.Amount)
				sum = sum.Add(part.Amount)
			}

			require.True(t, sum.Equal(types.RoundToCurrencyPrecision(periods[0].Amount, "usd")), "sum %s", sum)
		})
	}
}

// A year from 15 Jan 2027 starts and ends mid-period: its first and last periods prorate against their full
// months, 17 and 14 of 31 days, and the partial invoices of the last sum to its rounded floor.
func TestCommitmentPeriods_MidPeriodEndProratesTheLastPeriod(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	lastMonth := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &commitment.Commitment{
		Type:        types.COMMITMENT_TYPE_AMOUNT,
		Value:       decimal.NewFromInt(100),
		Currency:    "usd",
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Timezone:    "UTC",
		StartDate:   start,
		EndDate:     lo.ToPtr(end),
	}

	periods, err := commitmentPeriods(c, monthlySub(jan), identityAmount, "", c.StartDate, *c.EndDate)
	require.NoError(t, err)
	require.Len(t, periods, 13)
	last := periods[12]
	require.Equal(t, lastMonth, last.Start)
	require.Equal(t, end, last.End)
	require.Equal(t, lastMonth, last.FullStart)
	require.Equal(t, lastMonth.AddDate(0, 1, 0), last.FullEnd)
	require.False(t, last.Clipped)
	require.True(t, periods[0].Amount.Round(8).Equal(decimal.RequireFromString("54.83870968")), "first=%s", periods[0].Amount)
	require.True(t, last.Amount.Round(8).Equal(decimal.RequireFromString("45.16129032")), "last=%s", last.Amount)
	require.True(t, commitmentValue(periods).Round(8).Equal(decimal.NewFromInt(1200)), "total=%s", commitmentValue(periods))

	split := time.Date(2028, 1, 10, 0, 0, 0, 0, time.UTC)
	before := clipCommitmentPeriod(c, last, lastMonth, split, identityAmount, "", jan)
	after := clipCommitmentPeriod(c, last, split, lastMonth.AddDate(0, 1, 0), identityAmount, "", jan)
	require.True(t, before.Clipped)
	require.True(t, before.Amount.Equal(decimal.RequireFromString("29.03")), "before=%s", before.Amount)
	require.True(t, after.Clipped)
	require.Equal(t, end, after.End)
	require.True(t, after.Amount.Equal(decimal.RequireFromString("16.13")), "after=%s", after.Amount)
	require.True(t, before.Amount.Add(after.Amount).Equal(decimal.RequireFromString("45.16")))

	daily := *c
	daily.Type, daily.Measurement, daily.Value = types.COMMITMENT_TYPE_QUANTITY, types.CommitmentMeasurementWindow, decimal.NewFromInt(1)
	periods, err = commitmentPeriods(&daily, monthlySub(jan), identityAmount, types.WindowSizeDay, daily.StartDate, *daily.EndDate)
	require.NoError(t, err)
	require.Len(t, periods, 13)
	require.Equal(t, int64(17), periods[0].ActiveBuckets)
	require.Equal(t, int64(14), periods[12].ActiveBuckets) // 1 to 14 Jan; the 15 Jan bucket starts at the end
	require.True(t, commitmentValue(periods).Equal(decimal.NewFromInt(365)), "total=%s", commitmentValue(periods))
}

// An open-ended commitment has no last period: its periods run through the billing period holding `through`, each
// a full month at the full floor but the first, which prorates its mid-period start.
func TestCommitmentPeriods_OpenEndedRunsThroughTheWindow(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		start     time.Time
		through   time.Time
		wantFirst string
		wantLast  time.Time
		wantCount int
	}{
		{
			name:      "aligned start, window ending on a period boundary",
			start:     jan,
			through:   jan.AddDate(0, 4, 0),
			wantFirst: "100",
			wantLast:  jan.AddDate(0, 3, 0),
			wantCount: 4,
		},
		{
			name:      "window ending mid-period runs to that period's end",
			start:     jan,
			through:   time.Date(2027, 4, 10, 0, 0, 0, 0, time.UTC),
			wantFirst: "100",
			wantLast:  jan.AddDate(0, 3, 0),
			wantCount: 4,
		},
		{
			name:      "mid-period start on 15 Jan prorates the first floor",
			start:     time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC),
			through:   jan.AddDate(10, 0, 0),
			wantFirst: "54.83870968",
			wantLast:  jan.AddDate(9, 11, 0),
			wantCount: 120,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &commitment.Commitment{
				Type:        types.COMMITMENT_TYPE_AMOUNT,
				Value:       decimal.NewFromInt(100),
				Measurement: types.CommitmentMeasurementBillingPeriod,
				Timezone:    "UTC",
				StartDate:   tc.start,
			}

			periods, err := commitmentPeriods(c, monthlySub(jan), identityAmount, "", c.StartDate, tc.through)
			require.NoError(t, err)
			require.Len(t, periods, tc.wantCount)
			require.True(t, periods[0].Amount.Round(8).Equal(decimal.RequireFromString(tc.wantFirst)), "first=%s", periods[0].Amount)
			for _, p := range periods[1:] {
				require.True(t, p.Amount.Equal(decimal.NewFromInt(100)), "%s: amount=%s", p.Start, p.Amount)
				require.Equal(t, p.FullEnd, p.End, "%s is a whole period", p.Start)
			}

			last := periods[len(periods)-1]
			require.Equal(t, tc.wantLast, last.Start)
			require.Equal(t, tc.wantLast.AddDate(0, 1, 0), last.End)
		})
	}
}

// An open-ended commitment's floor stops at its subscription's scheduled end: the last period is
// prorated to the subscription end, and nothing accrues past it.
func TestCommitmentPeriods_OpenEndedStopsAtTheSubscriptionEnd(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := monthlySub(jan)
	sub.EndDate = lo.ToPtr(time.Date(2027, 3, 16, 0, 0, 0, 0, time.UTC))
	c := &commitment.Commitment{
		Type:        types.COMMITMENT_TYPE_AMOUNT,
		Value:       decimal.NewFromInt(100),
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Timezone:    "UTC",
		StartDate:   jan,
	}

	periods, err := commitmentPeriods(c, sub, identityAmount, "", c.StartDate, jan.AddDate(0, 6, 0))
	require.NoError(t, err)
	require.Len(t, periods, 3)
	require.True(t, periods[0].Amount.Equal(decimal.NewFromInt(100)))
	require.True(t, periods[1].Amount.Equal(decimal.NewFromInt(100)))
	// March runs only 1 Mar to 16 Mar: 15 of 31 days.
	require.Equal(t, *sub.EndDate, periods[2].End)
	require.True(t, periods[2].Amount.Round(8).Equal(decimal.RequireFromString("48.38709677")), "last=%s", periods[2].Amount)
}

// An open-ended commitment has no finite value; one with an end_date is valued over its whole term.
func TestTermValue(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &commitment.Commitment{
		Type:        types.COMMITMENT_TYPE_AMOUNT,
		Value:       decimal.NewFromInt(100),
		Measurement: types.CommitmentMeasurementBillingPeriod,
		Timezone:    "UTC",
		StartDate:   jan,
	}

	value, err := termValue(c, monthlySub(jan), identityAmount, "")
	require.NoError(t, err)
	require.Nil(t, value)

	c.EndDate = lo.ToPtr(jan.AddDate(0, 6, 0))
	value, err = termValue(c, monthlySub(jan), identityAmount, "")
	require.NoError(t, err)
	require.NotNil(t, value)
	require.True(t, value.Equal(decimal.NewFromInt(600)), "value=%s", value)
}

// periodGenerationFixtures are multi-year subscriptions across cadences, anchors and timezones.
func periodGenerationFixtures(t *testing.T) map[string]*subscription.Subscription {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	sub := func(start, anchor time.Time, period types.BillingPeriod, count int, tz string) *subscription.Subscription {
		return &subscription.Subscription{
			ID:                 "subs_1",
			StartDate:          start,
			BillingAnchor:      anchor,
			BillingPeriod:      period,
			BillingPeriodCount: count,
			Timezone:           tz,
			Currency:           "usd",
		}
	}
	jan31 := time.Date(2020, 1, 31, 0, 0, 0, 0, time.UTC)
	dstDaily := time.Date(2021, 1, 10, 2, 30, 0, 0, ny).UTC()
	wed := time.Date(2021, 1, 6, 9, 0, 0, 0, time.UTC)
	mon := time.Date(2021, 1, 4, 23, 0, 0, 0, time.UTC)
	return map[string]*subscription.Subscription{
		"monthly anchored on the 31st": sub(jan31, jan31, types.BILLING_PERIOD_MONTHLY, 1, "UTC"),
		"monthly calendar, mid-month start": sub(
			time.Date(2020, 1, 15, 10, 0, 0, 0, time.UTC), time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC),
			types.BILLING_PERIOD_MONTHLY, 1, "UTC",
		),
		"two-monthly in Kolkata": sub(
			time.Date(2019, 11, 30, 18, 30, 0, 0, time.UTC), time.Date(2019, 11, 30, 18, 30, 0, 0, time.UTC),
			types.BILLING_PERIOD_MONTHLY, 2, "Asia/Kolkata",
		),
		"quarterly":                    sub(time.Date(2019, 3, 15, 0, 0, 0, 0, time.UTC), time.Date(2019, 3, 15, 0, 0, 0, 0, time.UTC), types.BILLING_PERIOD_QUARTER, 1, "UTC"),
		"annual from 29 Feb":           sub(time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC), time.Date(2020, 2, 29, 0, 0, 0, 0, time.UTC), types.BILLING_PERIOD_ANNUAL, 1, "UTC"),
		"daily across DST in New York": sub(dstDaily, dstDaily, types.BILLING_PERIOD_DAILY, 1, "America/New_York"),
		"daily anchored later in the day": sub(
			time.Date(2021, 3, 1, 5, 0, 0, 0, time.UTC), time.Date(2021, 3, 1, 23, 0, 0, 0, time.UTC),
			types.BILLING_PERIOD_DAILY, 1, "UTC",
		),
		"three-daily":                sub(wed, mon, types.BILLING_PERIOD_DAILY, 3, "UTC"),
		"weekly, midweek start":      sub(wed, mon, types.BILLING_PERIOD_WEEKLY, 1, "UTC"),
		"fortnightly, midweek start": sub(wed, mon, types.BILLING_PERIOD_WEEKLY, 2, "UTC"),
	}
}

// fullPeriods generates every billing period from the subscription start, the reference the shortcuts must match.
func fullPeriods(t *testing.T, sub *subscription.Subscription, until time.Time) []types.Period {
	periods, err := types.CalculateBillingPeriods(&types.CalculateBillingPeriodsParams{
		InitialPeriodStart: sub.StartDate,
		EndDate:            &until,
		Anchor:             sub.BillingAnchor,
		PeriodCount:        sub.BillingPeriodCount,
		BillingPeriod:      sub.BillingPeriod,
		Timezone:           sub.Timezone,
	})
	require.NoError(t, err)
	return periods
}

// The period holding any instant has the edges generating every period from the subscription start gives.
func TestPeriodAt_MatchesFullGeneration(t *testing.T) {
	for name, sub := range periodGenerationFixtures(t) {
		t.Run(name, func(t *testing.T) {
			full := fullPeriods(t, sub, sub.StartDate.AddDate(4, 0, 0))
			full = full[:len(full)-1] // the last period is cut at the generation bound
			var instants []time.Time
			for _, p := range full {
				instants = append(instants, p.Start, p.Start.Add(time.Nanosecond), p.End.Add(-time.Nanosecond))
			}
			for at := sub.StartDate; at.Before(full[len(full)-1].End); at = at.Add(37*time.Hour + 13*time.Minute) {
				instants = append(instants, at)
			}

			for _, at := range instants {
				want, ok := lo.Find(full, func(p types.Period) bool { return !at.Before(p.Start) && at.Before(p.End) })
				require.True(t, ok, "no reference period holds %s", at)
				got, err := periodAt(sub, at)
				require.NoError(t, err)
				require.True(t, want.Start.Equal(got.Start) && want.End.Equal(got.End), "at %s: want %v, got %v", at, want, got)
			}
		})
	}
}

// periodEndAtFromStart is periodEndAt generating every period from the subscription start.
func periodEndAtFromStart(t *testing.T, sub *subscription.Subscription, at time.Time) time.Time {
	horizon, err := types.NextBillingDate(&types.NextBillingDateParams{
		CurrentPeriodStart:  at,
		BillingAnchor:       sub.BillingAnchor,
		Unit:                sub.BillingPeriodCount,
		Period:              sub.BillingPeriod,
		SubscriptionEndDate: sub.EndDate,
		Timezone:            sub.Timezone,
	})
	require.NoError(t, err)
	for _, p := range fullPeriods(t, sub, horizon) {
		if !p.End.Before(at) {
			return p.End
		}
	}

	return at
}

func TestPeriodEndAt_MatchesFullGeneration(t *testing.T) {
	for name, sub := range periodGenerationFixtures(t) {
		for _, ends := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, subscription end %t", name, ends), func(t *testing.T) {
				full := fullPeriods(t, sub, sub.StartDate.AddDate(3, 0, 0))
				s := *sub
				if ends {
					s.EndDate = lo.ToPtr(full[len(full)/2].Start.Add(5 * time.Hour))
				}

				// The reference walks from the start per instant, so sample about a dozen edges and instants.
				var instants []time.Time
				for i := 0; i < len(full); i += max(len(full)/12, 1) {
					end := full[i].End
					instants = append(instants, end, end.Add(time.Nanosecond), end.Add(-time.Nanosecond))
				}
				step := full[len(full)-1].Start.Sub(sub.StartDate) / 12
				for at := sub.StartDate.Add(time.Minute); at.Before(full[len(full)-1].Start); at = at.Add(step) {
					instants = append(instants, at)
				}

				for _, at := range instants {
					got, err := periodEndAt(&s, at)
					require.NoError(t, err)
					want := periodEndAtFromStart(t, &s, at)
					require.True(t, want.Equal(got), "at %s: want %s, got %s", at, want, got)
				}
			})
		}
	}
}

// Starting at the period holding the window yields the tail of the periods generated from the subscription start.
func TestCommitmentPeriods_FromWindowMatchesFullGeneration(t *testing.T) {
	for name, sub := range periodGenerationFixtures(t) {
		for _, openEnded := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, open-ended %t", name, openEnded), func(t *testing.T) {
				c := &commitment.Commitment{
					Type:        types.COMMITMENT_TYPE_AMOUNT,
					Value:       decimal.NewFromInt(100),
					Measurement: types.CommitmentMeasurementBillingPeriod,
					Timezone:    "UTC",
					StartDate:   sub.StartDate,
				}
				through := sub.StartDate.AddDate(3, 0, 0)
				if !openEnded {
					c.EndDate = lo.ToPtr(through)
				}

				all, err := commitmentPeriods(c, sub, identityAmount, "", c.StartDate, through)
				require.NoError(t, err)
				require.Greater(t, len(all), 3)

				for _, i := range []int{1, len(all) / 3, len(all) - 1} {
					from := all[i].Start.Add(time.Hour)
					got, err := commitmentPeriods(c, sub, identityAmount, "", from, through)
					require.NoError(t, err)
					require.Equal(t, all[i:], got, "from %s", from)
				}
			})
		}
	}
}
