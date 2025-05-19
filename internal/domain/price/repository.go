package price

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Repository defines the interface for price persistence operations
type Repository interface {
	// Core operations
	Create(ctx context.Context, price *Price) error
	Get(ctx context.Context, id string) (*Price, error)
	List(ctx context.Context, filter *types.PriceFilter) ([]*Price, error)
	Count(ctx context.Context, filter *types.PriceFilter) (int, error)
	ListAll(ctx context.Context, filter *types.PriceFilter) ([]*Price, error)
	Update(ctx context.Context, price *Price) error
	Delete(ctx context.Context, id string) error

	// Bulk operations
	CreateBulk(ctx context.Context, prices []*Price) error
	DeleteBulk(ctx context.Context, ids []string) error

	// Subscription price override operations
	CreateSubscriptionPriceOverride(ctx context.Context, override SubscriptionPriceOverride) (*Price, error)
}

// SubscriptionPriceOverride contains the necessary information to create a subscription-scoped price
type SubscriptionPriceOverride struct {
	OriginalPriceID string          // The ID of the original price to override
	SubscriptionID  string          // The ID of the subscription
	NewAmount       decimal.Decimal // The new amount for the override
}
