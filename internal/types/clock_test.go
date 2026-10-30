package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNow_WholeMicrosecondsUTC(t *testing.T) {
	now := Now()

	require.Equal(t, time.UTC, now.Location())
	require.Equal(t, now.Truncate(time.Microsecond), now,
		"Now() must be born at Postgres microsecond precision")
	require.WithinDuration(t, time.Now().UTC(), now, time.Second)
}
