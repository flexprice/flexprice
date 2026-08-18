package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/flexprice/cli/internal/exitcode"
)

// Three error shapes exist in practice, verified against the live API:
//
//	{"code":"not_found","message":"...","http_status_code":404,"details":{...}}
//	{"error":"Unauthorized"}   // auth middleware, a bare string
//	<non-JSON>                 // gateways and proxies
//
// details is json.RawMessage rather than a typed map so a bad shape there
// cannot take code and message down with it; it is decoded separately.
type envelope struct {
	Code           string          `json:"code"`
	Message        string          `json:"message"`
	HTTPStatusCode int             `json:"http_status_code"`
	Details        json.RawMessage `json:"details"`
	Error          json.RawMessage `json:"error"`
}

type APIError struct {
	Status  int
	Method  string
	Path    string
	Message string
	Code    string
	Details map[string]any
	Raw     []byte
}

var _ error = (*APIError)(nil)

func NewAPIError(status int, body []byte, method, path string) *APIError {
	e := &APIError{Status: status, Method: method, Path: path, Raw: body}

	// A type mismatch in one field does not stop the decoder populating
	// siblings it already matched, so these are copied regardless of err.
	var env envelope
	_ = json.Unmarshal(body, &env)
	e.Message = env.Message
	e.Code = env.Code
	if len(env.Details) > 0 {
		var details map[string]any
		if json.Unmarshal(env.Details, &details) == nil {
			e.Details = details
		}
	}

	// The auth middleware returns a string, not an object.
	if e.Message == "" && len(env.Error) > 0 {
		var bare string
		if json.Unmarshal(env.Error, &bare) == nil {
			e.Message = bare
		}
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	if e.Message == "" {
		e.Message = fmt.Sprintf("HTTP %d", status)
	}
	return e
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s failed (HTTP %d)\n  %s", e.Method, e.Path, e.Status, e.Message)

	// Sorted so the output is deterministic.
	if len(e.Details) > 0 {
		keys := make([]string, 0, len(e.Details))
		for k := range e.Details {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "\n    %s: %v", k, e.Details[k])
		}
	}
	if next := e.nextStep(); next != "" {
		fmt.Fprintf(&b, "\n\n  %s", next)
	}
	return b.String()
}

func (e *APIError) nextStep() string {
	switch e.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "Your key may be invalid or from a different region. Check: flexprice whoami"
	case http.StatusTooManyRequests:
		return "Rate limited. Retry in a moment, or lower --interval if you are tailing."
	case http.StatusNotFound:
		return "If you expected this endpoint to exist, your CLI may predate it: brew upgrade flexprice"
	}
	return ""
}

func (e *APIError) ExitCode() int {
	switch {
	case e.Status == http.StatusUnauthorized, e.Status == http.StatusForbidden:
		return exitcode.Auth
	case e.Status == http.StatusNotFound:
		return exitcode.NotFound
	case e.Status == http.StatusTooManyRequests:
		return exitcode.RateLimited
	case e.Status >= 400 && e.Status < 500:
		return exitcode.Usage
	default:
		return exitcode.Generic
	}
}
