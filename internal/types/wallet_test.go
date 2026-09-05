package types

import (
	"testing"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAutoTopup_NormalisesAbsentCooldown(t *testing.T) {
	tests := []struct {
		name     string
		cooldown *Duration
		want     *Duration
	}{
		{"absent becomes the clear signal", nil, &Duration{}},
		{"zero value stays the clear signal", &Duration{}, &Duration{}},
		{"a real cooloff is kept", &Duration{Value: 6, Unit: DurationUnitHour}, &Duration{Value: 6, Unit: DurationUnitHour}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAutoTopup(true, lo.ToPtr(decimal.NewFromInt(10)), lo.ToPtr(decimal.NewFromInt(50)), true, tt.cooldown)
			require.NotNil(t, got.Cooldown)
			assert.Equal(t, tt.want, got.Cooldown)
			assert.True(t, lo.FromPtr(got.Enabled))
			assert.True(t, lo.FromPtr(got.Invoicing))
		})
	}
}

func TestAutoTopupBuilder_LeavesUnsetFieldsAlone(t *testing.T) {
	existing := &AutoTopup{
		Enabled:   lo.ToPtr(true),
		Threshold: lo.ToPtr(decimal.NewFromInt(10)),
		Amount:    lo.ToPtr(decimal.NewFromInt(50)),
		Invoicing: lo.ToPtr(true),
		Cooldown:  &Duration{Value: 30, Unit: DurationUnitMinute},
	}

	got := NewAutoTopupBuilder(existing).WithEnabled(lo.ToPtr(false)).Build()

	assert.False(t, lo.FromPtr(got.Enabled))
	assert.Equal(t, decimal.NewFromInt(10), lo.FromPtr(got.Threshold))
	assert.Equal(t, decimal.NewFromInt(50), lo.FromPtr(got.Amount))
	require.NotNil(t, got.Cooldown)
	assert.Equal(t, 30, got.Cooldown.Value)
}

func TestAutoTopupBuilder_DoesNotMutateTheSource(t *testing.T) {
	existing := &AutoTopup{Enabled: lo.ToPtr(true), Cooldown: &Duration{Value: 30, Unit: DurationUnitMinute}}

	NewAutoTopupBuilder(existing).WithEnabled(lo.ToPtr(false)).WithCooldown(&Duration{}).Build()

	assert.True(t, lo.FromPtr(existing.Enabled), "the stored config must survive a failed update")
	require.NotNil(t, existing.Cooldown)
}

func TestAutoTopupBuilder_Cooldown(t *testing.T) {
	stored := &Duration{Value: 30, Unit: DurationUnitMinute}
	tests := []struct {
		name  string
		given *Duration
		want  *Duration
	}{
		{"nil leaves the stored cooloff", nil, stored},
		{"empty clears it", &Duration{}, nil},
		{"a value replaces it", &Duration{Value: 6, Unit: DurationUnitHour}, &Duration{Value: 6, Unit: DurationUnitHour}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewAutoTopupBuilder(&AutoTopup{Cooldown: stored}).WithCooldown(tt.given).Build()
			assert.Equal(t, tt.want, got.Cooldown)
		})
	}
}

// The portal submits the whole form, so its output has to survive the merge that
// UpdateWallet performs on top of a stored config.
func TestNewAutoTopup_ClearsThroughTheBuilder(t *testing.T) {
	stored := &AutoTopup{
		Threshold: lo.ToPtr(decimal.NewFromInt(10)),
		Amount:    lo.ToPtr(decimal.NewFromInt(50)),
		Cooldown:  &Duration{Value: 30, Unit: DurationUnitMinute},
	}
	req := NewAutoTopup(false, nil, nil, true, nil)

	got := NewAutoTopupBuilder(stored).WithAutoTopup(req).Build()

	assert.Nil(t, got.Cooldown, "an absent cooloff must survive the merge as a clear")
	assert.Equal(t, decimal.NewFromInt(10), lo.FromPtr(got.Threshold), "fields the form omits are preserved")
}
