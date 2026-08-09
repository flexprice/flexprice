package clickhouse

import (
	"regexp"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// validGroupByPropertyPattern matches safe property names (alphanumeric, underscores, dots).
var validGroupByPropertyPattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

// groupByPropertyAlias returns a dot-free, collision-free SQL column alias for a validated
// group_by property name. The property name cannot be used as the alias directly: dots are
// valid in property names (they address nested JSON keys) but ClickHouse parses
// "region.code" as a qualified identifier. A plain dots->underscores replacement is not
// injective ("region.code" and "region_code" both yield "region_code"), so the encoding
// below escapes underscores before mapping dots, making the transform reversible and thus
// collision-free: '_' -> "_5f" and '.' -> "_2e" (the chars' hex codes, which the escape of
// '_' guarantees cannot otherwise appear). Valid names only contain [A-Za-z0-9_.], so no
// other character needs escaping.
func groupByPropertyAlias(propertyName string) string {
	escaped := strings.ReplaceAll(propertyName, "_", "_5f")
	escaped = strings.ReplaceAll(escaped, ".", "_2e")
	return "prop_" + escaped
}

// validateGroupByProperty checks that a GroupByProperty value is safe to interpolate into SQL.
// It rejects any string that contains characters other than letters, digits, underscores, or dots.
func validateGroupByProperty(prop string) error {
	if prop == "" {
		return nil
	}
	if !validGroupByPropertyPattern.MatchString(prop) {
		return ierr.NewErrorf("invalid group_by property name: %q", prop).
			WithHint("GroupBy property name must contain only letters, digits, underscores, or dots").
			WithReportableDetails(map[string]interface{}{
				"group_by_property": prop,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
