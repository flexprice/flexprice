package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateGroupByProperty_RejectsInjectionShapedPropertyNames guards the fix in
// costsheet_usage.go and processed_event.go (both in GetDetailedUsageAnalytics): the
// group_by "properties.X" name is interpolated both as a SQL string literal
// (JSONExtractString(properties, '<name>')) and as a raw column-alias identifier, so it
// can never be bound via `?` — it must be validated against a strict allow-list before
// use. This test proves the guard rejects the exact live-verified PoC shape and accepts
// safe names.
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
	// Note: "" is accepted by the validator itself; callers reject empty property names
	// separately, since "properties." carries no field to group on.
	safeNames := []string{"org_id", "krn", "region.code", "a1_b2"}
	for _, name := range safeNames {
		err := validateGroupByProperty(name)
		assert.NoError(t, err, "expected validateGroupByProperty to accept %q", name)
	}
}

// TestGroupByPropertyValidation_RejectsRatherThanDrops pins the invariant that makes the
// SELECT column list and the scan targets stay in lockstep: an invalid group_by property
// must be rejected up front with ErrValidation, never silently dropped from the query.
// Dropping it would leave the scan sized off len(params.GroupBy) while the SELECT emitted
// one column fewer, turning a bad request into a row-scan failure (HTTP 500) — and, where
// the aggregation still ran, would silently collapse rows across an intended dimension.
func TestGroupByPropertyValidation_RejectsRatherThanDrops(t *testing.T) {
	for _, propertyName := range []string{"x') OR 1=1 -- ", "org id", "a-b"} {
		assert.Error(t, validateGroupByProperty(propertyName),
			"group_by property %q must be rejected, not dropped", propertyName)
	}
}

// TestGroupByPropertyAlias_DotFreeAndCollisionFree pins the alias contract the analytics
// SELECT/scan paths rely on: the alias must contain no dot (a raw "region.code" alias is
// parsed by ClickHouse as a qualified identifier, breaking the outer query) and must be
// injective (a plain dots->underscores scheme collides "region.code" with "region_code",
// producing duplicate column aliases that ClickHouse rejects).
func TestGroupByPropertyAlias_DotFreeAndCollisionFree(t *testing.T) {
	// dot-free
	for _, name := range []string{"region.code", "a.b.c", "org_id", "region_code"} {
		assert.NotContains(t, groupByPropertyAlias(name), ".",
			"alias for %q must not contain a dot", name)
	}

	// injective across names that a naive scheme would collide
	names := []string{"region.code", "region_code", "a.b", "a_b", "a", "org_id", "org.id"}
	seen := map[string]string{}
	for _, name := range names {
		alias := groupByPropertyAlias(name)
		if prev, dup := seen[alias]; dup {
			assert.Failf(t, "alias collision", "%q and %q both map to alias %q", prev, name, alias)
		}
		seen[alias] = name
	}
}
