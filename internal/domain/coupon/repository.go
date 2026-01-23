package coupon

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for coupon data access
type Repository interface {
	Create(ctx context.Context, coupon *Coupon) error
	Get(ctx context.Context, id string) (*Coupon, error)
	Update(ctx context.Context, coupon *Coupon) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.CouponFilter) ([]*Coupon, error)
	Count(ctx context.Context, filter *types.CouponFilter) (int, error)
	IncrementRedemptions(ctx context.Context, id string) error
}
