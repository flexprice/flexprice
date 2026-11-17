package analytics

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func statusView(op types.FilterOperatorType, values ...string) ViewDefinition {
	return ViewDefinition{
		Family:     types.FamilyBilling,
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricInvoiceTotal},
		Dimensions: []string{"customer_id"},
		Filters:    []*Filter{{Field: FilterCustomerStatus, Op: op, Value: values}},
		Time:       TimeSpecRaw{Range: "last_30_days"},
		Limit:      10,
	}
}

func TestViewValidate_CustomerStatusValues(t *testing.T) {
	require.NoError(t, statusView(types.EQUAL, "archived").Validate())
	require.NoError(t, statusView(types.IN, "published", "archived").Validate())

	require.Error(t, statusView(types.EQUAL, "deleted").Validate())
	require.Error(t, statusView(types.IN, "published", "deleted").Validate())
	require.Error(t, statusView(types.EQUAL, "published", "archived").Validate())
}

func TestViewValidate_CustomerStatusPlaceholderDefersToResolution(t *testing.T) {
	def := statusView(types.EQUAL, "{{status}}")
	def.Variables = []*Variable{{Name: "status", Type: types.VariableTypeString, Required: true}}
	require.NoError(t, def.Validate())

	rv, err := ResolveVariables(&def, map[string][]string{"status": {"archived"}})
	require.NoError(t, err)
	require.Equal(t, []string{"archived"}, rv.Filters[0].Value)

	_, err = ResolveVariables(&def, map[string][]string{"status": {"deleted"}})
	require.Error(t, err)
}
