package coupon_application

import (
	"context"
)

// Repository defines the interface for coupon application data access
type Repository interface {
	Create(ctx context.Context, couponApplication *CouponApplication) error
	Get(ctx context.Context, id string) (*CouponApplication, error)
	Update(ctx context.Context, couponApplication *CouponApplication) error
	Delete(ctx context.Context, id string) error
	GetByInvoice(ctx context.Context, invoiceID string) ([]*CouponApplication, error)
	GetBySubscription(ctx context.Context, subscriptionID string) ([]*CouponApplication, error)
	GetBySubscriptionAndCoupon(ctx context.Context, subscriptionID string, couponID string) ([]*CouponApplication, error)
	CountBySubscriptionAndCoupon(ctx context.Context, subscriptionID string, couponID string) (int, error)
}
