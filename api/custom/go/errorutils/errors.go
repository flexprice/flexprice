// Package errorutils provides helper functions for inspecting Flexprice SDK errors.
// These functions check the HTTP status code of an *errors.APIError,
// using errors.As so wrapped errors are still classified correctly.
//
// Usage:
//
//	_, err := client.Customers.CreateCustomer(ctx, req)
//	if errorutils.IsConflict(err) {
//	    // handle duplicate customer
//	}
package errorutils

import (
	"errors"
	"net/http"

	sderr "github.com/flexprice/go-sdk/v2/models/errors"
)

// IsNotFound reports whether err is an API error with HTTP 404.
func IsNotFound(err error) bool {
	return hasStatusCode(err, http.StatusNotFound)
}

// IsValidation reports whether err is an API error with HTTP 400.
func IsValidation(err error) bool {
	return hasStatusCode(err, http.StatusBadRequest)
}

// IsConflict reports whether err is an API error with HTTP 409.
func IsConflict(err error) bool {
	return hasStatusCode(err, http.StatusConflict)
}

// IsRateLimit reports whether err is an API error with HTTP 429.
func IsRateLimit(err error) bool {
	return hasStatusCode(err, http.StatusTooManyRequests)
}

// IsPermissionDenied reports whether err is an API error with HTTP 403.
func IsPermissionDenied(err error) bool {
	return hasStatusCode(err, http.StatusForbidden)
}

// IsServerError reports whether err is an API error with HTTP 5xx.
func IsServerError(err error) bool {
	var e *sderr.APIError
	if errors.As(err, &e) {
		return e.StatusCode >= http.StatusInternalServerError && e.StatusCode < 600
	}
	return false
}

// hasStatusCode checks if err is (or wraps) an APIError with the given HTTP status code.
func hasStatusCode(err error, code int) bool {
	var e *sderr.APIError
	return errors.As(err, &e) && e.StatusCode == code
}
