package storage

import (
	"fmt"
	"path"
	"strings"
)

// IsCleanKey reports whether key has no empty, "." or ".." segments and no leading slash.
func IsCleanKey(key string) bool {
	trimmed := strings.TrimSuffix(key, "/")
	if strings.HasPrefix(trimmed, "/") || path.Clean(trimmed) != trimmed {
		return false
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// ObjectKey builds a backend-agnostic object key of the form
// {prefix}/{entityType}/{filename}.{format}[.gz]. Empty prefix segments
// are dropped so callers don't need to special-case an empty KeyPrefix.
func ObjectKey(prefix, entityType, filename, format string, compressed bool) string {
	ext := format
	if compressed {
		ext = ext + ".gz"
	}

	parts := make([]string, 0, 3)
	if p := strings.Trim(prefix, "/"); p != "" {
		parts = append(parts, p)
	}
	if entityType != "" {
		parts = append(parts, entityType)
	}
	parts = append(parts, fmt.Sprintf("%s.%s", filename, ext))

	return strings.Join(parts, "/")
}
