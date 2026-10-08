package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseEventLookupWindow(t *testing.T) {
	window, err := parseEventLookupWindow("", "")
	require.NoError(t, err)
	require.Equal(t, eventLookupDefaultLookback, window.End.Sub(window.Start))
	require.WithinDuration(t, time.Now().UTC(), window.End, time.Minute)

	window, err = parseEventLookupWindow("", "2026-09-01T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC), window.Start)

	window, err = parseEventLookupWindow("2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), window.Start)

	_, err = parseEventLookupWindow("2026-02-01T00:00:00Z", "2026-01-01T00:00:00Z")
	require.Error(t, err)

	_, err = parseEventLookupWindow("not-a-time", "")
	require.Error(t, err)
}
