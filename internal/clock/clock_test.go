package clock

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

func TestTruncateTimes_InPlaceThroughNesting(t *testing.T) {
	type inner struct {
		At time.Time
	}
	type payload struct {
		At       time.Time
		EndsAt   *time.Time
		Items    []inner
		ByKey    map[string]time.Time
		Wrapped  any
		internal time.Time
	}

	at := time.Date(2026, 1, 10, 12, 0, 0, 123456789, time.UTC)
	endsAt := at
	want := time.Date(2026, 1, 10, 12, 0, 0, 123456000, time.UTC)
	p := payload{
		At:       at,
		EndsAt:   &endsAt,
		Items:    []inner{{At: at}},
		ByKey:    map[string]time.Time{"k": at},
		Wrapped:  &inner{At: at},
		internal: at,
	}

	TruncateTimes(&p)

	require.Equal(t, want, p.At)
	require.Equal(t, want, *p.EndsAt)
	require.Equal(t, want, p.Items[0].At)
	require.Equal(t, want, p.ByKey["k"])
	require.Equal(t, want, p.Wrapped.(*inner).At)
	require.Equal(t, at, p.internal, "unexported fields are not settable and stay untouched")
}
