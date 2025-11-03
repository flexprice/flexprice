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
	// List retrieves coupon associations based on the provided filter
	List(ctx context.Context, filter *types.CouponAssociationFilter) ([]*CouponAssociation, error)
	// GetBySubscriptionFilter retrieves coupon associations using the domain Filter
	// This is a convenience method that uses the domain filter struct
	GetBySubscriptionFilter(ctx context.Context, filter *Filter) ([]*CouponAssociation, error)
}
