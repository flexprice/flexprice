package clickhouse

import (
	"slices"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func colNames(cols []analytics.ReportColumn) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

func TestReportColumnsMatchRealBuilder(t *testing.T) {
	repo := &ReportsRepository{cfg: config.ReportsConfig{Database: "reports_db", UsageDatabase: "reports_db", MaxResultRows: 1000}}
	keys := []types.Metric{
		types.MetricInvoiceTotal, types.MetricInvoiceTotalIssued, types.MetricReceivablesAging,
		types.MetricDaysToPay, types.MetricCreditConsumption, types.MetricCreditTopups,
	}
	for _, key := range keys {
		entry, ok := analytics.LookupReport(string(key))
		require.True(t, ok, key)
		if !slices.ContainsFunc(entry.Dimensions, func(d analytics.ReportDimension) bool { return d.Key == "customer_id" }) {
			continue
		}
		for _, shape := range entry.Shapes {
			for _, compare := range []string{"", "previous_period"} {
				if compare != "" && !entry.SupportsCompare {
					continue
				}
				name := string(key) + "/" + string(shape) + "/" + compare
				t.Run(name, func(t *testing.T) {
					rv := billingRV(shape, "customer_id")
					rv.Metrics = []types.Metric{key}
					rv.Compare = compare
					rv.Filters = []*analytics.Filter{{Field: "customer_id", Op: types.IN, Value: []string{"c1"}}}

					real, err := buildBillingQuery(rv, "reports_db", "t", "e", 1000)
					if err != nil {
						t.Skipf("combination not runnable: %v", err)
					}
					got, err := repo.ReportColumns(rv, "t", "e")
					require.NoError(t, err)
					require.Equal(t, columnNames(real), colNames(got))
					require.Contains(t, colNames(got), "customer_id")
					if compare != "" {
						require.Contains(t, colNames(got), string(key)+"_prev")
					}
					if key == types.MetricCreditConsumption {
						require.Contains(t, colNames(got), "currency")
					}
				})
			}
		}
	}
}
