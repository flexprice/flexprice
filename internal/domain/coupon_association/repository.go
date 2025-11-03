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
	// GetBySubscription retrieves subscription-level coupon associations (backwards compatibility)
	GetBySubscription(ctx context.Context, subID string) ([]*CouponAssociation, error)
	// GetBySubscriptionForLineItems retrieves line-item level coupon associations (backwards compatibility)
	GetBySubscriptionForLineItems(ctx context.Context, subID string) ([]*CouponAssociation, error)
}
