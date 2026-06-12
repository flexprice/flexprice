package e2eprobe

import (
	"errors"
	"fmt"
)

// CheckError wraps an error with structured attributes that will appear in the
// failure report sent to Slack/OTEL/log. Checks should use Errorf to construct
// these so that the Runner can extract them automatically.
type CheckError struct {
	Err        error
	Attributes map[string]string
}

func (e *CheckError) Error() string { return e.Err.Error() }
func (e *CheckError) Unwrap() error { return e.Err }

// Errorf is a convenience constructor that wraps a fmt.Errorf-style message
// with structured key/value attributes for the failure report.
func Errorf(attrs map[string]string, format string, args ...any) error {
	return &CheckError{
		Err:        fmt.Errorf(format, args...),
		Attributes: attrs,
	}
}

// AttributesFrom extracts attributes from an error chain. Returns nil if no
// CheckError is present in the chain.
func AttributesFrom(err error) map[string]string {
	var ce *CheckError
	if errors.As(err, &ce) {
		return ce.Attributes
	}
	return nil
}
