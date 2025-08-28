package subscription

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// LineItemRepository defines the interface for subscription line item persistence operations
type LineItemRepository interface {
	// Core operations
	Create(ctx context.Context, item *SubscriptionLineItem) error
	Get(ctx context.Context, id string) (*SubscriptionLineItem, error)
	Update(ctx context.Context, item *SubscriptionLineItem) error
	Delete(ctx context.Context, id string) error

	// Bulk operations
	CreateBulk(ctx context.Context, items []*SubscriptionLineItem) error

	// Query operations
	ListBySubscription(ctx context.Context, sub *Subscription) ([]*SubscriptionLineItem, error)

	// Filter based operations
	List(ctx context.Context, filter *types.SubscriptionLineItemFilter) ([]*SubscriptionLineItem, error)
	Count(ctx context.Context, filter *types.SubscriptionLineItemFilter) (int, error)
}
