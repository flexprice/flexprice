package config

import (
	"fmt"
	"time"
)

// StripeConfig represents global application-level Stripe integration configuration
type StripeConfig struct {
	// Global feature flag to enable/disable Stripe integration
	Enabled bool `mapstructure:"enabled" validate:"required"`

	// Webhook configuration
	WebhookSecret string `mapstructure:"webhook_secret" validate:"required_if=Enabled true"`

	// Default sync settings
	SyncGracePeriodMinutes int `mapstructure:"sync_grace_period_minutes" default:"5" validate:"min=1,max=30"`
	BatchSizeLimit         int `mapstructure:"batch_size_limit" default:"10000" validate:"min=100,max=100000"`

	// API timeout and retry settings
	APITimeoutSeconds int `mapstructure:"api_timeout_seconds" default:"30" validate:"min=5,max=300"`
	MaxRetries        int `mapstructure:"max_retries" default:"3" validate:"min=1,max=10"`

	// Rate limiting
	RateLimitPerTenantPerMinute int `mapstructure:"rate_limit_per_tenant_per_minute" default:"100" validate:"min=10,max=1000"`

	// Circuit breaker settings
	CircuitBreakerConfig CircuitBreakerConfig `mapstructure:"circuit_breaker"`

	// Default aggregation window
	DefaultAggregationWindowMinutes int `mapstructure:"default_aggregation_window_minutes" default:"60" validate:"min=5,max=1440"`

	// Performance tuning
	ConcurrentSyncLimit int `mapstructure:"concurrent_sync_limit" default:"10" validate:"min=1,max=100"`

	// Data retention
	SyncBatchRetentionDays int `mapstructure:"sync_batch_retention_days" default:"30" validate:"min=1,max=365"`
}

// CircuitBreakerConfig represents circuit breaker configuration for Stripe API calls
type CircuitBreakerConfig struct {
	// Enable circuit breaker functionality
	Enabled bool `mapstructure:"enabled" default:"true"`

	// Number of consecutive failures before opening circuit
	FailureThreshold int `mapstructure:"failure_threshold" default:"5" validate:"min=1,max=20"`

	// Duration to wait before attempting to close circuit
	RecoveryTimeoutSeconds int `mapstructure:"recovery_timeout_seconds" default:"60" validate:"min=10,max=600"`

	// Minimum number of requests needed before circuit can open
	MinRequestThreshold int `mapstructure:"min_request_threshold" default:"10" validate:"min=1,max=100"`
}

// StripeValidationRules contains validation patterns for Stripe-specific data
type StripeValidationRules struct {
	// Stripe API key patterns
	APIKeyPattern string `json:"api_key_pattern"`

	// Webhook URL patterns
	WebhookURLPattern string `json:"webhook_url_pattern"`

	// Customer ID patterns
	CustomerIDPattern string `json:"customer_id_pattern"`

	// Meter ID patterns
	MeterIDPattern string `json:"meter_id_pattern"`
}

// GetDefaultStripeValidationRules returns default validation rules for Stripe data
func GetDefaultStripeValidationRules() StripeValidationRules {
	return StripeValidationRules{
		// Stripe API keys start with sk_test_ or sk_live_ followed by 24+ characters
		APIKeyPattern: `^sk_(test|live)_[A-Za-z0-9]{24,}$`,

		// Webhook URLs must be HTTPS
		WebhookURLPattern: `^https://[a-zA-Z0-9\-\.]+\.[a-zA-Z]{2,}(/.*)?$`,

		// Stripe customer IDs start with cus_ followed by 14+ characters
		CustomerIDPattern: `^cus_[A-Za-z0-9]{14,}$`,

		// Stripe meter IDs start with mtr_ followed by 14+ characters
		MeterIDPattern: `^mtr_[A-Za-z0-9]{14,}$`,
	}
}

// GetAPITimeout returns the API timeout as a time.Duration
func (s StripeConfig) GetAPITimeout() time.Duration {
	return time.Duration(s.APITimeoutSeconds) * time.Second
}

// GetSyncGracePeriod returns the sync grace period as a time.Duration
func (s StripeConfig) GetSyncGracePeriod() time.Duration {
	return time.Duration(s.SyncGracePeriodMinutes) * time.Minute
}

