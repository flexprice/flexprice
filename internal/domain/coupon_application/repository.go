package coupon_application

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for coupon application data access
type Repository interface {
	Create(ctx context.Context, couponApplication *CouponApplication) error
	Get(ctx context.Context, id string) (*CouponApplication, error)
	Update(ctx context.Context, couponApplication *CouponApplication) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.CouponApplicationFilter) ([]*CouponApplication, error)
	Count(ctx context.Context, filter *types.CouponApplicationFilter) (int, error)
}
