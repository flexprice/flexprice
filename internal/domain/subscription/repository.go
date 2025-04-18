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
	Count(ctx context.Context, filter *types.SubscriptionFilter) (int, error)
	ListAll(ctx context.Context, filter *types.SubscriptionFilter) ([]*Subscription, error)
	ListAllTenant(ctx context.Context, filter *types.SubscriptionFilter) ([]*Subscription, error)
	ListByCustomerID(ctx context.Context, customerID string) ([]*Subscription, error)
	ListByIDs(ctx context.Context, subscriptionIDs []string) ([]*Subscription, error)
	CreateWithLineItems(ctx context.Context, subscription *Subscription, items []*SubscriptionLineItem) error
	GetWithLineItems(ctx context.Context, id string) (*Subscription, []*SubscriptionLineItem, error)

	// Pause-related methods
	CreatePause(ctx context.Context, pause *SubscriptionPause) error
	GetPause(ctx context.Context, id string) (*SubscriptionPause, error)
	UpdatePause(ctx context.Context, pause *SubscriptionPause) error
	ListPauses(ctx context.Context, subscriptionID string) ([]*SubscriptionPause, error)
	GetWithPauses(ctx context.Context, id string) (*Subscription, []*SubscriptionPause, error)
}
