package errors

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cockroachdb/errors"
)

// Stripe error codes
const (
	ErrCodeStripeAPI             = "stripe_api_error"
	ErrCodeStripeAuthentication  = "stripe_authentication_error"
	ErrCodeStripeConfiguration   = "stripe_configuration_error"
	ErrCodeStripeRateLimit       = "stripe_rate_limit_error"
	ErrCodeStripeNetwork         = "stripe_network_error"
	ErrCodeStripeWebhook         = "stripe_webhook_error"
	ErrCodeStripeSync            = "stripe_sync_error"
	ErrCodeStripeMigration       = "stripe_migration_error"
	ErrCodeStripeCustomerMapping = "stripe_customer_mapping_error"
	ErrCodeStripeMeterMapping    = "stripe_meter_mapping_error"
	ErrCodeStripeTimeout         = "stripe_timeout_error"
	ErrCodeStripeCircuitBreaker  = "stripe_circuit_breaker_error"
)

// Stripe error instances
var (
	ErrStripeAPI             = new(ErrCodeStripeAPI, "Stripe API error")
	ErrStripeAuthentication  = new(ErrCodeStripeAuthentication, "Stripe authentication error")
	ErrStripeConfiguration   = new(ErrCodeStripeConfiguration, "Stripe configuration error")
	ErrStripeRateLimit       = new(ErrCodeStripeRateLimit, "Stripe rate limit error")
	ErrStripeNetwork         = new(ErrCodeStripeNetwork, "Stripe network error")
	ErrStripeWebhook         = new(ErrCodeStripeWebhook, "Stripe webhook error")
	ErrStripeSync            = new(ErrCodeStripeSync, "Stripe sync error")
	ErrStripeMigration       = new(ErrCodeStripeMigration, "Stripe migration error")
	ErrStripeCustomerMapping = new(ErrCodeStripeCustomerMapping, "Stripe customer mapping error")
	ErrStripeMeterMapping    = new(ErrCodeStripeMeterMapping, "Stripe meter mapping error")
	ErrStripeTimeout         = new(ErrCodeStripeTimeout, "Stripe timeout error")
	ErrStripeCircuitBreaker  = new(ErrCodeStripeCircuitBreaker, "Stripe circuit breaker open")
)

// HTTP status mapping for Stripe errors
var stripeStatusCodeMap = map[error]int{
	ErrStripeAPI:             http.StatusInternalServerError,
	ErrStripeAuthentication:  http.StatusUnauthorized,
	ErrStripeConfiguration:   http.StatusInternalServerError,
	ErrStripeRateLimit:       http.StatusTooManyRequests,
	ErrStripeNetwork:         http.StatusInternalServerError,
	ErrStripeWebhook:         http.StatusBadRequest,
	ErrStripeSync:            http.StatusInternalServerError,
	ErrStripeMigration:       http.StatusInternalServerError,
	ErrStripeCustomerMapping: http.StatusBadRequest,
	ErrStripeMeterMapping:    http.StatusBadRequest,
	ErrStripeTimeout:         http.StatusGatewayTimeout,
	ErrStripeCircuitBreaker:  http.StatusServiceUnavailable,
}

