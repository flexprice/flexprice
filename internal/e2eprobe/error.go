package e2eprobe

import (
	"errors"
	"fmt"
	"strconv"

	sdkerrors "github.com/flexprice/go-sdk/v2/models/errors"
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
//
// If the wrapped error chain contains a Flexprice SDK error type, Errorf
// auto-populates `status_code` and `error_body` attributes. This makes
// otherwise-opaque "%s: {}" alerts (where the SDK's ErrorResponse.Error()
// marshals to the literal string "{}") actionable: callers don't have to
// reach into the SDK type at every alert site.
//
// Caller-provided attributes win on key conflicts — Errorf only fills in
// `status_code` / `error_body` when they're absent.
func Errorf(attrs map[string]string, format string, args ...any) error {
	if attrs == nil {
		attrs = map[string]string{}
	}
	err := fmt.Errorf(format, args...)
	enrichWithSDKError(err, attrs)
	return &CheckError{
		Err:        err,
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

// enrichWithSDKError walks the error chain for known Flexprice SDK error
// types and mutates attrs to include their structured detail. Recognized
// types:
//
//   - *sdkerrors.APIError — generic SDK API errors. Has explicit Body and
//     StatusCode fields.
//   - *sdkerrors.ErrorResponse — typed error response. Status code
//     lives on HTTPMeta.Response; the body marshals via json.Marshal of the
//     struct (often "{}" when the server returns an unparseable response).
//
// Caller-provided keys are never overwritten — this is enrichment, not
// replacement.
func enrichWithSDKError(err error, attrs map[string]string) {
	var api *sdkerrors.APIError
	if errors.As(err, &api) && api != nil {
		setIfAbsent(attrs, "status_code", strconv.Itoa(api.StatusCode))
		if api.Body != "" {
			setIfAbsent(attrs, "error_body", api.Body)
		}
		return
	}

	var eer *sdkerrors.ErrorResponse
	if errors.As(err, &eer) && eer != nil {
		if eer.HTTPMeta.Response != nil {
			setIfAbsent(attrs, "status_code", strconv.Itoa(eer.HTTPMeta.Response.StatusCode))
		}
		// Use eer.Error() (the SDK's own marshaling), not err.Error() (the
		// fully-wrapped chain). For ErrorResponse with no populated
		// fields this returns the bare "{}" we've been chasing — recording
		// that explicitly is the whole point.
		setIfAbsent(attrs, "error_body", eer.Error())
		return
	}
}

func setIfAbsent(m map[string]string, k, v string) {
	if _, ok := m[k]; !ok {
		m[k] = v
	}
}
