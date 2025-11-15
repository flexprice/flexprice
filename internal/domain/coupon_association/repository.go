package coupon_association

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for coupon association data access
type Repository interface {
	Create(ctx context.Context, couponAssociation *CouponAssociation) error
	Get(ctx context.Context, id string) (*CouponAssociation, error)
	Update(ctx context.Context, couponAssociation *CouponAssociation) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.CouponAssociationFilter) ([]*CouponAssociation, error)
	Count(ctx context.Context, filter *types.CouponAssociationFilter) (int, error)
}
