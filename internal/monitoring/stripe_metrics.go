package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
)

// StripeMetrics tracks metrics and health for Stripe integration
type StripeMetrics struct {
	logger *logger.Logger
	mu     sync.RWMutex

	// Sync metrics
	SyncAttempts       int64           `json:"sync_attempts"`
	SyncSuccesses      int64           `json:"sync_successes"`
	SyncFailures       int64           `json:"sync_failures"`
	LastSyncTime       time.Time       `json:"last_sync_time"`
	LastSyncDuration   time.Duration   `json:"last_sync_duration"`
	AverageSyncLatency time.Duration   `json:"average_sync_latency"`
	SyncLatencies      []time.Duration `json:"-"` // Keep last 100 for averaging

	// API call metrics
	APICallCount      int64            `json:"api_call_count"`
	APISuccessCount   int64            `json:"api_success_count"`
	APIErrorCount     int64            `json:"api_error_count"`
	APIRateLimitCount int64            `json:"api_rate_limit_count"`
	APITimeoutCount   int64            `json:"api_timeout_count"`
	LastAPICallTime   time.Time        `json:"last_api_call_time"`
	AverageAPILatency time.Duration    `json:"average_api_latency"`
	APILatencies      []time.Duration  `json:"-"` // Keep last 100 for averaging
	APIErrorsByType   map[string]int64 `json:"api_errors_by_type"`

	// Webhook metrics
	WebhookReceived       int64           `json:"webhook_received"`
	WebhookProcessed      int64           `json:"webhook_processed"`
	WebhookFailed         int64           `json:"webhook_failed"`
	WebhookLatencies      []time.Duration `json:"-"`
	AverageWebhookLatency time.Duration   `json:"average_webhook_latency"`

	// Batch processing metrics
	BatchesCreated       int64 `json:"batches_created"`
	BatchesProcessed     int64 `json:"batches_processed"`
	BatchesFailed        int64 `json:"batches_failed"`
	EventsProcessedTotal int64 `json:"events_processed_total"`
	EventsFailedTotal    int64 `json:"events_failed_total"`

	// Circuit breaker metrics
	CircuitBreakerStates map[string]*CircuitBreakerState `json:"circuit_breaker_states"`
}

// CircuitBreakerState tracks the state of a circuit breaker
type CircuitBreakerState struct {
	State        CircuitState `json:"state"`
	FailureCount int64        `json:"failure_count"`
	LastFailure  time.Time    `json:"last_failure"`
	LastSuccess  time.Time    `json:"last_success"`
	NextAttempt  time.Time    `json:"next_attempt"`
	OpenedAt     time.Time    `json:"opened_at,omitempty"`
}

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

// Circuit breaker configuration
type CircuitBreakerConfig struct {
	FailureThreshold int           `json:"failure_threshold"`
	OpenTimeout      time.Duration `json:"open_timeout"`
	HalfOpenTimeout  time.Duration `json:"half_open_timeout"`
}

var DefaultCircuitBreakerConfig = CircuitBreakerConfig{
	FailureThreshold: 5,
	OpenTimeout:      30 * time.Second,
	HalfOpenTimeout:  10 * time.Second,
}

// NewStripeMetrics creates a new metrics instance
func NewStripeMetrics(logger *logger.Logger) *StripeMetrics {
	return &StripeMetrics{
		logger:               logger,
		APIErrorsByType:      make(map[string]int64),
		CircuitBreakerStates: make(map[string]*CircuitBreakerState),
		SyncLatencies:        make([]time.Duration, 0, 100),
		APILatencies:         make([]time.Duration, 0, 100),
		WebhookLatencies:     make([]time.Duration, 0, 100),
	}
}

// RecordSyncAttempt records a sync attempt
func (m *StripeMetrics) RecordSyncAttempt() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SyncAttempts++
}

// RecordSyncSuccess records a successful sync
func (m *StripeMetrics) RecordSyncSuccess(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SyncSuccesses++
	m.LastSyncTime = time.Now()
	m.LastSyncDuration = duration

	// Update average latency
	m.SyncLatencies = append(m.SyncLatencies, duration)
	if len(m.SyncLatencies) > 100 {
		m.SyncLatencies = m.SyncLatencies[1:]
	}
	m.updateAverageSyncLatency()
}

// RecordSyncFailure records a failed sync
func (m *StripeMetrics) RecordSyncFailure(errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SyncFailures++
	m.logger.Errorw("Stripe sync failure recorded",
		"error_type", errorType,
		"total_failures", m.SyncFailures,
		"success_rate", m.calculateSyncSuccessRate(),
	)
}

// RecordAPICall records an API call
func (m *StripeMetrics) RecordAPICall() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.APICallCount++
	m.LastAPICallTime = time.Now()
}

