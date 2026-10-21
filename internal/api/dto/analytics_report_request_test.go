package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportQueryRequest_UnsupportedCompareAndNoCurrency(t *testing.T) {
	e := &analytics.ReportCatalogEntry{
		Key:          types.Metric("no_such_metric"),
		Shapes:       []types.Shape{types.ShapeScalar},
		DefaultShape: types.ShapeScalar,
	}
	err := (&ReportQueryRequest{Shape: "scalar", Compare: "previous_period"}).validateAgainst(e, 0)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))

	err = (&ReportQueryRequest{Shape: "scalar", Currency: "usd"}).validateAgainst(e, 0)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestReportQueryRequest_ValidateForReport(t *testing.T) {
	req := &ReportQueryRequest{Shape: "scalar", Range: "2026-09-01..2026-10-01"}
	_, err := req.ValidateForReport("nope", 0)
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))

	e, err := req.ValidateForReport("invoice_total", 0)
	require.NoError(t, err)
	assert.Equal(t, types.MetricInvoiceTotal, e.Key)
}
