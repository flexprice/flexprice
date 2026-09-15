package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShapeConstants(t *testing.T) {
	assert.Equal(t, Shape("timeseries"), ShapeTimeseries)
	assert.Equal(t, Shape("breakdown"), ShapeBreakdown)
}

func TestGrain_ToWindowSize(t *testing.T) {
	assert.Equal(t, WindowSizeHour, GrainHour.ToWindowSize())
	assert.Equal(t, WindowSizeDay, GrainDay.ToWindowSize())
	assert.Equal(t, WindowSizeWeek, GrainWeek.ToWindowSize())
	assert.Equal(t, WindowSizeMonth, GrainMonth.ToWindowSize())
	assert.Equal(t, WindowSize(""), Grain("").ToWindowSize())
}
