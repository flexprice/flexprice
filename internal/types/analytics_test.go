package types

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestFamily_Validate(t *testing.T) {
	for _, f := range []Family{"", FamilyUsage, FamilyBilling, FamilyReport} {
		require.NoError(t, f.Validate(), "family %q", f)
	}
	err := Family("raw").Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestShapeScalarAndInvoiceTotal(t *testing.T) {
	assert.Equal(t, Shape("scalar"), ShapeScalar)
	assert.Equal(t, Metric("invoice_total"), MetricInvoiceTotal)
}
