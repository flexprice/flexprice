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

// EntitlementGrantService owns the lifecycle of entitlement_grants rows.
type EntitlementGrantService interface {
	// EnsureGrants opens a live grant per time-boxed entitlement config on the
	// customer's active subscriptions, expires stale rows, and returns the
	// hydrated live set. Idempotent per (config, customer) slot.
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

	subs, err := s.listActiveSubscriptions(ctx, cust.ID, at)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}

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

	// Read all live grants in one query keyed on customer_id (partial index).
	liveFilter := types.NewNoLimitEntitlementGrantFilter().
		WithCustomerIDs(cust.ID).
		WithLiveOnly(at)
	liveGrants, err := s.EntitlementGrantRepo.List(ctx, liveFilter)
	if err != nil {
		return nil, err
	}

	liveByConfigID := make(map[string]*entitlementgrant.EntitlementGrant, len(liveGrants))
	for _, g := range liveGrants {
		liveByConfigID[g.EntitlementConfigID] = g
	}

	opened, err := s.openMissingGrants(ctx, subs, ecsBySub, liveByConfigID, at)
	if err != nil {
		return nil, err
	}

	if len(opened) == 0 {
		return liveGrants, nil
	}
	return append(liveGrants, opened...), nil
}

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
				continue
			}
			opened = append(opened, g)
			liveByConfigID[ec.ID] = g
		}
	}
	return opened, nil
}

// openOneGrant expires the stale slot and inserts a new grant. On INSERT conflict
// it re-reads the winner. Returns (nil, nil) when the window is < 1 hour.
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

	if _, err := s.EntitlementGrantRepo.ExpireLiveByConfigAndCustomer(ctx, ec.ID, sub.CustomerID, at); err != nil {
		return nil, err
	}

	newGrant := &entitlementgrant.EntitlementGrant{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENTITLEMENT_GRANT),
		EntitlementConfigID: ec.ID,
		CustomerID:          sub.CustomerID,
		SubscriptionID:      sub.ID,
		ScopeEntityType:     types.EntitlementGrantScopeFeature,
		ScopeEntityID:       ec.FeatureID,
		Measure:             ec.GrantMeasure,
		Quota:               lo.FromPtr(ec.GrantQuota),
		ValidFrom:           validFrom,
		ValidTo:             validTo,
		GrantStatus:         types.EntitlementGrantStatusActive,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
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
	// Lost the race; re-read the winner so callers see a consistent live set.
	winner, wErr := s.EntitlementGrantRepo.FindLiveByConfigAndCustomer(ctx, ec.ID, sub.CustomerID)
	if wErr != nil {
		return nil, wErr
	}
	return winner, nil
}

// computeGrantWindow derives [valid_from, valid_to) for a new grant:
// anchor on previous valid_to inside the cycle, drift-guard 5min, cap to cycle_end.
// Returns ok=false if the window would be under 1 hour.
func (s *entitlementGrantService) computeGrantWindow(
	ctx context.Context,
	ec *entitlement.Entitlement,
	sub *subscription.Subscription,
	at time.Time,
	dur time.Duration,
) (time.Time, time.Time, bool, error) {
	cycleStart := sub.CurrentPeriodStart
	cycleEnd := sub.CurrentPeriodEnd

	validFrom := latestOf(at, cycleStart)
	last, err := s.EntitlementGrantRepo.FindLastByConfigAndCustomer(ctx, ec.ID, sub.CustomerID)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	if last != nil && !last.ValidTo.Before(cycleStart) && !last.ValidTo.After(cycleEnd) {
		validFrom = last.ValidTo
	}

	// Drift guard: stale triggers can't credit unpriced historical usage.
	driftCap := at.Add(-5 * time.Minute)
	if validFrom.Before(driftCap) {
		validFrom = driftCap
	}

	// Cycle-boundary cap: grants never straddle two cycles.
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

func isTimeBoxedGrantConfig(e *entitlement.Entitlement) bool {
	if e == nil {
		return false
	}
	return e.GrantType == types.EntitlementGrantTypeTimeBoxed
}

func latestOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
