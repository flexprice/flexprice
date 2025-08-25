package alert

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for alert persistence operations
type Repository interface {
	// Create creates a new alert
	Create(ctx context.Context, alert *Alert) error

	// GetLatestByEntity retrieves the latest alert for a given entity and metric
	GetLatestByEntity(ctx context.Context, tenantID, environmentID, entityType, entityID, alertMetric string) (*Alert, error)
}

// Filter defines comprehensive query parameters for searching and filtering alerts
type Filter struct {
	// QueryFilter contains pagination and basic query parameters
	QueryFilter *types.QueryFilter

	// TimeRangeFilter allows filtering by time periods
	TimeRangeFilter *types.TimeRangeFilter

	// Filters contains custom filtering conditions
	Filters []*types.FilterCondition

	// Sort specifies result ordering preferences
	Sort []*types.SortCondition

	// AlertIDs allows filtering by specific alert IDs
	AlertIDs []string

	// EntityTypes filters by specific entity types
	EntityTypes []string

	// EntityIDs filters by specific entity IDs
	EntityIDs []string

	// AlertMetrics filters by specific alert metrics
	AlertMetrics []string

	// AlertStates filters by specific alert states
	AlertStates []string

	// AlertEnabled filters by alert enabled status
	AlertEnabled *bool

	// TenantID filters by specific tenant ID
	TenantID string

	// EnvironmentID filters by specific environment ID
	EnvironmentID string
}

// GetLimit implements BaseFilter interface
func (f *Filter) GetLimit() int {
	if f.QueryFilter == nil || f.QueryFilter.Limit == nil {
		return 0
	}
	return *f.QueryFilter.Limit
}

// GetOffset implements BaseFilter interface
func (f *Filter) GetOffset() int {
	if f.QueryFilter == nil || f.QueryFilter.Offset == nil {
		return 0
	}
	return *f.QueryFilter.Offset
}
