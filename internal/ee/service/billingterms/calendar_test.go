package billingterms

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCalendarUTCYearBounds(t *testing.T) {
	for _, value := range []string{"0001-01-01T00:30:00+01:00", "9999-12-31T23:30:00-01:00"} {
		t.Run(value, func(t *testing.T) {
			boundary := date(t, value)
			_, err := NewCalendar(boundary, "UTC")
			require.Error(t, err)

			req := testRequest(t)
			req.Rows = []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}
			if boundary.Before(req.Owner.StartDate) {
				req.Owner.StartDate = boundary
			} else {
				req.Owner.EndDate = boundary
			}
			result, err := NewPlanner().Plan(context.Background(), req)
			require.Error(t, err)
			require.Nil(t, result)
		})
	}

	for _, value := range []string{"0001-01-01T01:30:00+01:00", "9999-12-31T22:30:00-01:00"} {
		t.Run(value, func(t *testing.T) {
			boundary := date(t, value)
			_, err := NewCalendar(boundary, "UTC")
			require.NoError(t, err)
			req := testRequest(t)
			req.Rows = []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}
			if boundary.Before(req.Owner.StartDate) {
				req.Owner.StartDate = boundary
			} else {
				req.Owner.EndDate = boundary
			}
			result, err := NewPlanner().Plan(context.Background(), req)
			require.NoError(t, err)
			_, err = json.Marshal(result.Items())
			require.NoError(t, err)
		})
	}
}

func TestCalendarBoundaries(t *testing.T) {
	anchor := date(t, "2028-01-31T00:00:00Z")
	calendar := testCalendar(t, anchor, "UTC")
	end := date(t, "2028-05-15T00:00:00Z")
	first, err := calendar.Next(anchor, end, types.BILLING_PERIOD_MONTHLY, 1)
	require.NoError(t, err)
	require.Equal(t, date(t, "2028-02-29T00:00:00Z"), first)
	next, err := calendar.Next(first, end, types.BILLING_PERIOD_MONTHLY, 1)
	require.NoError(t, err)
	require.Equal(t, date(t, "2028-03-31T00:00:00Z"), next)
	for _, interval := range []types.BillingPeriod{types.BILLING_PERIOD_DAILY, types.BILLING_PERIOD_WEEKLY, types.BILLING_PERIOD_MONTHLY, types.BILLING_PERIOD_QUARTER, types.BILLING_PERIOD_HALF_YEAR, types.BILLING_PERIOD_ANNUAL} {
		got, err := calendar.Next(anchor, end, interval, math.MaxInt32)
		require.NoError(t, err)
		require.Equal(t, end, got)
	}
}

func TestCalendarRejectsInvalidInputs(t *testing.T) {
	_, err := NewCalendar(time.Time{}, "UTC")
	require.Error(t, err)
	_, err = NewCalendar(date(t, "2027-01-01T00:00:00Z"), "invalid")
	require.Error(t, err)
	var calendar *calendar
	_, err = calendar.Next(time.Now(), time.Now(), types.BILLING_PERIOD_DAILY, 1)
	require.Error(t, err)
}

func TestCalendarDailyAndWeeklyAnchoredGrid(t *testing.T) {
	for _, test := range []struct {
		name, anchor, start, end, timezone, want string
		interval                                 types.BillingPeriod
		count                                    int
	}{
		{"daily partial", "2027-01-01T12:00:00Z", "2027-01-15T09:00:00Z", "2027-02-01T00:00:00Z", "UTC", "2027-01-15T12:00:00Z", types.BILLING_PERIOD_DAILY, 1},
		{"daily multiple", "2027-01-01T12:00:00Z", "2027-01-02T09:00:00Z", "2027-02-01T00:00:00Z", "UTC", "2027-01-03T12:00:00Z", types.BILLING_PERIOD_DAILY, 2},
		{"daily before anchor", "2027-01-15T12:00:00Z", "2027-01-02T09:00:00Z", "2027-02-01T00:00:00Z", "UTC", "2027-01-03T12:00:00Z", types.BILLING_PERIOD_DAILY, 2},
		{"large stride before anchor", "2027-01-15T12:00:00Z", "2027-01-02T09:00:00Z", "2027-02-01T00:00:00Z", "UTC", "2027-01-15T12:00:00Z", types.BILLING_PERIOD_DAILY, math.MaxInt32},
		{"weekly partial", "2027-01-01T12:00:00Z", "2027-01-15T09:00:00Z", "2027-02-01T00:00:00Z", "UTC", "2027-01-15T12:00:00Z", types.BILLING_PERIOD_WEEKLY, 1},
		{"weekly multiple", "2027-01-01T12:00:00Z", "2027-01-08T09:00:00Z", "2027-02-01T00:00:00Z", "UTC", "2027-01-15T12:00:00Z", types.BILLING_PERIOD_WEEKLY, 2},
		{"DST wall clock", "2027-03-07T17:00:00Z", "2027-03-14T13:00:00Z", "2027-04-01T00:00:00Z", "America/New_York", "2027-03-14T16:00:00Z", types.BILLING_PERIOD_WEEKLY, 1},
		{"exact boundary advances", "2027-01-01T12:00:00Z", "2027-01-15T12:00:00Z", "2027-02-01T00:00:00Z", "UTC", "2027-01-16T12:00:00Z", types.BILLING_PERIOD_DAILY, 1},
		{"clips partial", "2027-01-01T12:00:00Z", "2027-01-15T09:00:00Z", "2027-01-15T10:00:00Z", "UTC", "2027-01-15T10:00:00Z", types.BILLING_PERIOD_DAILY, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calendar := testCalendar(t, date(t, test.anchor), test.timezone)
			next, err := calendar.Next(date(t, test.start), date(t, test.end), test.interval, test.count)
			require.NoError(t, err)
			require.Equal(t, date(t, test.want), next)
		})
	}
}

func TestOwnerTimezoneMustBeExplicit(t *testing.T) {
	for _, timezone := range []string{"", "Local", "Unknown/Zone"} {
		t.Run(timezone, func(t *testing.T) {
			req := testRequest(t)
			_, err := NewCalendar(req.Owner.StartDate, timezone)
			require.Error(t, err)
			req.Owner.Timezone = timezone
			req.Rows = []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder}}
			_, err = NewPlanner().Plan(context.Background(), req)
			require.Error(t, err)
			ctx, core, db := newCore(t)
			db.owner.Timezone = timezone
			_, err = core.Get(ctx, db.owner.Owner)
			require.Error(t, err)
		})
	}
}
