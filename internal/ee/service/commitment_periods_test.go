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
		ID: "subs_1", StartDate: start, BillingAnchor: start, BillingPeriod: types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1, Timezone: "UTC", Currency: "usd",
	}
}

func TestCommitmentPeriods(t *testing.T) {
	jan := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		c           *commitment.Commitment
		rates       commitmentRates
		bucket      types.WindowSize
		wantPeriods int
		wantFirst   string
		wantTotal   string
	}{
		{
			name: "P1 amount, aligned 12 months",
			c: &commitment.Commitment{Type: types.COMMITMENT_TYPE_AMOUNT, Value: decimal.NewFromInt(100),
				Measurement: types.CommitmentMeasurementBillingPeriod, Timezone: "UTC", StartDate: jan, EndDate: jan.AddDate(1, 0, 0)},
			rates: commitmentRates{Committed: decimal.NewFromInt(1)}, wantPeriods: 12, wantFirst: "100", wantTotal: "1200",
		},
		{
			name: "P2 mid-period start on 15 Jan is prorated",
			c: &commitment.Commitment{Type: types.COMMITMENT_TYPE_AMOUNT, Value: decimal.NewFromInt(100),
				Measurement: types.CommitmentMeasurementBillingPeriod, Timezone: "UTC",
				StartDate: time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC), EndDate: jan.AddDate(1, 0, 0)},
			rates: commitmentRates{Committed: decimal.NewFromInt(1)}, wantPeriods: 12, wantFirst: "54.83870968", wantTotal: "1154.83870968",
		},
		{
			name: "P3 quantity per minute, August only",
			c: &commitment.Commitment{Type: types.COMMITMENT_TYPE_QUANTITY, Value: decimal.NewFromInt(8),
				Measurement: types.CommitmentMeasurementWindow, Timezone: "UTC",
				StartDate: time.Date(2027, 8, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2027, 9, 1, 0, 0, 0, 0, time.UTC)},
			rates: commitmentRates{Committed: decimal.RequireFromString("0.048")}, bucket: types.WindowSizeMinute,
			wantPeriods: 1, wantFirst: "17141.76", wantTotal: "17141.76",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			periods, err := commitmentPeriods(tc.c, monthlySub(jan), tc.rates, tc.bucket)
			require.NoError(t, err)
			require.Len(t, periods, tc.wantPeriods)
			require.True(t, periods[0].Amount.Round(8).Equal(decimal.RequireFromString(tc.wantFirst)), "first=%s", periods[0].Amount)
			require.True(t, commitmentValue(periods).Round(8).Equal(decimal.RequireFromString(tc.wantTotal)), "total=%s", commitmentValue(periods))
		})
	}
}

func TestCommitmentPeriods_ActiveWindowsIST(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	start := time.Date(2026, 8, 25, 0, 0, 0, 0, ist)
	end := time.Date(2026, 9, 1, 0, 0, 0, 0, ist)
	c := &commitment.Commitment{Type: types.COMMITMENT_TYPE_QUANTITY, Value: decimal.NewFromInt(32),
		Measurement: types.CommitmentMeasurementWindow, Timezone: "Asia/Kolkata",
		ActiveWindows: types.ActiveWindows{{Start: "09:00", End: "21:00"}}, StartDate: start.UTC(), EndDate: end.UTC()}
	sub := monthlySub(time.Date(2026, 8, 1, 0, 0, 0, 0, ist).UTC())
	sub.Timezone = "Asia/Kolkata"
	periods, err := commitmentPeriods(c, sub, commitmentRates{Committed: decimal.RequireFromString("0.0491667")}, types.WindowSizeMinute)
	require.NoError(t, err)
	require.Len(t, periods, 1)
	require.Equal(t, int64(7*12*60), periods[0].ActiveBuckets) // 7 days × 12 h × 60 min
}
