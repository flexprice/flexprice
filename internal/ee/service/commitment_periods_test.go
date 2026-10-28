package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
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
				EndDate:     jan.AddDate(1, 0, 0),
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
				EndDate:     jan.AddDate(1, 0, 0),
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
				EndDate:     time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC),
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
			periods, err := commitmentPeriods(tc.c, monthlySub(jan), tc.amountOf, tc.bucket)
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
		EndDate:   end.UTC(),
	}

	sub := monthlySub(time.Date(2026, 8, 1, 0, 0, 0, 0, ist).UTC())
	sub.Timezone = "Asia/Kolkata"

	periods, err := commitmentPeriods(
		c,
		sub,
		mulFn(decimal.RequireFromString("0.0491667")),
		types.WindowSizeMinute,
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
				EndDate:     tc.end,
			}

			periods, err := commitmentPeriods(c, tc.sub, mulFn(decimal.NewFromInt(1)), "")
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
		EndDate:     time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC),
	}

	periods, err := commitmentPeriods(
		c,
		monthlySub(jan),
		mulFn(decimal.NewFromInt(1)),
		types.WindowSizeHour,
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
		EndDate:     time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}

	periods, err := commitmentPeriods(c, monthlySub(jan), mulFn(decimal.NewFromInt(1)), types.WindowSizeWeek)
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
		EndDate:     jan.Add(12*time.Hour + 10*time.Second),
	}

	periods, err := commitmentPeriods(c, monthlySub(jan), mulFn(decimal.NewFromInt(1)), types.WindowSizeMinute)
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
				EndDate:     end,
			}
			periods, err := commitmentPeriods(c, monthlySub(tc.month), tc.amountOf, "")
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
		EndDate:     end,
	}

	periods, err := commitmentPeriods(c, monthlySub(jan), identityAmount, "")
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
	periods, err = commitmentPeriods(&daily, monthlySub(jan), identityAmount, types.WindowSizeDay)
	require.NoError(t, err)
	require.Len(t, periods, 13)
	require.Equal(t, int64(17), periods[0].ActiveBuckets)
	require.Equal(t, int64(14), periods[12].ActiveBuckets) // 1 to 14 Jan; the 15 Jan bucket starts at the end
	require.True(t, commitmentValue(periods).Equal(decimal.NewFromInt(365)), "total=%s", commitmentValue(periods))
}
