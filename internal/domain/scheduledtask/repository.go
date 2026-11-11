package scheduledtask

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for scheduled task persistence operations
type Repository interface {
	// Create creates a new scheduled task
	Create(ctx context.Context, task *ScheduledTask) error

	// Get retrieves a scheduled task by ID
	Get(ctx context.Context, id string) (*ScheduledTask, error)

	// Update updates an existing scheduled task
	Update(ctx context.Context, task *ScheduledTask) error

	// UpdateSettings writes only the fields selected in fields.
	UpdateSettings(ctx context.Context, task *ScheduledTask, fields SettingsFields) error

	// Delete deletes a scheduled task
	Delete(ctx context.Context, id string) error

	// List retrieves all scheduled tasks with optional filters
	List(ctx context.Context, filters *ListFilters) ([]*ScheduledTask, error)

	// GetByConnection retrieves all scheduled tasks for a specific connection
	GetByConnection(ctx context.Context, connectionID string) ([]*ScheduledTask, error)
}

// SettingsFields selects the columns UpdateSettings writes.
type SettingsFields struct {
	Name     bool
	Schedule bool
	Email    bool
}

// ListFilters defines filters for listing scheduled tasks
type ListFilters struct {
	TenantID      string
	EnvironmentID string
	ConnectionID  string
	EntityType    types.ScheduledTaskEntityType
	Interval      types.ScheduledTaskInterval
	Enabled       *bool
	Origin        types.ExportOrigin
	ExcludeOrigin types.ExportOrigin
	Limit         int
	Offset        int
}
