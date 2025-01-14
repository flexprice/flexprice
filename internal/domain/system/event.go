package system

import (
	"context"
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
	ID        uuid.UUID       `json:"id"`
	Type      SystemEventType `json:"type"`
	Payload   interface{}     `json:"payload"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	CreatedBy string          `json:"created_by"`
	UpdatedBy string          `json:"updated_by"`
}

// Repository interface for system events
type Repository interface {
	CreateEvent(ctx context.Context, event *Event) error
	GetEvents(ctx context.Context, workflowID string) ([]*Event, error)
	UpdateEventStatus(ctx context.Context, eventID uuid.UUID, status string) error
}
