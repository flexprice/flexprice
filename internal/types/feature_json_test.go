package types

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportingUnit_ConversionRateEncodesAsNumber(t *testing.T) {
	rate := decimal.RequireFromString("0.001")
	unit := ReportingUnit{
		UnitSingular:   "second",
		UnitPlural:     "seconds",
		ConversionRate: &rate,
	}

	raw, err := json.Marshal(unit)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.Equal(t, 0.001, payload["conversion_rate"])
	assert.Equal(t, "second", payload["unit_singular"])

	var sdk struct {
		ConversionRate *float64 `json:"conversion_rate"`
	}
	require.NoError(t, json.Unmarshal(raw, &sdk))
	require.NotNil(t, sdk.ConversionRate)
	assert.Equal(t, 0.001, *sdk.ConversionRate)

	var roundTrip ReportingUnit
	require.NoError(t, json.Unmarshal(raw, &roundTrip))
	require.NotNil(t, roundTrip.ConversionRate)
	assert.True(t, rate.Equal(*roundTrip.ConversionRate))

	legacy := []byte(`{"unit_singular":"second","unit_plural":"seconds","conversion_rate":"0.001"}`)
	var stored ReportingUnit
	require.NoError(t, json.Unmarshal(legacy, &stored))
	require.NotNil(t, stored.ConversionRate)
	assert.True(t, rate.Equal(*stored.ConversionRate))
}
