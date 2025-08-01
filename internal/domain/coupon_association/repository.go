package coupon_association

import (
	"context"
)

// Repository defines the interface for coupon association data access
type Repository interface {
	Create(ctx context.Context, couponAssociation *CouponAssociation) error
	Get(ctx context.Context, id string) (*CouponAssociation, error)
	Update(ctx context.Context, couponAssociation *CouponAssociation) error
	Delete(ctx context.Context, id string) error
	GetBySubscription(ctx context.Context, subscriptionID string) ([]*CouponAssociation, error)
}
