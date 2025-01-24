package system

import (
	"context"

	"github.com/google/uuid"
)

// Repository interface for system events
type Repository interface {
	// CreateEvent creates a new system event
	CreateEvent(ctx context.Context, event *Event) error

	// GetEvents retrieves system events by workflow ID
	GetEvents(ctx context.Context, workflowID string) ([]*Event, error)

	// UpdateEventStatus updates the status of a system event
	UpdateEventStatus(ctx context.Context, eventID uuid.UUID, status string) error
}
