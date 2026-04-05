// Package errorutils provides helper functions for inspecting Flexprice SDK errors.
// These functions check the HTTP status code of an *errors.APIError.
//
// Usage:
//
//	_, err := client.Customers.CreateCustomer(ctx, req)
//	if errorutils.IsConflict(err) {
//	    // handle duplicate customer
//	}
package errorutils

import (
	"net/http"

	sderr "github.com/flexprice/go-sdk/v2/models/errors"
)

// IsNotFound reports whether err is an API error with HTTP 404.
func IsNotFound(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusNotFound
}

// IsValidation reports whether err is an API error with HTTP 400.
func IsValidation(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusBadRequest
}

// IsConflict reports whether err is an API error with HTTP 409.
func IsConflict(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusConflict
}

// IsRateLimit reports whether err is an API error with HTTP 429.
func IsRateLimit(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusTooManyRequests
}

// IsPermissionDenied reports whether err is an API error with HTTP 403.
func IsPermissionDenied(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode == http.StatusForbidden
}

// IsServerError reports whether err is an API error with HTTP 5xx.
func IsServerError(err error) bool {
	e, ok := err.(*sderr.APIError)
	return ok && e.StatusCode >= http.StatusInternalServerError
}
