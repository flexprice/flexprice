package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateGroupByProperty_RejectsInjectionShapedPropertyNames guards the fix in
// feature_usage.go (getStandardAnalytics, getMaxBucketTotals, getSumBucketTotals),
// costsheet_usage.go (getStandardAnalytics, getMaxBucketTotals), and
// processed_event.go (GetDetailedUsageAnalytics): the group_by "properties.X" name is
// interpolated both as a SQL string literal (JSONExtractString(properties, '<name>'))
// and as a raw column-alias identifier, so it can never be bound via `?` — it must be
// validated against a strict allow-list before use. This test proves the guard rejects
// the exact live-verified PoC shape and accepts safe names.
func TestValidateGroupByProperty_RejectsInjectionShapedPropertyNames(t *testing.T) {
	maliciousNames := []string{
		"x') OR 1=1 -- ",
		"x' OR '1'='1",
		"org_id'); DROP TABLE feature_usage; --",
		"org id", // space is also disallowed by the allow-list
	}
	for _, name := range maliciousNames {
		err := validateGroupByProperty(name)
		assert.Error(t, err, "expected validateGroupByProperty to reject %q", name)
	}
}

func TestValidateGroupByProperty_AcceptsSafeNames(t *testing.T) {
	safeNames := []string{"org_id", "krn", "region.code", "a1_b2", ""}
	for _, name := range safeNames {
		err := validateGroupByProperty(name)
		assert.NoError(t, err, "expected validateGroupByProperty to accept %q", name)
	}
}
