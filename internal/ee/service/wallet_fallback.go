package service

import (
	"context"
	"errors"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// isFallbackEligibleError reports whether err is the kind of failure that
// should trigger cached-balance fallback on the wallet-balance endpoint.
// Validation and not-found errors are surfaced to the caller unchanged.
func isFallbackEligibleError(err error) bool {
	if err == nil {
		return false
	}
	if ierr.IsValidation(err) || ierr.IsNotFound(err) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	return ierr.IsDatabase(err) || ierr.IsSystem(err) || ierr.IsInternal(err) || ierr.IsHTTPClient(err)
}

// classifyFallbackReason returns a low-cardinality label suitable for logging.
func classifyFallbackReason(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case ierr.IsDatabase(err):
		return "db_error"
	case ierr.IsSystem(err):
		return "system"
	case ierr.IsInternal(err):
		return "internal"
	case ierr.IsHTTPClient(err):
		return "http_client"
	default:
		return "other"
	}
}
