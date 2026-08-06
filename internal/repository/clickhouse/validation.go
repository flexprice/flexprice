package clickhouse

import (
	"regexp"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// validGroupByPropertyPattern matches safe property names (alphanumeric, underscores, dots).
var validGroupByPropertyPattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

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
