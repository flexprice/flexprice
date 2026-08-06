package alertlogs

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for alert logs persistence operations
type Repository interface {
	// Core operations
	Create(ctx context.Context, alertLog *AlertLog) error
	Get(ctx context.Context, id string) (*AlertLog, error)
	List(ctx context.Context, filter *types.AlertLogFilter) ([]*AlertLog, error)
	Count(ctx context.Context, filter *types.AlertLogFilter) (int, error)

	// Entity-specific operations. When alertSettingID is non-nil, the lookup is scoped to that
	// alert_settings row and restricted to created_at >= periodStart, so a previous billing
	// period's alert never suppresses the current period's first breach (see AlertService).
	GetLatestAlert(ctx context.Context, entityType types.AlertEntityType, entityID string, alertType *types.AlertType, parentEntityType *string, parentEntityID *string, alertSettingID *string, periodStart *time.Time) (*AlertLog, error)
	ListByEntity(ctx context.Context, entityType types.AlertEntityType, entityID string, limit int) ([]*AlertLog, error)
	ListByAlertType(ctx context.Context, alertType types.AlertType, limit int) ([]*AlertLog, error)
}
