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
	ListAll(ctx context.Context, filter *types.FXRateFilter) ([]*FXRate, error)
	Count(ctx context.Context, filter *types.FXRateFilter) (int, error)
	Update(ctx context.Context, r *FXRate) error
	Delete(ctx context.Context, r *FXRate) error // soft-archive: sets status = archived

	// GetTenantRate returns the published tenant-scope rate for a pair, or ErrNotFound.
	GetTenantRate(ctx context.Context, from, to string) (*FXRate, error)

	// FindOverlapping returns published rows for (scope, scopeID, pair) whose
	// [valid_from, valid_to) window overlaps [validFrom, validTo), excluding excludeID.
	// A nil validFrom means −∞, a nil validTo means +∞.
	FindOverlapping(ctx context.Context, scope types.FXRateScope, scopeID, from, to string, validFrom, validTo *time.Time, excludeID string) ([]*FXRate, error)
}
