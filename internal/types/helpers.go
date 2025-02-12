package types

import "time"

// ToNillableString returns a pointer to the string if not empty, nil otherwise
func ToNillableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ToNillableTime returns a pointer to the time if not zero, nil otherwise
func ToNillableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// FromNillableString returns the string value or empty string if nil
func FromNillableString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// FromNillableTime returns the time value or zero time if nil
func FromNillableTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
