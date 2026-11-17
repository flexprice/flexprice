package analytics

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func statusQuery(values ...string) ReportQuery {
	return ReportQuery{
		Shape:      string(types.ShapeBreakdown),
		Dimensions: []string{"customer_id"},
		Limit:      10,
		Filters:    []ReportQueryFilter{{Field: FilterCustomerStatus, Op: "in", Value: values}},
	}
}

func TestReportQueryCustomerStatusFilter(t *testing.T) {
	e, ok := LookupReport(string(types.MetricInvoiceTotal))
	require.True(t, ok)

	for _, vals := range [][]string{{"published"}, {"archived"}, {"published", "archived"}} {
		q := statusQuery(vals...)
		require.NoError(t, q.ValidateAgainst(e, 1000), "values %v", vals)
	}

	bad := statusQuery("deleted")
	require.Error(t, bad.ValidateAgainst(e, 1000))

	noValue := statusQuery()
	require.Error(t, noValue.ValidateAgainst(e, 1000))
}

func TestReportQueryCustomerStatusNeedsCustomerDimension(t *testing.T) {
	e, ok := LookupReport(string(types.MetricTotalReceivables))
	require.True(t, ok)
	q := ReportQuery{
		Shape:   string(types.ShapeScalar),
		Filters: []ReportQueryFilter{{Field: FilterCustomerStatus, Op: "eq", Value: []string{"archived"}}},
	}
	require.Error(t, q.ValidateAgainst(e, 1000))
}

func TestValidateReport_AllowsCustomerStatusFilter(t *testing.T) {
	def := ViewDefinition{
		Family:     types.FamilyBilling,
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricInvoiceTotal},
		Dimensions: []string{"customer_id"},
		Filters:    []*Filter{{Field: FilterCustomerStatus, Op: types.EQUAL, Value: []string{"archived"}}},
		Time:       TimeSpecRaw{Range: "last_30_days"},
		Limit:      10,
	}
	require.NoError(t, def.ValidateReport(1000))

	def.Metrics = []types.Metric{types.MetricTotalReceivables}
	def.Shape = types.ShapeScalar
	def.Dimensions = nil
	def.Limit = 0
	require.Error(t, def.ValidateReport(1000))
}
