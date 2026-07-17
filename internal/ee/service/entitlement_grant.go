package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// EntitlementGrantService owns the lifecycle of entitlement_grants rows:
// opening new time-boxed grants when their EC has no live one, expiring stale
// slots so the next grant can INSERT, and returning the hydrated set of live
// grants the alert workflow will iterate.
//
// The workflow calls EnsureGrants once per tick. Everything else (usage
// refresh, alert-state transitions, snapshot writes) is layered on top and
// lives in the workflow/activity itself.
type EntitlementGrantService interface {
	// EnsureGrants is the ERD §8.4 primitive. For the customer's active
	// subscriptions:
	//   1. Resolves each subscription's entitlements (plan + addon + sub
	//      overrides via GetAggregatedSubscriptionEntitlements' underlying
	//      row-level fetch).
	//   2. Expires stale-but-live rows on any EC that needs a new grant.
	//   3. Opens a new grant per time_boxed EC without a live one, honoring
	//      the cycle-boundary cap and the 1-hour minimum.
	//   4. Returns the full hydrated set of live grants (existing + newly
	//      created) at `at`.
	//
	// Idempotent: calling twice within the same tick yields the same set,
	// with no duplicate inserts (partial unique index + ON CONFLICT).
	EnsureGrants(ctx context.Context, cust *customer.Customer, at time.Time) ([]*entitlementgrant.EntitlementGrant, error)
}

type entitlementGrantService struct {
	ServiceParams
}

func NewEntitlementGrantService(params ServiceParams) EntitlementGrantService {
	return &entitlementGrantService{ServiceParams: params}
}

// -----------------------------------------------------------------------------
// EnsureGrants
// -----------------------------------------------------------------------------

func (s *entitlementGrantService) EnsureGrants(
	ctx context.Context,
	cust *customer.Customer,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	if cust == nil {
		return nil, ierr.NewError("customer is required").Mark(ierr.ErrValidation)
	}

	// 1. Active subscriptions for this customer.
	subs, err := s.listActiveSubscriptions(ctx, cust.ID, at)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		// No subs → no grants. Also clear anything that had gone live from a
		// prior sub cycle; callers depend on the empty return.
		return nil, nil
	}

	// 2. Resolve entitlement configs (rows) per subscription. Aggregation
	//    happens elsewhere; here we need the individual rows so we can open
	//    one grant per EC that opts into time_boxed.
	subscriptionSvc := NewSubscriptionService(s.ServiceParams)
	ecsBySub := make(map[string][]*entitlement.Entitlement, len(subs))
	for _, sub := range subs {
		ents, err := subscriptionSvc.GetSubscriptionEntitlements(ctx, sub.ID)
		if err != nil {
			return nil, err
		}
		rows := make([]*entitlement.Entitlement, 0, len(ents))
		for _, e := range ents {
			if e == nil || e.Entitlement == nil {
				continue
			}
			rows = append(rows, e.Entitlement)
		}
		ecsBySub[sub.ID] = rows
	}

	// 3. Read all live grants for this customer in one shot. The workflow
	//    iterates by (EC, sub); a single query keyed on customer_id hits the
	//    partial index on (tenant, env, customer) WHERE grant_status IN
	//    live-statuses and is much cheaper than one query per EC.
	liveFilter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(cust.ID).
		WithLiveOnly(at)
	liveGrants, err := s.EntitlementGrantRepo.List(ctx, liveFilter)
	if err != nil {
		return nil, err
	}

	// Index existing live grants by EC id for O(1) "already covered?" lookups
	// during the create loop. Parallel ECs on the same feature are keyed
	// separately by EC id, so their independence is preserved without extra
	// bookkeeping here.
	liveByConfigID := make(map[string]*entitlementgrant.EntitlementGrant, len(liveGrants))
	for _, g := range liveGrants {
		liveByConfigID[g.EntitlementConfigID] = g
	}

	// 4. Open missing grants per (sub, EC). Any newly created rows join the
	//    return set alongside the pre-existing live grants.
	opened, err := s.openMissingGrants(ctx, subs, ecsBySub, liveByConfigID, at)
	if err != nil {
		return nil, err
	}

	if len(opened) == 0 {
		return liveGrants, nil
	}
	return append(liveGrants, opened...), nil
}

// listActiveSubscriptions fetches subscriptions active at `at` for the customer.
// Uses ActiveAt so we naturally exclude anything that expired mid-tick.
func (s *entitlementGrantService) listActiveSubscriptions(
	ctx context.Context,
	customerID string,
	at time.Time,
) ([]*subscription.Subscription, error) {
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = customerID
	filter.SubscriptionStatus = []types.SubscriptionStatus{types.SubscriptionStatusActive, types.SubscriptionStatusTrialing}
	filter.ActiveAt = &at
	return s.SubRepo.List(ctx, filter)
}

// openMissingGrants walks (subscription, EC) pairs and INSERTs a new grant for
// each time_boxed EC without an existing live grant. Returns the freshly-opened
// rows so the caller can append them to the live set.
func (s *entitlementGrantService) openMissingGrants(
	ctx context.Context,
	subs []*subscription.Subscription,
	ecsBySub map[string][]*entitlement.Entitlement,
	liveByConfigID map[string]*entitlementgrant.EntitlementGrant,
	at time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	opened := make([]*entitlementgrant.EntitlementGrant, 0)
	for _, sub := range subs {
		for _, ec := range ecsBySub[sub.ID] {
			if !isTimeBoxedGrantConfig(ec) {
				continue
			}
			if _, ok := liveByConfigID[ec.ID]; ok {
				continue
			}
			g, err := s.openOneGrant(ctx, sub, ec, at)
			if err != nil {
				return nil, err
			}
			if g == nil {
				continue // sub-1-hour window skipped, or lost race resolved to caller-visible row
			}
			opened = append(opened, g)
			liveByConfigID[ec.ID] = g
		}
	}
	return opened, nil
}

