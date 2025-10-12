package alertlogs

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for alert logs persistence operations
type Repository interface {
	// Core operations
	Create(ctx context.Context, alertLog *AlertLog) error
	Get(ctx context.Context, id string) (*AlertLog, error)
	List(ctx context.Context, filter *types.AlertLogFilter) ([]*AlertLog, error)
	Count(ctx context.Context, filter *types.AlertLogFilter) (int, error)

	// Entity-specific operations
	GetLatestByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string) (*AlertLog, error)
	GetLatestByEntityAndAlertType(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType types.AlertType) (*AlertLog, error)
	ListByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*AlertLog, error)
	ListByAlertType(ctx context.Context, alertType types.AlertType, limit int) ([]*AlertLog, error)
}
