package addon

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for addon repository operations
type Repository interface {
	// Addon CRUD operations
	Create(ctx context.Context, addon *Addon) error
	GetByID(ctx context.Context, id string) (*Addon, error)
	GetByLookupKey(ctx context.Context, lookupKey string) (*Addon, error)
	Update(ctx context.Context, addon *Addon) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.AddonFilter) ([]*Addon, error)
	Count(ctx context.Context, filter *types.AddonFilter) (int, error)

	// Subscription Addon operations
	// CreateSubscriptionAddon(ctx context.Context, subscriptionAddon *SubscriptionAddon) error
	// GetSubscriptionAddonByID(ctx context.Context, id string) (*SubscriptionAddon, error)
	// GetSubscriptionAddons(ctx context.Context, subscriptionID string) ([]*SubscriptionAddon, error)
	// UpdateSubscriptionAddon(ctx context.Context, subscriptionAddon *SubscriptionAddon) error
}
