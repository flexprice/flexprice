package flexprice

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"time"

	sderr "github.com/flexprice/go-sdk/v2/models/errors"
)

// ---------------------------------------------------------------------------
// Error classification helpers
// ---------------------------------------------------------------------------

// IsNotFound reports whether err is an API error with HTTP 404.
func IsNotFound(err error) bool { return hasStatusCode(err, http.StatusNotFound) }

// IsValidation reports whether err is an API error with HTTP 400.
func IsValidation(err error) bool { return hasStatusCode(err, http.StatusBadRequest) }

// IsAlreadyExists reports whether err is an API error with HTTP 409.
func IsAlreadyExists(err error) bool { return hasStatusCode(err, http.StatusConflict) }

// IsPermissionDenied reports whether err is an API error with HTTP 403.
func IsPermissionDenied(err error) bool { return hasStatusCode(err, http.StatusForbidden) }

// IsRateLimit reports whether err is an API error with HTTP 429.
func IsRateLimit(err error) bool { return hasStatusCode(err, http.StatusTooManyRequests) }

// IsInvalidOperation reports whether err is an API error with HTTP 400.
func IsInvalidOperation(err error) bool { return hasStatusCode(err, http.StatusBadRequest) }

// IsVersionConflict reports whether err is an API error with HTTP 409.
func IsVersionConflict(err error) bool { return hasStatusCode(err, http.StatusConflict) }

// IsServerError reports whether err is an API error with HTTP 5xx.
func IsServerError(err error) bool {
	var e *sderr.APIError
	if errors.As(err, &e) {
		return e.StatusCode >= http.StatusInternalServerError
	}
	return false
}

// hasStatusCode checks if err is (or wraps) an APIError with the given HTTP status code.
func hasStatusCode(err error, code int) bool {
	var e *sderr.APIError
	return errors.As(err, &e) && e.StatusCode == code
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// CustomHelpers provides utility functions for the FlexPrice Go SDK
type CustomHelpers struct{}

// FormatCurrency formats currency amount with proper formatting
func (h *CustomHelpers) FormatCurrency(amount float64, currency string) string {
	if currency == "USD" {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

// GenerateID generates a unique ID with optional prefix
func (h *CustomHelpers) GenerateID(prefix string) string {
	if prefix == "" {
		prefix = "id"
	}
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	randomStr := generateRandomString(9)
	return fmt.Sprintf("%s_%d_%s", prefix, timestamp, randomStr)
}

// IsValidEmail validates email format
func (h *CustomHelpers) IsValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	return emailRegex.MatchString(email)
}

// FormatDate formats date to ISO string
func (h *CustomHelpers) FormatDate(t time.Time) string {
	return t.Format(time.RFC3339)
}

// generateRandomString generates a random string of specified length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// NewCustomHelpers creates a new instance of CustomHelpers
func NewCustomHelpers() *CustomHelpers {
	return &CustomHelpers{}
}