// StripeError provides detailed Stripe error information
type StripeError struct {
	*InternalError
	StripeCode    string                 `json:"stripe_code,omitempty"`
	StripeMessage string                 `json:"stripe_message,omitempty"`
	RequestID     string                 `json:"request_id,omitempty"`
	StatusCode    int                    `json:"status_code,omitempty"`
	Retryable     bool                   `json:"retryable"`
	RetryAfter    *time.Duration         `json:"retry_after,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

func (e *StripeError) Error() string {
	if e.StripeMessage != "" {
		return fmt.Sprintf("%s: %s (stripe: %s)", e.Code, e.StripeMessage, e.StripeCode)
	}
	return e.InternalError.Error()
}

// IsRetryable determines if the error should be retried
func (e *StripeError) IsRetryable() bool {
	return e.Retryable
}

// GetRetryAfter returns the retry delay, if any
func (e *StripeError) GetRetryAfter() *time.Duration {
	return e.RetryAfter
}

// NewStripeError creates a new Stripe error
func NewStripeError(base *InternalError, stripeCode, stripeMessage, requestID string, statusCode int) *StripeError {
	retryable := isRetryableStripeError(base, statusCode)

	return &StripeError{
		InternalError: base,
		StripeCode:    stripeCode,
		StripeMessage: stripeMessage,
		RequestID:     requestID,
		StatusCode:    statusCode,
		Retryable:     retryable,
		Metadata:      make(map[string]interface{}),
	}
}

// NewStripeRateLimitError creates a rate limit error with retry after
func NewStripeRateLimitError(retryAfter time.Duration, requestID string) *StripeError {
	return &StripeError{
		InternalError: &InternalError{
			Code:    ErrCodeStripeRateLimit,
			Message: "Stripe rate limit exceeded",
		},
		StripeCode: "rate_limit_error",
		RequestID:  requestID,
		StatusCode: 429,
		Retryable:  true,
		RetryAfter: &retryAfter,
		Metadata:   make(map[string]interface{}),
	}
}

// NewStripeTimeoutError creates a timeout error
func NewStripeTimeoutError(operation string, timeout time.Duration) *StripeError {
	return &StripeError{
		InternalError: &InternalError{
			Code:    ErrCodeStripeTimeout,
			Message: fmt.Sprintf("Stripe operation '%s' timed out after %v", operation, timeout),
			Op:      operation,
		},
		StripeCode: "timeout_error",
		StatusCode: 408,
		Retryable:  true,
		Metadata: map[string]interface{}{
			"operation": operation,
			"timeout":   timeout.String(),
		},
	}
}

// NewStripeCircuitBreakerError creates a circuit breaker error
func NewStripeCircuitBreakerError(operation string) *StripeError {
	return &StripeError{
		InternalError: &InternalError{
			Code:    ErrCodeStripeCircuitBreaker,
			Message: fmt.Sprintf("Stripe circuit breaker is open for operation '%s'", operation),
			Op:      operation,
		},
		StripeCode: "circuit_breaker_open",
		StatusCode: 503,
		Retryable:  false, // Don't retry when circuit breaker is open
		Metadata: map[string]interface{}{
			"operation": operation,
		},
	}
}

// Retry strategy configuration
type RetryStrategy struct {
	MaxRetries      int
	BaseDelay       time.Duration
	MaxDelay        time.Duration
	ExponentialBase float64
	Jitter          bool
}

// Default retry strategies for different error types
var (
	// Rate limit errors: 5 retries with exponential backoff
	RateLimitRetryStrategy = RetryStrategy{
		MaxRetries:      5,
		BaseDelay:       1 * time.Second,
		MaxDelay:        32 * time.Second,
		ExponentialBase: 2.0,
		Jitter:          true,
	}

	// Network errors: 3 retries with linear backoff
	NetworkRetryStrategy = RetryStrategy{
		MaxRetries:      3,
		BaseDelay:       500 * time.Millisecond,
		MaxDelay:        5 * time.Second,
		ExponentialBase: 1.5,
		Jitter:          true,
	}

	// API errors: 3 retries with exponential backoff
	APIRetryStrategy = RetryStrategy{
		MaxRetries:      3,
		BaseDelay:       1 * time.Second,
		MaxDelay:        8 * time.Second,
		ExponentialBase: 2.0,
		Jitter:          false,
	}

	// No retry for authentication and configuration errors
	NoRetryStrategy = RetryStrategy{
		MaxRetries: 0,
	}
)

// GetRetryStrategy returns the appropriate retry strategy for an error
func GetRetryStrategy(err error) RetryStrategy {
	if stripeErr, ok := err.(*StripeError); ok {
		if !stripeErr.Retryable {
			return NoRetryStrategy
		}

		switch stripeErr.Code {
		case ErrCodeStripeRateLimit:
			return RateLimitRetryStrategy
		case ErrCodeStripeNetwork, ErrCodeStripeTimeout:
			return NetworkRetryStrategy
		case ErrCodeStripeAPI:
			return APIRetryStrategy
		case ErrCodeStripeAuthentication, ErrCodeStripeConfiguration:
			return NoRetryStrategy
		default:
			return APIRetryStrategy
		}
	}

	// For non-Stripe errors, check base error types
	if IsHTTPClient(err) || IsSystem(err) {
		return NetworkRetryStrategy
	}

	return NoRetryStrategy
}

// CalculateDelay calculates the delay for a retry attempt
func (rs RetryStrategy) CalculateDelay(attempt int) time.Duration {
	if attempt >= rs.MaxRetries {
		return 0
	}

	delay := rs.BaseDelay
	if rs.ExponentialBase > 1.0 {
		for i := 0; i < attempt; i++ {
			delay = time.Duration(float64(delay) * rs.ExponentialBase)
		}
	}

	if delay > rs.MaxDelay {
		delay = rs.MaxDelay
	}

	if rs.Jitter {
		// Add ±25% jitter
		jitterRange := delay / 4
		jitter := time.Duration(float64(jitterRange) * (2.0*float64(time.Now().UnixNano()%1000)/1000.0 - 1.0))
		delay += jitter
	}

	return delay
}

// Error type checking functions
func IsStripeError(err error) bool {
	_, ok := err.(*StripeError)
	return ok
}

func IsStripeRateLimit(err error) bool {
	return errors.Is(err, ErrStripeRateLimit)
}

func IsStripeAuthentication(err error) bool {
	return errors.Is(err, ErrStripeAuthentication)
}

func IsStripeConfiguration(err error) bool {
	return errors.Is(err, ErrStripeConfiguration)
}

func IsStripeNetwork(err error) bool {
	return errors.Is(err, ErrStripeNetwork)
}

func IsStripeTimeout(err error) bool {
	return errors.Is(err, ErrStripeTimeout)
}

func IsStripeCircuitBreaker(err error) bool {
	return errors.Is(err, ErrStripeCircuitBreaker)
}

// HTTPStatusFromStripeErr returns HTTP status code for Stripe errors
func HTTPStatusFromStripeErr(err error) int {
	if stripeErr, ok := err.(*StripeError); ok && stripeErr.StatusCode > 0 {
		return stripeErr.StatusCode
	}

	for e, status := range stripeStatusCodeMap {
		if errors.Is(err, e) {
			return status
		}
	}

	return http.StatusInternalServerError
}

// isRetryableStripeError determines if a Stripe error should be retried
func isRetryableStripeError(base *InternalError, statusCode int) bool {
	switch base.Code {
	case ErrCodeStripeAuthentication, ErrCodeStripeConfiguration:
		return false // Never retry auth/config errors
	case ErrCodeStripeRateLimit:
		return true // Always retry rate limits
	case ErrCodeStripeCircuitBreaker:
		return false // Don't retry when circuit breaker is open
	default:
		// Retry based on HTTP status code
		return statusCode >= 500 || statusCode == 429 || statusCode == 408
	}
}
