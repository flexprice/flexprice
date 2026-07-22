package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuration_Validate(t *testing.T) {
	assert.NoError(t, (*Duration)(nil).Validate())

	assert.Error(t, (&Duration{Value: 0, Unit: DurationUnitMinute}).Validate())
	assert.Error(t, (&Duration{Value: 5, Unit: ""}).Validate())
	assert.Error(t, (&Duration{Value: 5, Unit: DurationUnit("WEEK")}).Validate())

	assert.NoError(t, (&Duration{Value: 5, Unit: DurationUnitSecond}).Validate())
	assert.NoError(t, (&Duration{Value: 5, Unit: DurationUnitMinute}).Validate())
	assert.NoError(t, (&Duration{Value: 5, Unit: DurationUnitHour}).Validate())
	assert.NoError(t, (&Duration{Value: 5, Unit: DurationUnitDay}).Validate())
}

func TestDuration_ToDuration(t *testing.T) {
	_, err := (*Duration)(nil).ToDuration()
	assert.Error(t, err)

	got, err := (&Duration{Value: 3, Unit: DurationUnitSecond}).ToDuration()
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, got)

	got, err = (&Duration{Value: 3, Unit: DurationUnitMinute}).ToDuration()
	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, got)

	got, err = (&Duration{Value: 3, Unit: DurationUnitHour}).ToDuration()
	require.NoError(t, err)
	assert.Equal(t, 3*time.Hour, got)

	got, err = (&Duration{Value: 3, Unit: DurationUnitDay}).ToDuration()
	require.NoError(t, err)
	assert.Equal(t, 3*24*time.Hour, got)
}

func TestDuration_IsSet(t *testing.T) {
	assert.False(t, (*Duration)(nil).IsSet())
	assert.False(t, (&Duration{}).IsSet())
	assert.False(t, (&Duration{Value: 1}).IsSet())
	assert.True(t, (&Duration{Value: 1, Unit: DurationUnitMinute}).IsSet())
}
