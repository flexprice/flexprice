package selfbilling

import (
	"encoding/json"
	"testing"

	sdktypes "github.com/flexprice/go-sdk/v2/models/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/types"
)

func TestPortalSDKDecodesSummaryDecimals(t *testing.T) {
	factor := decimal.RequireFromString("1.5")
	bucketRaw, err := json.Marshal(types.TimeOfDayBucket{
		CommitmentValue: decimal.RequireFromString("10"),
		OverageFactor:   &factor,
		Start:           types.Bucket{Hour: 9},
		End:             types.Bucket{Hour: 17},
	})
	require.NoError(t, err)

	var bucket sdktypes.TimeOfDayBucket
	require.NoError(t, json.Unmarshal(bucketRaw, &bucket))
	require.NotNil(t, bucket.OverageFactor)
	assert.Equal(t, "1.5", *bucket.OverageFactor)

	rate := decimal.RequireFromString("0.001")
	unitRaw, err := json.Marshal(types.ReportingUnit{
		UnitSingular:   "second",
		UnitPlural:     "seconds",
		ConversionRate: &rate,
	})
	require.NoError(t, err)

	var unit sdktypes.ReportingUnit
	require.NoError(t, json.Unmarshal(unitRaw, &unit))
	require.NotNil(t, unit.ConversionRate)
	assert.Equal(t, 0.001, *unit.ConversionRate)
}
