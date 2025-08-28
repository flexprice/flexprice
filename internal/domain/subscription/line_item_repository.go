package subscription

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// LineItemRepository defines the interface for subscription line item operations
type LineItemRepository interface {
	// Create creates a new subscription line item
	Create(ctx context.Context, lineItem *SubscriptionLineItem) error

	// CreateBulk creates multiple subscription line items in bulk
	CreateBulk(ctx context.Context, lineItems []*SubscriptionLineItem) error

	// Get retrieves a subscription line item by ID
	Get(ctx context.Context, id string) (*SubscriptionLineItem, error)

	// Update updates an existing subscription line item
	Update(ctx context.Context, lineItem *SubscriptionLineItem) error

	// Delete deletes a subscription line item by ID
	Delete(ctx context.Context, id string) error

	// ListBySubscription retrieves all line items for a subscription
	ListBySubscription(ctx context.Context, sub *Subscription) ([]*SubscriptionLineItem, error)

	// List retrieves subscription line items based on filter
	List(ctx context.Context, filter *types.SubscriptionLineItemFilter) ([]*SubscriptionLineItem, error)
}
