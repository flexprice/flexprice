package entitlementgrant

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// SlotWindowEnd is one (config, subscription) slot's latest window end —
// everything before it is covered by past windows, so the slot's next window
// starts at the first usage after it.
type SlotWindowEnd struct {
	EntitlementConfigID string
	SubscriptionID      string
	ValidTo             time.Time
}

// Repository is the PG storage surface for entitlement_grants.
// Two canonical filter shapes: filter.WithLiveOnly(now) for alerts,
// filter.WithCycleOverlap(cStart, cEnd) for billing.
type Repository interface {
	Create(ctx context.Context, g *EntitlementGrant) (*EntitlementGrant, error)
	Get(ctx context.Context, id string) (*EntitlementGrant, error)
	List(ctx context.Context, filter *types.EntitlementGrantFilter) ([]*EntitlementGrant, error)
	Count(ctx context.Context, filter *types.EntitlementGrantFilter) (int, error)
	Update(ctx context.Context, g *EntitlementGrant) (*EntitlementGrant, error)
	Delete(ctx context.Context, id string) error

	// UpdateSnapshot writes only usage, grant_status, last_computed_at.
	UpdateSnapshot(ctx context.Context, g *EntitlementGrant) error

	// LatestWindowEndBySlot aggregates max(valid_to) per (config, subscription)
	// slot for the customer's grants with valid_to > validToAfter. One row per
	// slot no matter how many windows the cycle accumulated.
	LatestWindowEndBySlot(ctx context.Context, customerID string, validToAfter time.Time) ([]SlotWindowEnd, error)

	// ListOpenOrUnfinalized returns the evaluation working set: grants with
	// valid_to > validToAfter that are open at `at` OR closed with a snapshot
	// predating the close (last_computed_at IS NULL or < valid_to). Constant
	// size per slot regardless of cycle age.
	ListOpenOrUnfinalized(ctx context.Context, customerID string, at, validToAfter time.Time) ([]*EntitlementGrant, error)

	// FindLastBySlot returns the grant with the latest valid_to on the
	// (config, customer, subscription) slot, any status, or (nil, nil) if none.
	// Used only to re-read the winner after losing the INSERT race.
	FindLastBySlot(ctx context.Context, entitlementConfigID, customerID, subscriptionID string) (*EntitlementGrant, error)
}
