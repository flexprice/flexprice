package subscription

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionScheduleRepository defines the interface for schedule persistence operations
type SubscriptionScheduleRepository interface {
	// Create creates a new schedule
	Create(ctx context.Context, schedule *SubscriptionSchedule) error

	// Get retrieves a schedule by ID
	Get(ctx context.Context, id string) (*SubscriptionSchedule, error)

	// Update updates an existing schedule
	Update(ctx context.Context, schedule *SubscriptionSchedule) error

	// Delete soft deletes a schedule
	Delete(ctx context.Context, id string) error

	// GetBySubscriptionID retrieves all schedules for a subscription
	GetBySubscriptionID(ctx context.Context, subscriptionID string) ([]*SubscriptionSchedule, error)

	// GetPendingBySubscriptionAndType retrieves a pending schedule of a specific type for a subscription
	GetPendingBySubscriptionAndType(ctx context.Context, subscriptionID string, scheduleType types.SubscriptionScheduleChangeType) (*SubscriptionSchedule, error)

	// List retrieves schedules with filters
	List(ctx context.Context, filter *types.SubscriptionScheduleFilter) ([]*SubscriptionSchedule, error)

	// Count returns the count of schedules matching the filter
	Count(ctx context.Context, filter *types.SubscriptionScheduleFilter) (int, error)
}
