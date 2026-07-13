package storage

import (
	"fmt"
	"strings"
)

// ObjectKey builds a backend-agnostic object key of the form
// {prefix}/{entityType}/{filename}.{format}[.gz]. Empty prefix segments
// are dropped so callers don't need to special-case an empty KeyPrefix.
func ObjectKey(prefix, entityType, filename, format string, compressed bool) string {
	ext := format
	if compressed {
		ext = ext + ".gz"
	}

	parts := make([]string, 0, 3)
	if prefix != "" {
		parts = append(parts, strings.Trim(prefix, "/"))
	}
	if entityType != "" {
		parts = append(parts, entityType)
	}
	parts = append(parts, fmt.Sprintf("%s.%s", filename, ext))

	return strings.Join(parts, "/")
}