// GetCircuitBreakerRecoveryTimeout returns the circuit breaker recovery timeout as a time.Duration
func (s StripeConfig) GetCircuitBreakerRecoveryTimeout() time.Duration {
	return time.Duration(s.CircuitBreakerConfig.RecoveryTimeoutSeconds) * time.Second
}

// GetDefaultAggregationWindow returns the default aggregation window as a time.Duration
func (s StripeConfig) GetDefaultAggregationWindow() time.Duration {
	return time.Duration(s.DefaultAggregationWindowMinutes) * time.Minute
}

// GetSyncBatchRetentionDuration returns the sync batch retention period as a time.Duration
func (s StripeConfig) GetSyncBatchRetentionDuration() time.Duration {
	return time.Duration(s.SyncBatchRetentionDays) * 24 * time.Hour
}

// ValidateStripeConfig validates the Stripe configuration
func (s StripeConfig) Validate() error {
	if s.Enabled {
		if s.WebhookSecret == "" {
			return fmt.Errorf("webhook_secret is required when Stripe integration is enabled")
		}

		if s.SyncGracePeriodMinutes < 1 || s.SyncGracePeriodMinutes > 30 {
			return fmt.Errorf("sync_grace_period_minutes must be between 1 and 30")
		}

		if s.BatchSizeLimit < 100 || s.BatchSizeLimit > 100000 {
			return fmt.Errorf("batch_size_limit must be between 100 and 100000")
		}

		if s.APITimeoutSeconds < 5 || s.APITimeoutSeconds > 300 {
			return fmt.Errorf("api_timeout_seconds must be between 5 and 300")
		}

		if s.MaxRetries < 1 || s.MaxRetries > 10 {
			return fmt.Errorf("max_retries must be between 1 and 10")
		}

		if s.RateLimitPerTenantPerMinute < 10 || s.RateLimitPerTenantPerMinute > 1000 {
			return fmt.Errorf("rate_limit_per_tenant_per_minute must be between 10 and 1000")
		}

		if s.DefaultAggregationWindowMinutes < 5 || s.DefaultAggregationWindowMinutes > 1440 {
			return fmt.Errorf("default_aggregation_window_minutes must be between 5 and 1440")
		}

		if s.ConcurrentSyncLimit < 1 || s.ConcurrentSyncLimit > 100 {
			return fmt.Errorf("concurrent_sync_limit must be between 1 and 100")
		}

		if s.SyncBatchRetentionDays < 1 || s.SyncBatchRetentionDays > 365 {
			return fmt.Errorf("sync_batch_retention_days must be between 1 and 365")
		}

		// Validate circuit breaker config
		if s.CircuitBreakerConfig.Enabled {
			if s.CircuitBreakerConfig.FailureThreshold < 1 || s.CircuitBreakerConfig.FailureThreshold > 20 {
				return fmt.Errorf("circuit_breaker.failure_threshold must be between 1 and 20")
			}

			if s.CircuitBreakerConfig.RecoveryTimeoutSeconds < 10 || s.CircuitBreakerConfig.RecoveryTimeoutSeconds > 600 {
				return fmt.Errorf("circuit_breaker.recovery_timeout_seconds must be between 10 and 600")
			}

			if s.CircuitBreakerConfig.MinRequestThreshold < 1 || s.CircuitBreakerConfig.MinRequestThreshold > 100 {
				return fmt.Errorf("circuit_breaker.min_request_threshold must be between 1 and 100")
			}
		}
	}

	return nil
}

// GetDefaultStripeConfig returns a default Stripe configuration
func GetDefaultStripeConfig() StripeConfig {
	return StripeConfig{
		Enabled:                         false,
		WebhookSecret:                   "",
		SyncGracePeriodMinutes:          5,
		BatchSizeLimit:                  10000,
		APITimeoutSeconds:               30,
		MaxRetries:                      3,
		RateLimitPerTenantPerMinute:     100,
		DefaultAggregationWindowMinutes: 60,
		ConcurrentSyncLimit:             10,
		SyncBatchRetentionDays:          30,
		CircuitBreakerConfig: CircuitBreakerConfig{
			Enabled:                true,
			FailureThreshold:       5,
			RecoveryTimeoutSeconds: 60,
			MinRequestThreshold:    10,
		},
	}
}
