package outbound

import "time"

// formatOptionalTime formats a *time.Time as RFC3339, or "" if nil.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02T15:04:05Z07:00")
}
