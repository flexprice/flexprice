package e2eprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

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

// Errorf wraps a formatted message with attributes for the failure report; an
// SDK error in the chain adds status_code and error_body unless already set.
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

// enrichWithSDKError adds the status and body of an SDK error in the chain to
// attrs without overwriting what the caller set.
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

// Brief renders err for an alert: a Flexprice API error becomes "<status> <message>".
func Brief(err error) string {
	var eer *sdkerrors.ErrorResponse
	if errors.As(err, &eer) && eer != nil && eer.Message != nil {
		if eer.HTTPStatusCode != nil {
			return fmt.Sprintf("%d %s", *eer.HTTPStatusCode, *eer.Message)
		}
		return *eer.Message
	}
	var api *sdkerrors.APIError
	if errors.As(err, &api) && api != nil {
		return fmt.Sprintf("%d %s", api.StatusCode, strings.TrimSpace(api.Body))
	}
	return err.Error()
}

func setIfAbsent(m map[string]string, k, v string) {
	if _, ok := m[k]; !ok {
		m[k] = v
	}
}

// IsInfra reports whether err means the API could not answer at all: a 5xx
// (decodable or not), a timeout or a transport failure. Such failures feed one region-level
// incident instead of paging per scenario.
func IsInfra(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var api *sdkerrors.APIError
	if errors.As(err, &api) && api != nil {
		return api.StatusCode >= 500
	}
	var rve *sdkerrors.ResponseValidationError
	if errors.As(err, &rve) && rve != nil {
		return rve.StatusCode >= 500
	}
	var eer *sdkerrors.ErrorResponse
	if errors.As(err, &eer) && eer != nil {
		if eer.HTTPStatusCode != nil {
			return *eer.HTTPStatusCode >= 500
		}
		if eer.HTTPMeta.Response != nil {
			return eer.HTTPMeta.Response.StatusCode >= 500
		}
	}
	return false
}
