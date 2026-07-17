package entitlementgrant

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository is the PG storage surface for entitlement_grants.
//
// The two canonical filter shapes used by the ERD (alert path vs. billing
// path) are expressed through EntitlementGrantFilter helpers:
//   - filter.WithLiveOnly(now)               // alert path: ACTIVE/EXHAUSTED, still in window
//   - filter.WithCycleOverlap(cStart, cEnd)  // billing path: any grant overlapping the cycle
type Repository interface {
	Create(ctx context.Context, g *EntitlementGrant) (*EntitlementGrant, error)
	Get(ctx context.Context, id string) (*EntitlementGrant, error)
	List(ctx context.Context, filter *types.EntitlementGrantFilter) ([]*EntitlementGrant, error)
	Count(ctx context.Context, filter *types.EntitlementGrantFilter) (int, error)
	Update(ctx context.Context, g *EntitlementGrant) (*EntitlementGrant, error)
	Delete(ctx context.Context, id string) error

	// UpdateSnapshot updates the fields that the alert workflow refreshes every
	// tick (usage, grant_status, last_alert_pct, last_alert_at, last_computed_at)
	// without touching immutable fields. Cheaper and safer than a full Update
	// when the workflow processes hundreds of thousands of grants per tick.
	UpdateSnapshot(ctx context.Context, g *EntitlementGrant) error

	// ExpireLiveByConfigAndCustomer transitions any live grant for the given
	// (config, customer) whose window has closed (valid_to <= at) to EXPIRED.
	// Called by ensureGrants (ERD §8.4) immediately before opening a new grant
	// on the same slot — the unique index would otherwise reject the insert.
	// Returns the number of rows updated so the caller can log expirations.
	ExpireLiveByConfigAndCustomer(ctx context.Context, entitlementConfigID, customerID string, at time.Time) (int, error)

	// FindLastByConfigAndCustomer returns the most recently ended grant on
	// this (config, customer) slot, regardless of status. Used by ensureGrants
	// to align a new grant's valid_from to the previous grant's valid_to so
	// back-to-back windows have no gap. Returns (nil, nil) when the slot has
	// never had a grant.
	FindLastByConfigAndCustomer(ctx context.Context, entitlementConfigID, customerID string) (*EntitlementGrant, error)

	// FindLiveByConfigAndCustomer returns the current live grant on the slot
	// (status IN ACTIVE/EXHAUSTED). Used by ensureGrants when it needs to
	// re-read the winner after losing an INSERT ... ON CONFLICT race. Returns
	// (nil, nil) when nothing is live.
	FindLiveByConfigAndCustomer(ctx context.Context, entitlementConfigID, customerID string) (*EntitlementGrant, error)
}