// openOneGrant computes the window per ERD §8.4, expires any stale-but-live
// row on the slot, and INSERTs the new grant with ON CONFLICT DO NOTHING. When
// the insert loses to a concurrent open, we re-read the winning row so the
// caller ends up with the same live grant either way.
//
// Returns (nil, nil) when the window would be shorter than the product-mandated
// 1-hour floor — the next cycle rollover will open a fresh grant naturally.
func (s *entitlementGrantService) openOneGrant(
	ctx context.Context,
	sub *subscription.Subscription,
	ec *entitlement.Entitlement,
	at time.Time,
) (*entitlementgrant.EntitlementGrant, error) {
	dur, err := ec.GrantDuration()
	if err != nil {
		return nil, err
	}

	validFrom, validTo, ok, err := s.computeGrantWindow(ctx, ec, sub, at, dur)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.Logger.Debug(ctx, "skipping sub-1-hour trailing grant window",
			"entitlement_config_id", ec.ID,
			"subscription_id", sub.ID,
			"customer_id", sub.CustomerID,
			"valid_from", validFrom,
			"valid_to", validTo)
		return nil, nil
	}

	// Free the slot if the last live grant on it has already closed. Only
	// touches rows the partial index would otherwise block.
	if _, err := s.EntitlementGrantRepo.ExpireLiveByConfigAndCustomer(ctx, ec.ID, sub.CustomerID, at); err != nil {
		return nil, err
	}

	newGrant := &entitlementgrant.EntitlementGrant{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT),
		EntitlementConfigID: ec.ID,
		CustomerID:          sub.CustomerID,
		SubscriptionID:      sub.ID,
		// Feature-scoped in Phase 1. The scope columns are ready for future
		// SUBSCRIPTION/GROUP grants without needing another migration.
		ScopeEntityType: types.EntitlementGrantScopeFeature,
		ScopeEntityID:   ec.FeatureID,
		Measure:         ec.GrantMeasure,
		Quota:           lo.FromPtr(ec.GrantQuota),
		ValidFrom:       validFrom,
		ValidTo:         validTo,
		GrantStatus:     types.EntitlementGrantStatusActive,
		EnvironmentID:   types.GetEnvironmentID(ctx),
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	if err := newGrant.Validate(); err != nil {
		return nil, err
	}

	created, err := s.EntitlementGrantRepo.Create(ctx, newGrant)
	if err == nil {
		return created, nil
	}
	if !ierr.IsAlreadyExists(err) {
		return nil, err
	}
	// Lost the race — another worker (or the same workflow retrying) already
	// opened a grant on this slot. Return the winner so the caller sees a
	// consistent live set.
	winner, wErr := s.EntitlementGrantRepo.FindLiveByConfigAndCustomer(ctx, ec.ID, sub.CustomerID)
	if wErr != nil {
		return nil, wErr
	}
	return winner, nil
}

// computeGrantWindow encodes the ERD §8.4 window math in one place.
//   - valid_from = last grant's valid_to (this cycle) OR max(at, cycle_start).
//   - Drift guard: never let valid_from land more than 5 min in the past.
//   - Cycle-boundary cap: valid_to <= sub.current_period_end.
//   - If the resulting window is shorter than the 1-hour minimum, return
//     ok=false so the caller can skip cleanly.
func (s *entitlementGrantService) computeGrantWindow(
	ctx context.Context,
	ec *entitlement.Entitlement,
	sub *subscription.Subscription,
	at time.Time,
	dur time.Duration,
) (time.Time, time.Time, bool, error) {
	cycleStart := sub.CurrentPeriodStart
	cycleEnd := sub.CurrentPeriodEnd

	// Anchor to the previous grant's valid_to inside this cycle if it exists;
	// otherwise start from now (clamped to the cycle start). This mirrors the
	// pseudocode in the ERD literally.
	validFrom := latestOf(at, cycleStart)
	last, err := s.EntitlementGrantRepo.FindLastByConfigAndCustomer(ctx, ec.ID, sub.CustomerID)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if last != nil && !last.ValidTo.Before(cycleStart) && !last.ValidTo.After(cycleEnd) {
		validFrom = last.ValidTo
	}

	// Drift guard: a stale trigger with valid_from far in the past would
	// otherwise credit unpriced historical usage against the fresh grant.
	// Cap to at-5min. Aligns exactly with the ERD constant.
	driftCap := at.Add(-5 * time.Minute)
	if validFrom.Before(driftCap) {
		validFrom = driftCap
	}

	// Cycle-boundary cap. Grants never straddle two cycles — pricer stays
	// cycle-scoped, ERD §11.1 rationale.
	proposedValidTo := validFrom.Add(dur)
	validTo := proposedValidTo
	if validTo.After(cycleEnd) {
		validTo = cycleEnd
	}

	if validTo.Sub(validFrom) < types.EntitlementGrantMinDuration {
		return validFrom, validTo, false, nil
	}
	return validFrom, validTo, true, nil
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// isTimeBoxedGrantConfig returns true when the EC opts into grant lifecycle.
// A quiet false for legacy or missing configs keeps callers loop-friendly.
func isTimeBoxedGrantConfig(e *entitlement.Entitlement) bool {
	if e == nil {
		return false
	}
	return e.GrantType == types.EntitlementGrantTypeTimeBoxed
}

// latestOf returns the later of the two times. Used to anchor valid_from to at
// least the cycle start when no previous grant exists on the slot.
func latestOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
