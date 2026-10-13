package fxrate

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository is the persistence contract for FX rates.
type Repository interface {
	Create(ctx context.Context, r *FXRate) error
	Get(ctx context.Context, id string) (*FXRate, error)
	List(ctx context.Context, filter *types.FXRateFilter) ([]*FXRate, error)
	Count(ctx context.Context, filter *types.FXRateFilter) (int, error)
	Update(ctx context.Context, r *FXRate) error
	Delete(ctx context.Context, r *FXRate) error // soft-archive: sets status = archived

	// GetTenantRate returns the published tenant-scope rate for a pair, or ErrNotFound.
	GetTenantRate(ctx context.Context, from, to string) (*FXRate, error)

	// FindOverlapping returns published rows for (scope, scopeID, pair) whose
	// [start_date, end_date) window overlaps [startDate, endDate), excluding excludeID.
	// A nil startDate means −∞, a nil endDate means +∞.
	FindOverlapping(ctx context.Context, scope types.FXRateScope, scopeID, from, to string, startDate, endDate *time.Time, excludeID string) ([]*FXRate, error)
}
