package clickhouse

import (
	"regexp"
	"strings"
)

var propertyPathPattern = regexp.MustCompile(`^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*$`)

func parseGroupByPropertyPath(groupBy string) (string, bool) {
	if !strings.HasPrefix(groupBy, "properties.") {
		return "", false
	}
	path := strings.TrimPrefix(groupBy, "properties.")
	if !propertyPathPattern.MatchString(path) {
		return "", false
	}
	return path, true
}

func propertyAlias(path string) string {
	if !propertyPathPattern.MatchString(path) {
		return "prop_invalid"
	}
	return "prop_" + strings.ReplaceAll(path, ".", "_")
}
