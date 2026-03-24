package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateBucketStarts_EmptyRange(t *testing.T) {
	start := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeDay, nil)
	assert.Nil(t, out)

	end2 := time.Date(2024, 1, 9, 0, 0, 0, 0, time.UTC)
	out2 := generateBucketStarts(start, end2, types.WindowSizeDay, nil)
	assert.Nil(t, out2)
}

func TestGenerateBucketStarts_Day(t *testing.T) {
	start := time.Date(2024, 1, 10, 12, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 13, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeDay, nil)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 1, 11, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 1, 12, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Hour(t *testing.T) {
	start := time.Date(2024, 1, 10, 1, 30, 0, 0, time.UTC)
	end := time.Date(2024, 1, 10, 5, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeHour, nil)
	require.Len(t, out, 4)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 10, 1, 0, 0, 0, time.UTC)))
	assert.True(t, out[3].Equal(time.Date(2024, 1, 10, 4, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_NoAnchor(t *testing.T) {
	// No anchor: buckets align to period start (e.g. subscription created 15 Jan → 15 Jan, 15 Feb, 15 Mar)
	start := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, nil)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_NoAnchor_SubscriptionCreated5Feb(t *testing.T) {
	// Calendar subscription created 5 Feb: period 5 Feb - 5 Mar; buckets align to period start (5th)
	start := time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, nil)
	require.Len(t, out, 1)
	assert.True(t, out[0].Equal(time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_WithAnchor(t *testing.T) {
	// Anchor 5th: periods are 5th - 5th
	anchor := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC) // in period Jan 5 - Feb 5
	end := time.Date(2024, 3, 10, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, &anchor)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)))
}

func TestGenerateBucketStarts_Month_WithAnchor_StartBeforeAnchorDay(t *testing.T) {
	// Start is Jan 3; anchor 5th -> period containing Jan 3 is Dec 5 - Jan 5
	anchor := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)
	start := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 6, 0, 0, 0, 0, time.UTC)
	out := generateBucketStarts(start, end, types.WindowSizeMonth, &anchor)
	require.Len(t, out, 3)
	assert.True(t, out[0].Equal(time.Date(2023, 12, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[1].Equal(time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)))
	assert.True(t, out[2].Equal(time.Date(2024, 2, 5, 0, 0, 0, 0, time.UTC)))
}
