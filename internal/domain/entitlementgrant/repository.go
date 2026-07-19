package entitlementgrant

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

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

	// ExpireLiveByConfigAndCustomer flips any live grant with valid_to <= at to expired
	// so a new grant can open on the same slot. Returns the number of rows updated.
	ExpireLiveByConfigAndCustomer(ctx context.Context, entitlementConfigID, customerID string, at time.Time) (int, error)

	// FindLastByConfigAndCustomer returns the most recently ended grant on this slot,
	// any status, or (nil, nil) if none. Used to butt new windows against the previous valid_to.
	FindLastByConfigAndCustomer(ctx context.Context, entitlementConfigID, customerID string) (*EntitlementGrant, error)

	// FindLiveByConfigAndCustomer returns the current live grant (active/exhausted)
	// on the slot, or (nil, nil) if none. Used after losing an INSERT ... ON CONFLICT race.
	FindLiveByConfigAndCustomer(ctx context.Context, entitlementConfigID, customerID string) (*EntitlementGrant, error)
}
