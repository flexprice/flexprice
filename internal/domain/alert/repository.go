package alert

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for alert settings persistence operations
type Repository interface {
	Create(ctx context.Context, alertSettings *AlertSettings) error
	Get(ctx context.Context, id string) (*AlertSettings, error)
	Update(ctx context.Context, alertSettings *AlertSettings) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.AlertSettingsFilter) ([]*AlertSettings, error)
	Count(ctx context.Context, filter *types.AlertSettingsFilter) (int, error)
}
