package subscription

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, subscription *Subscription) error
	Get(ctx context.Context, id string) (*Subscription, error)
	Update(ctx context.Context, subscription *Subscription) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.SubscriptionFilter) ([]*Subscription, error)
	ListAll(ctx context.Context, filter *types.SubscriptionFilter) ([]*Subscription, error)
}
