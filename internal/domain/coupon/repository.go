package coupon

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for coupon data access
type Repository interface {
	Create(ctx context.Context, coupon *Coupon) error
	Get(ctx context.Context, id string) (*Coupon, error)
	GetByCode(ctx context.Context, code string) (*Coupon, error)
	Update(ctx context.Context, coupon *Coupon) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.CouponFilter) ([]*Coupon, error)
	Count(ctx context.Context, filter *types.CouponFilter) (int, error)
	// IncrementRedemptions atomically increments total_redemptions, enforcing
	// maxRedemptions as a DB-level guard when non-nil (nil = unlimited). The
	// caller is expected to pass the coupon's own MaxRedemptions value (from
	// an earlier Get/GetByCode) — it's immutable coupon config, safe to reuse,
	// unlike total_redemptions which the guard re-checks fresh in the database.
	IncrementRedemptions(ctx context.Context, id string, maxRedemptions *int) error
}
