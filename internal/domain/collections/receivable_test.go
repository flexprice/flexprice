package collections

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsOverdue(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	before, after := now.Add(-time.Second), now.Add(time.Second)

	require.True(t, IsOverdue(&before, now))
	require.False(t, IsOverdue(&now, now))
	require.False(t, IsOverdue(&after, now))
	require.False(t, IsOverdue(nil, now))
}
