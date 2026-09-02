package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A grant whose period starts at local midnight must expire at local midnight too, so that a
// one-month expiry agrees with the period end NextBillingDate derives for the same subscription.
func TestAddExpiryDurationRespectsTimezone(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	assert.NoError(t, err)

	// 2026-03-01 00:00 IST, stored as the UTC instant 2026-02-28T18:30:00Z
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, ist).UTC()

	got, ok := AddExpiryDuration(start, 1, CreditGrantExpiryDurationUnitMonths, "Asia/Kolkata")
	assert.True(t, ok)
	assert.Equal(t, time.Date(2026, 4, 1, 0, 0, 0, 0, ist), got.In(ist))

	// Same input computed in UTC lands on the 28th — the behaviour the timezone fixes.
	gotUTC, ok := AddExpiryDuration(start, 1, CreditGrantExpiryDurationUnitMonths, DefaultTimezone)
	assert.True(t, ok)
	assert.Equal(t, time.Date(2026, 3, 28, 18, 30, 0, 0, time.UTC), gotUTC)
}

// DAY and WEEK previously added a fixed 24h*n, which drifts an hour across a DST transition.
func TestAddExpiryDurationHoldsLocalClockAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	assert.NoError(t, err)

	// 2026-03-05 00:00 EST; US spring-forward falls on 2026-03-08
	start := time.Date(2026, 3, 5, 0, 0, 0, 0, ny).UTC()

	for _, tc := range []struct {
		unit     CreditGrantExpiryDurationUnit
		duration int
		want     time.Time
	}{
		{CreditGrantExpiryDurationUnitDays, 7, time.Date(2026, 3, 12, 0, 0, 0, 0, ny)},
		{CreditGrantExpiryDurationUnitWeeks, 1, time.Date(2026, 3, 12, 0, 0, 0, 0, ny)},
	} {
		got, ok := AddExpiryDuration(start, tc.duration, tc.unit, "America/New_York")
		assert.True(t, ok)
		assert.Equal(t, tc.want, got.In(ny), "unit %s", tc.unit)
	}
}

func TestAddExpiryDurationUnknownUnit(t *testing.T) {
	_, ok := AddExpiryDuration(time.Now().UTC(), 1, CreditGrantExpiryDurationUnit("DECADE"), DefaultTimezone)
	assert.False(t, ok)
}

// An unresolvable or empty timezone falls back to UTC rather than erroring, matching loadTimezone.
func TestAddExpiryDurationFallsBackToUTC(t *testing.T) {
	start := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	want := time.Date(2027, 1, 31, 9, 0, 0, 0, time.UTC)

	for _, tz := range []string{"", "Not/AZone"} {
		got, ok := AddExpiryDuration(start, 1, CreditGrantExpiryDurationUnitYears, tz)
		assert.True(t, ok)
		assert.Equal(t, want, got, "tz %q", tz)
	}
}
