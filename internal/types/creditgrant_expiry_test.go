package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCreditsExpiry(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	day := CreditGrantExpiryDurationUnitDays
	week := CreditGrantExpiryDurationUnitWeeks
	month := CreditGrantExpiryDurationUnitMonths
	year := CreditGrantExpiryDurationUnitYears
	invalid := CreditGrantExpiryDurationUnit("HOUR")
	n := 3

	assert.Nil(t, ResolveCreditsExpiry(nil, &day, now))
	assert.Nil(t, ResolveCreditsExpiry(&n, nil, now))
	assert.Nil(t, ResolveCreditsExpiry(&n, &invalid, now))

	got := ResolveCreditsExpiry(&n, &day, now)
	require.NotNil(t, got)
	assert.Equal(t, now.Add(3*24*time.Hour), *got)

	got = ResolveCreditsExpiry(&n, &week, now)
	require.NotNil(t, got)
	assert.Equal(t, now.Add(3*7*24*time.Hour), *got)

	got = ResolveCreditsExpiry(&n, &month, now)
	require.NotNil(t, got)
	assert.Equal(t, now.AddDate(0, 3, 0), *got)

	got = ResolveCreditsExpiry(&n, &year, now)
	require.NotNil(t, got)
	assert.Equal(t, now.AddDate(3, 0, 0), *got)
}
