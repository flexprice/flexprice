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

func TestCommitmentPeriods(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		c             *commitment.Commitment
		committedRate decimal.Decimal
		bucket        types.WindowSize
		wantPeriods   int
		wantFirst     string
		wantTotal     string
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
			committedRate: decimal.NewFromInt(1),
			wantPeriods:   12,
			wantFirst:     "100",
			wantTotal:     "1200",
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
			committedRate: decimal.NewFromInt(1),
			wantPeriods:   12,
			wantFirst:     "54.83870968",
			wantTotal:     "1154.83870968",
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
			committedRate: decimal.RequireFromString("0.048"),
			bucket:        types.WindowSizeMinute,
			wantPeriods:   1,
			wantFirst:     "17141.76",
			wantTotal:     "17141.76",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			periods, err := commitmentPeriods(tc.c, monthlySub(jan), tc.committedRate, tc.bucket)
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
		decimal.RequireFromString("0.0491667"),
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

			periods, err := commitmentPeriods(c, tc.sub, decimal.NewFromInt(1), "")
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
		decimal.NewFromInt(1),
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

	periods, err := commitmentPeriods(c, monthlySub(jan), decimal.NewFromInt(1), types.WindowSizeWeek)
	require.NoError(t, err)
	require.Len(t, periods, 2)
	require.Equal(t, int64(4), periods[0].ActiveBuckets) // Mondays 5, 12, 19, 26 Jan; 29 Dec starts before the term
	require.Equal(t, int64(4), periods[1].ActiveBuckets) // Mondays 2, 9, 16, 23 Feb; 26 Jan already counted in January
	require.True(t, commitmentValue(periods).Equal(decimal.NewFromInt(8)))
}
