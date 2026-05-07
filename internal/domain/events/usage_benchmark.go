package events

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// UsageBenchmarkEvent is the thin Kafka payload published by the wallet service.
type UsageBenchmarkEvent struct {
	SubscriptionID string    `json:"subscription_id"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	TenantID       string    `json:"tenant_id"`
	EnvironmentID  string    `json:"environment_id"`
}

// UsageBenchmarkRecord is one row in the usage_benchmark ClickHouse table.
type UsageBenchmarkRecord struct {
	TenantID            string          `ch:"tenant_id"`
	EnvironmentID       string          `ch:"environment_id"`
	SubscriptionID      string          `ch:"subscription_id"`
	StartTime           time.Time       `ch:"start_time"`
	EndTime             time.Time       `ch:"end_time"`
	FeatureUsageAmount  decimal.Decimal `ch:"feature_usage_amount"`
	MeterUsageAmount    decimal.Decimal `ch:"meter_usage_amount"`
	Diff                decimal.Decimal `ch:"diff"`
	Currency            string          `ch:"currency"`
	CreatedAt           time.Time       `ch:"created_at"`
}

// UsageBenchmarkRepository persists benchmark comparison rows.
type UsageBenchmarkRepository interface {
	Insert(ctx context.Context, record *UsageBenchmarkRecord) error
}