// RecordAPISuccess records a successful API call
func (m *StripeMetrics) RecordAPISuccess(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.APISuccessCount++

	// Update average latency
	m.APILatencies = append(m.APILatencies, duration)
	if len(m.APILatencies) > 100 {
		m.APILatencies = m.APILatencies[1:]
	}
	m.updateAverageAPILatency()
}

// RecordAPIError records an API error
func (m *StripeMetrics) RecordAPIError(errorType string, isRateLimit, isTimeout bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.APIErrorCount++
	m.APIErrorsByType[errorType]++

	if isRateLimit {
		m.APIRateLimitCount++
	}
	if isTimeout {
		m.APITimeoutCount++
	}

	m.logger.Warnw("Stripe API error recorded",
		"error_type", errorType,
		"is_rate_limit", isRateLimit,
		"is_timeout", isTimeout,
		"total_errors", m.APIErrorCount,
		"error_rate", m.calculateAPIErrorRate(),
	)
}

// RecordWebhookReceived records a received webhook
func (m *StripeMetrics) RecordWebhookReceived() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.WebhookReceived++
}

// RecordWebhookProcessed records a successfully processed webhook
func (m *StripeMetrics) RecordWebhookProcessed(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.WebhookProcessed++

	// Update average latency
	m.WebhookLatencies = append(m.WebhookLatencies, duration)
	if len(m.WebhookLatencies) > 100 {
		m.WebhookLatencies = m.WebhookLatencies[1:]
	}
	m.updateAverageWebhookLatency()
}

// RecordWebhookFailed records a failed webhook processing
func (m *StripeMetrics) RecordWebhookFailed(errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.WebhookFailed++
	m.logger.Errorw("Stripe webhook processing failed",
		"error_type", errorType,
		"total_failures", m.WebhookFailed,
		"success_rate", m.calculateWebhookSuccessRate(),
	)
}

// RecordBatchCreated records a created batch
func (m *StripeMetrics) RecordBatchCreated(eventCount int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BatchesCreated++
	m.EventsProcessedTotal += eventCount
}

// RecordBatchProcessed records a successfully processed batch
func (m *StripeMetrics) RecordBatchProcessed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BatchesProcessed++
}

// RecordBatchFailed records a failed batch
func (m *StripeMetrics) RecordBatchFailed(eventCount int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BatchesFailed++
	m.EventsFailedTotal += eventCount
}

// Circuit Breaker Implementation

// CheckCircuitBreaker checks if the circuit breaker allows the operation
func (m *StripeMetrics) CheckCircuitBreaker(operation string, config CircuitBreakerConfig) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.CircuitBreakerStates[operation]
	if !exists {
		state = &CircuitBreakerState{
			State: CircuitClosed,
		}
		m.CircuitBreakerStates[operation] = state
	}

	now := time.Now()

	switch state.State {
	case CircuitClosed:
		return true

	case CircuitOpen:
		if now.After(state.NextAttempt) {
			state.State = CircuitHalfOpen
			state.NextAttempt = now.Add(config.HalfOpenTimeout)
			m.logger.Infow("Circuit breaker transitioning to half-open",
				"operation", operation,
				"next_attempt", state.NextAttempt,
			)
			return true
		}
		return false

	case CircuitHalfOpen:
		return true

	default:
		return false
	}
}

// RecordCircuitBreakerSuccess records a successful operation
func (m *StripeMetrics) RecordCircuitBreakerSuccess(operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.CircuitBreakerStates[operation]
	if !exists {
		return
	}

	state.LastSuccess = time.Now()
	state.FailureCount = 0

	if state.State == CircuitHalfOpen {
		state.State = CircuitClosed
		m.logger.Infow("Circuit breaker closed",
			"operation", operation,
		)
	}
}

// RecordCircuitBreakerFailure records a failed operation
func (m *StripeMetrics) RecordCircuitBreakerFailure(operation string, config CircuitBreakerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.CircuitBreakerStates[operation]
	if !exists {
		state = &CircuitBreakerState{
			State: CircuitClosed,
		}
		m.CircuitBreakerStates[operation] = state
	}

	state.LastFailure = time.Now()
	state.FailureCount++

	if state.FailureCount >= int64(config.FailureThreshold) {
		state.State = CircuitOpen
		state.OpenedAt = time.Now()
		state.NextAttempt = time.Now().Add(config.OpenTimeout)

		m.logger.Errorw("Circuit breaker opened",
			"operation", operation,
			"failure_count", state.FailureCount,
			"threshold", config.FailureThreshold,
			"next_attempt", state.NextAttempt,
		)
	}
}

