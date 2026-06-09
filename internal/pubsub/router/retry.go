package router

import (
	"context"
	"net"
	"net/http"

	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/logger"
)

func shouldRetry(logger *logger.Logger, err error) bool {
	// HTTP errors
	if httpErr, ok := httpclient.IsHTTPError(err); ok {
		switch httpErr.StatusCode {
		case http.StatusTooManyRequests,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			logger.Debug(context.Background(), "retrying due to HTTP error",
				"status_code", httpErr.StatusCode,
				"error", httpErr,
			)
			return true
		}
		logger.Debug(context.Background(), "non-retryable HTTP error",
			"status_code", httpErr.StatusCode,
			"error", httpErr,
		)
		return false
	}

	// Network errors
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		logger.Debug(context.Background(), "retrying due to network timeout", "error", netErr)
		return true
	}

	// Business logic errors (don't retry)
	if errors.IsValidation(err) ||
		errors.IsNotFound(err) ||
		errors.IsPermissionDenied(err) {
		return false
	}

	// By default, retry unknown errors
	return true
}
