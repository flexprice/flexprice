package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/flexprice/cli/internal/exitcode"
)

// envelope matches the API's error responses. Three shapes exist in practice,
// verified against the live API:
//
//	{"code":"not_found","message":"...","http_status_code":404,"details":{"customer_id":"..."}}
//	{"error":"Unauthorized"}                      // auth middleware, a bare string
//	<non-JSON>                                    // gateways and proxies
//
// details is field-keyed and is the most actionable part of a validation
// failure, so it is rendered rather than discarded. Its values are typed as any
// rather than string: a single non-string value would otherwise fail the whole
// envelope decode and silently take message and code down with it.
type envelope struct {
	Code           string          `json:"code"`
	Message        string          `json:"message"`
	HTTPStatusCode int             `json:"http_status_code"`
	Details        map[string]any  `json:"details"`
	Error          json.RawMessage `json:"error"`
}

// APIError renders as what failed, why, and what to do next.
type APIError struct {
	Status  int
	Method  string
	Path    string
	Message string
	Code    string
	Details map[string]any
	Raw     []byte
}

// compile-time assertion that *APIError satisfies the error interface.
var _ error = (*APIError)(nil)

func NewAPIError(status int, body []byte, method, path string) *APIError {
	e := &APIError{Status: status, Method: method, Path: path, Raw: body}

	var env envelope
	if err := json.Unmarshal(body, &env); err == nil {
		e.Message = env.Message
		e.Code = env.Code
		e.Details = env.Details

		// The auth middleware returns {"error":"Unauthorized"} — a string, not an
		// object — so it is decoded separately rather than into the main shape.
		if e.Message == "" && len(env.Error) > 0 {
			var bare string
			if json.Unmarshal(env.Error, &bare) == nil {
				e.Message = bare
			}
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

	// details names the offending fields; sorted so the output is deterministic.
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

// nextStep turns a status into a concrete action the user can take.
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