// GetSnapshot returns a snapshot of current metrics
func (m *StripeMetrics) GetSnapshot() StripeMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a deep copy
	snapshot := *m
	snapshot.APIErrorsByType = make(map[string]int64)
	for k, v := range m.APIErrorsByType {
		snapshot.APIErrorsByType[k] = v
	}

	snapshot.CircuitBreakerStates = make(map[string]*CircuitBreakerState)
	for k, v := range m.CircuitBreakerStates {
		stateCopy := *v
		snapshot.CircuitBreakerStates[k] = &stateCopy
	}

	return snapshot
}

// GetHealthStatus returns the health status of Stripe integration
func (m *StripeMetrics) GetHealthStatus() HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := HealthStatus{
		Healthy: true,
		Issues:  make([]string, 0),
	}

	// Check sync success rate
	syncSuccessRate := m.calculateSyncSuccessRate()
	if syncSuccessRate < 0.95 && m.SyncAttempts > 0 {
		health.Healthy = false
		health.Issues = append(health.Issues, fmt.Sprintf("Sync success rate too low: %.2f%%", syncSuccessRate*100))
	}

	// Check API error rate
	apiErrorRate := m.calculateAPIErrorRate()
	if apiErrorRate > 0.1 && m.APICallCount > 0 {
		health.Healthy = false
		health.Issues = append(health.Issues, fmt.Sprintf("API error rate too high: %.2f%%", apiErrorRate*100))
	}

	// Check webhook success rate
	webhookSuccessRate := m.calculateWebhookSuccessRate()
	if webhookSuccessRate < 0.95 && m.WebhookReceived > 0 {
		health.Healthy = false
		health.Issues = append(health.Issues, fmt.Sprintf("Webhook success rate too low: %.2f%%", webhookSuccessRate*100))
	}

	// Check circuit breaker states
	for operation, state := range m.CircuitBreakerStates {
		if state.State == CircuitOpen {
			health.Healthy = false
			health.Issues = append(health.Issues, fmt.Sprintf("Circuit breaker open for %s", operation))
		}
	}

	// Check recent activity
	if !m.LastSyncTime.IsZero() && time.Since(m.LastSyncTime) > 2*time.Hour {
		health.Healthy = false
		health.Issues = append(health.Issues, "No recent sync activity")
	}

	return health
}

// HealthStatus represents the health of the Stripe integration
type HealthStatus struct {
	Healthy bool     `json:"healthy"`
	Issues  []string `json:"issues"`
}

// LogMetricsSummary logs a summary of current metrics
func (m *StripeMetrics) LogMetricsSummary() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	m.logger.Infow("Stripe metrics summary",
		"sync_attempts", m.SyncAttempts,
		"sync_success_rate", m.calculateSyncSuccessRate(),
		"api_calls", m.APICallCount,
		"api_error_rate", m.calculateAPIErrorRate(),
		"webhooks_received", m.WebhookReceived,
		"webhook_success_rate", m.calculateWebhookSuccessRate(),
		"batches_processed", m.BatchesProcessed,
		"events_processed", m.EventsProcessedTotal,
		"average_sync_latency", m.AverageSyncLatency,
		"average_api_latency", m.AverageAPILatency,
	)
}

// Helper methods for calculating rates and averages

func (m *StripeMetrics) calculateSyncSuccessRate() float64 {
	if m.SyncAttempts == 0 {
		return 1.0
	}
	return float64(m.SyncSuccesses) / float64(m.SyncAttempts)
}

func (m *StripeMetrics) calculateAPIErrorRate() float64 {
	if m.APICallCount == 0 {
		return 0.0
	}
	return float64(m.APIErrorCount) / float64(m.APICallCount)
}

func (m *StripeMetrics) calculateWebhookSuccessRate() float64 {
	if m.WebhookReceived == 0 {
		return 1.0
	}
	return float64(m.WebhookProcessed) / float64(m.WebhookReceived)
}

func (m *StripeMetrics) updateAverageSyncLatency() {
	if len(m.SyncLatencies) == 0 {
		return
	}

	var total time.Duration
	for _, latency := range m.SyncLatencies {
		total += latency
	}
	m.AverageSyncLatency = total / time.Duration(len(m.SyncLatencies))
}

func (m *StripeMetrics) updateAverageAPILatency() {
	if len(m.APILatencies) == 0 {
		return
	}

	var total time.Duration
	for _, latency := range m.APILatencies {
		total += latency
	}
	m.AverageAPILatency = total / time.Duration(len(m.APILatencies))
}

func (m *StripeMetrics) updateAverageWebhookLatency() {
	if len(m.WebhookLatencies) == 0 {
		return
	}

	var total time.Duration
	for _, latency := range m.WebhookLatencies {
		total += latency
	}
	m.AverageWebhookLatency = total / time.Duration(len(m.WebhookLatencies))
}

// StartMetricsReporting starts a goroutine that periodically logs metrics
func (m *StripeMetrics) StartMetricsReporting(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.LogMetricsSummary()
		}
	}
}
