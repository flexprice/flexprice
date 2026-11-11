package analytics

import (
	"encoding/json"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func billingDef(shape types.Shape, dims ...string) ViewDefinition {
	return ViewDefinition{
		Family:     types.FamilyBilling,
		Shape:      shape,
		Metrics:    []types.Metric{types.MetricInvoiceTotal},
		Dimensions: dims,
		Time:       TimeSpecRaw{Range: "last_30_days", Grain: types.GrainDay},
	}
}

func requireValidationErr(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err), "want validation error, got %v", err)
}

func TestBillingValidate_AcceptsPilotShapes(t *testing.T) {
	require.NoError(t, billingDef(types.ShapeScalar).Validate())
	require.NoError(t, billingDef(types.ShapeTimeseries, "currency").Validate())
	require.NoError(t, billingDef(types.ShapeBreakdown, "customer_id").Validate())
}

func TestBillingValidate_RejectsUnknownDimension(t *testing.T) {
	requireValidationErr(t, billingDef(types.ShapeBreakdown, "meter_id").Validate())
	requireValidationErr(t, billingDef(types.ShapeBreakdown, "currency; DROP TABLE x").Validate())
}

func TestBillingValidate_RejectsDuplicateDimension(t *testing.T) {
	requireValidationErr(t, billingDef(types.ShapeTimeseries, "currency", "currency").Validate())
}

func TestBillingValidate_RejectsScalarSortOrLimit(t *testing.T) {
	d := billingDef(types.ShapeScalar)
	d.Limit = 5
	requireValidationErr(t, d.Validate())
	d = billingDef(types.ShapeScalar)
	d.Sort = []*SortSpec{{Field: "invoice_total"}}
	requireValidationErr(t, d.Validate())
}

func TestBillingValidate_RejectsBreakdownWithoutDimension(t *testing.T) {
	requireValidationErr(t, billingDef(types.ShapeBreakdown).Validate())
}

func TestBillingValidate_RejectsHourGrain(t *testing.T) {
	d := billingDef(types.ShapeTimeseries)
	d.Time.Grain = types.GrainHour
	requireValidationErr(t, d.Validate())
}

func TestBillingValidate_RejectsMultipleOrUsageMetrics(t *testing.T) {
	d := billingDef(types.ShapeScalar)
	d.Metrics = []types.Metric{types.MetricInvoiceTotal, types.MetricInvoiceTotal}
	requireValidationErr(t, d.Validate())
	d.Metrics = []types.Metric{types.MetricEventCount}
	requireValidationErr(t, d.Validate())
}

func TestBillingValidate_RejectsFilterOnNonAllowlistedField(t *testing.T) {
	d := billingDef(types.ShapeScalar)
	d.Filters = []*Filter{{Field: "tenant_id", Op: types.EQUAL, Value: []string{"other"}}}
	requireValidationErr(t, d.Validate())
}

func TestUsageValidate_RejectsBillingMetric(t *testing.T) {
	d := ViewDefinition{Shape: types.ShapeBreakdown, Metrics: []types.Metric{types.MetricInvoiceTotal}}
	requireValidationErr(t, d.Validate())
}

func TestValidate_LegacyJSONWithoutFamilyIsUsage(t *testing.T) {
	var d ViewDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"shape":"breakdown","metrics":["usage_quantity"],"dimensions":["meter_id"]}`), &d))
	assert.Equal(t, types.Family(""), d.Family)
	require.NoError(t, d.Validate())
}

func TestValidate_RejectsUnknownFamily(t *testing.T) {
	d := billingDef(types.ShapeScalar)
	d.Family = "raw"
	requireValidationErr(t, d.Validate())
}

func TestResolveVariables_CarriesFamily(t *testing.T) {
	d := billingDef(types.ShapeScalar)
	rv, err := ResolveVariables(&d, nil)
	require.NoError(t, err)
	assert.Equal(t, types.FamilyBilling, rv.Family)
}

func TestLookupBillingMetric_InvoiceTotalForcesCurrency(t *testing.T) {
	bm, ok := LookupBillingMetric(types.MetricInvoiceTotal)
	require.True(t, ok)
	assert.True(t, bm.Currency)
}

func TestBillingValidate_SnapshotRejectsTimeseries(t *testing.T) {
	withMetric(t, &BillingMetric{
		Metric:     "test_snap_v",
		Dimensions: []string{"currency"},
		Shapes:     []types.Shape{types.ShapeScalar, types.ShapeTimeseries}, Snapshot: true,
	})
	d := billingDef(types.ShapeTimeseries)
	d.Metrics = []types.Metric{"test_snap_v"}
	requireValidationErr(t, d.Validate())
}
