package types

import "time"

func ParseTime(t string) (time.Time, error) {
	return time.Parse(time.RFC3339, t)
}

func FormatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}
