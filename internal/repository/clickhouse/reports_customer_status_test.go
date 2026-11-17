package clickhouse

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const statusSubquery = "SELECT id FROM reports_db.customers WHERE tenant_id = {tenant_id:String} AND environment_id = {environment_id:String} " +
	"GROUP BY id HAVING argMax(_peerdb_is_deleted, _peerdb_version) = 0 AND argMax(status, _peerdb_version) IN ({f0_0:String})"

func TestBuildBillingQuery_CustomerStatusFilter(t *testing.T) {
	rv := billingRV(types.ShapeBreakdown, "customer_id")
	rv.Filters = []*analytics.Filter{{Field: analytics.FilterCustomerStatus, Op: types.EQUAL, Value: []string{"archived"}}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "customer_id IN ("+statusSubquery+")")
	assert.Equal(t, "archived", q.Params["f0_0"])
}

func TestBuildBillingQuery_CustomerStatusValuesAreParams(t *testing.T) {
	rv := billingRV(types.ShapeScalar)
	evil := "x') OR 1=1 --"
	rv.Filters = []*analytics.Filter{{Field: analytics.FilterCustomerStatus, Op: types.IN, Value: []string{"published", evil}}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "IN ({f0_0:String}, {f0_1:String})")
	assert.NotContains(t, q.SQL, "OR 1=1")
	assert.Equal(t, evil, q.Params["f0_1"])
}

func TestBuildBillingQuery_CustomerStatusEmptyMatchesNothing(t *testing.T) {
	rv := billingRV(types.ShapeScalar)
	rv.Filters = []*analytics.Filter{{Field: analytics.FilterCustomerStatus, Op: types.IN, Value: nil}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "1 = 0")
	assert.NotContains(t, q.SQL, "customers")
}

func TestBuildBillingQuery_CustomerStatusRejectedWithoutCustomerDimension(t *testing.T) {
	rv := customRV(types.MetricTotalReceivables, types.ShapeScalar)
	rv.Filters = []*analytics.Filter{{Field: analytics.FilterCustomerStatus, Op: types.EQUAL, Value: []string{"archived"}}}
	_, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.Error(t, err)
}

func TestBuildCreditConsumption_CustomerStatusFilter(t *testing.T) {
	rv := customRV(types.MetricCreditConsumption, types.ShapeBreakdown)
	rv.Dimensions = []string{"customer_id"}
	rv.Filters = []*analytics.Filter{{Field: analytics.FilterCustomerStatus, Op: types.EQUAL, Value: []string{"published"}}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 100)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "d.customer_id IN (SELECT id FROM reports_db.customers WHERE")
	assert.Equal(t, "published", q.Params["f0_0"])
}
