package system

import (
	"time"

	"github.com/google/uuid"
)

type SystemEventType string

const (
	SystemEventTypeSyncMeter            SystemEventType = "sync_meter"
	SystemEventTypeUpdateBillingPeriods SystemEventType = "update_billing_periods"
	// Add other event types here
)

const (
	EventStatusPending   = "pending"
	EventStatusCompleted = "completed"
	EventStatusFailed    = "failed"
)

type Event struct {
	ID        uuid.UUID              `json:"id"`
	Type      SystemEventType        `json:"type"`
	Status    string                 `json:"status"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	CreatedBy string                 `json:"created_by"`
	UpdatedBy string                 `json:"updated_by"`
	TenantID  string                 `json:"tenant_id"`
	Metadata  map[string]interface{} `json:"metadata"`
	Payload   interface{}            `json:"payload"`
}
