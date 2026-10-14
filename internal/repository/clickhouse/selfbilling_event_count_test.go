package clickhouse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelfBillingCountQuery(t *testing.T) {
	for _, want := range []string{
		"toStartOfHour(ingested_at, 'UTC') AS hour",
		"uniqExact(id)",
		"ingested_at >= ?", "ingested_at < ?",
		"GROUP BY tenant_id, environment_id, hour",
	} {
		assert.Contains(t, selfBillingCountQuery, want)
	}
	// No timestamp bound: lag only moves rows between ingest hours, so the timestamp column must not filter.
	assert.False(t, strings.Contains(selfBillingCountQuery, "timestamp"), "must not filter on the event timestamp")
}
