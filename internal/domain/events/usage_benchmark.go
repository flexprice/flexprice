package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

// UsageBenchmarkKind discriminates between benchmark event variants.
type UsageBenchmarkKind string

const (
	// UsageBenchmarkKindSubscription is the original subscription pipeline benchmark.
	// Empty Kind is also treated as subscription for backwards compat with in-flight events.
	UsageBenchmarkKindSubscription UsageBenchmarkKind = "subscription"
	// UsageBenchmarkKindAnalytics compares the feature-usage vs meter-usage analytics pipelines.
	UsageBenchmarkKindAnalytics UsageBenchmarkKind = "analytics"
)

// UsageBenchmarkEvent is the thin Kafka payload published for benchmarking.
// Subscription and analytics variants share the topic; consumer dispatches on Kind.
type UsageBenchmarkEvent struct {
	// Kind discriminates the event variant. Empty == "subscription" for back-compat.
	Kind UsageBenchmarkKind `json:"kind,omitempty"`
	// TenantID and EnvironmentID are always populated.
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`

	// Subscription-kind fields.
	SubscriptionID string    `json:"subscription_id,omitempty"`
	StartTime      time.Time `json:"start_time,omitempty"`
	EndTime        time.Time `json:"end_time,omitempty"`

	// Analytics-kind fields. AnalyticsRequest carries the raw dto.GetUsageAnalyticsRequest
	// as JSON to avoid an import cycle (events <- dto would cycle through service).
	AnalyticsRequest json.RawMessage `json:"analytics_request,omitempty"`
}

// UsageBenchmarkRecord is one row in the usage_benchmark ClickHouse table.
type UsageBenchmarkRecord struct {
	TenantID           string          `ch:"tenant_id"`
	EnvironmentID      string          `ch:"environment_id"`
	SubscriptionID     string          `ch:"subscription_id"`
	StartTime          time.Time       `ch:"start_time"`
	EndTime            time.Time       `ch:"end_time"`
	FeatureUsageAmount decimal.Decimal `ch:"feature_usage_amount"`
	MeterUsageAmount   decimal.Decimal `ch:"meter_usage_amount"`
	Diff               decimal.Decimal `ch:"diff"`
	Currency           string          `ch:"currency"`
	CreatedAt          time.Time       `ch:"created_at"`
}

// UsageBenchmarkRepository persists benchmark comparison rows.
type UsageBenchmarkRepository interface {
	Insert(ctx context.Context, record *UsageBenchmarkRecord) error
}
