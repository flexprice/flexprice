package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryEntitlementGrantStore implements entitlementgrant.Repository in memory.
type InMemoryEntitlementGrantStore struct {
	*InMemoryStore[*entitlementgrant.EntitlementGrant]
}

func NewInMemoryEntitlementGrantStore() *InMemoryEntitlementGrantStore {
	return &InMemoryEntitlementGrantStore{
		InMemoryStore: NewInMemoryStore[*entitlementgrant.EntitlementGrant](),
	}
}

// entitlementGrantFilterFn mirrors applyEntitlementGrantFilter in the ent repo.
func entitlementGrantFilterFn(ctx context.Context, g *entitlementgrant.EntitlementGrant, filter interface{}) bool {
	if g == nil {
		return false
	}

	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok && tenantID != "" {
		if g.TenantID != tenantID {
			return false
		}
	}
	if !CheckEnvironmentFilter(ctx, g.EnvironmentID) {
		return false
	}

	f, ok := filter.(*types.EntitlementGrantFilter)
	if !ok {
		return true
	}

	if len(f.IDs) > 0 && !lo.Contains(f.IDs, g.ID) {
		return false
	}
	if len(f.EntitlementConfigIDs) > 0 && !lo.Contains(f.EntitlementConfigIDs, g.EntitlementConfigID) {
		return false
	}
	if len(f.CustomerIDs) > 0 && !lo.Contains(f.CustomerIDs, g.CustomerID) {
		return false
	}
	if len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, g.SubscriptionID) {
		return false
	}
	if f.ScopeEntityType != nil && g.ScopeEntityType != *f.ScopeEntityType {
		return false
	}
	if len(f.ScopeEntityIDs) > 0 && !lo.Contains(f.ScopeEntityIDs, g.ScopeEntityID) {
		return false
	}
	if len(f.Statuses) > 0 && !lo.Contains(f.Statuses, g.GrantStatus) {
		return false
	}
	if f.Measure != nil && g.Measure != *f.Measure {
		return false
	}

	// Alert-path shortcut: "currently in window at `at`".
	if f.ValidAtOrAfter != nil {
		at := *f.ValidAtOrAfter
		if !g.ValidFrom.Before(at) && !g.ValidFrom.Equal(at) {
			return false
		}
		if !g.ValidTo.After(at) {
			return false
		}
	}
	// Billing-path shortcut: "overlaps [ValidToAfter, ValidFromBefore)". Half-open
	// interval overlap = a.start < b.end AND b.start < a.end.
	if f.ValidFromBefore != nil && !g.ValidFrom.Before(*f.ValidFromBefore) {
		return false
	}
	if f.ValidToAfter != nil && !g.ValidTo.After(*f.ValidToAfter) {
		return false
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && g.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && g.CreatedAt.After(*f.EndTime) {
			return false
		}
	}
	return true
}

// entitlementGrantSortFn keeps List deterministic: newest valid_to first, then
// newest created_at as tiebreaker. Callers relying on "the latest grant on
// this slot" get stable ordering.
func entitlementGrantSortFn(i, j *entitlementgrant.EntitlementGrant) bool {
	if i == nil || j == nil {
		return false
	}
	if !i.ValidTo.Equal(j.ValidTo) {
		return i.ValidTo.After(j.ValidTo)
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryEntitlementGrantStore) Create(ctx context.Context, g *entitlementgrant.EntitlementGrant) (*entitlementgrant.EntitlementGrant, error) {
	if g == nil {
		return nil, errors.NewError("entitlement grant cannot be nil").Mark(errors.ErrValidation)
	}
	if g.EnvironmentID == "" {
		g.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	// Mirrors the unique (slot, valid_from) index — the INSERT race arbiter.
	existing, err := s.InMemoryStore.List(ctx, nil, func(cctx context.Context, other *entitlementgrant.EntitlementGrant, _ interface{}) bool {
		return other != nil &&
			other.EntitlementConfigID == g.EntitlementConfigID &&
			other.CustomerID == g.CustomerID &&
			other.SubscriptionID == g.SubscriptionID &&
			other.ValidFrom.Equal(g.ValidFrom)
	}, entitlementGrantSortFn)
	if err == nil && len(existing) > 0 {
		return nil, errors.NewError("grant with this window start already exists for the slot").
			WithReportableDetails(map[string]interface{}{
				"entitlement_config_id": g.EntitlementConfigID,
				"customer_id":           g.CustomerID,
				"subscription_id":       g.SubscriptionID,
				"valid_from":            g.ValidFrom,
			}).
			Mark(errors.ErrAlreadyExists)
	}
	if err := s.InMemoryStore.Create(ctx, g.ID, g); err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to create entitlement grant").
			WithReportableDetails(map[string]interface{}{"id": g.ID}).
			Mark(errors.ErrDatabase)
	}
	return g, nil
}

func (s *InMemoryEntitlementGrantStore) Get(ctx context.Context, id string) (*entitlementgrant.EntitlementGrant, error) {
	g, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Entitlement grant not found").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(errors.ErrNotFound)
	}
	return g, nil
}

func (s *InMemoryEntitlementGrantStore) List(ctx context.Context, filter *types.EntitlementGrantFilter) ([]*entitlementgrant.EntitlementGrant, error) {
	rows, err := s.InMemoryStore.List(ctx, filter, entitlementGrantFilterFn, entitlementGrantSortFn)
	if err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to list entitlement grants").
			Mark(errors.ErrDatabase)
	}
	return rows, nil
}

func (s *InMemoryEntitlementGrantStore) Count(ctx context.Context, filter *types.EntitlementGrantFilter) (int, error) {
	n, err := s.InMemoryStore.Count(ctx, filter, entitlementGrantFilterFn)
	if err != nil {
		return 0, errors.WithError(err).
			WithHint("Failed to count entitlement grants").
			Mark(errors.ErrDatabase)
	}
	return n, nil
}

func (s *InMemoryEntitlementGrantStore) Update(ctx context.Context, g *entitlementgrant.EntitlementGrant) (*entitlementgrant.EntitlementGrant, error) {
	if g == nil {
		return nil, errors.NewError("entitlement grant cannot be nil").Mark(errors.ErrValidation)
	}
	if err := s.InMemoryStore.Update(ctx, g.ID, g); err != nil {
		return nil, errors.WithError(err).
			WithHint("Failed to update entitlement grant").
			WithReportableDetails(map[string]interface{}{"id": g.ID}).
			Mark(errors.ErrDatabase)
	}
	return g, nil
}

func (s *InMemoryEntitlementGrantStore) Delete(ctx context.Context, id string) error {
	if err := s.InMemoryStore.Delete(ctx, id); err != nil {
		return errors.WithError(err).
			WithHint("Failed to delete entitlement grant").
			Mark(errors.ErrDatabase)
	}
	return nil
}

// UpdateSnapshot patches the workflow-hot fields — matches the real repo's
// selective update so tests can distinguish "snapshot write" from "full row
// rewrite" in traces.
func (s *InMemoryEntitlementGrantStore) UpdateSnapshot(ctx context.Context, g *entitlementgrant.EntitlementGrant) error {
	if g == nil {
		return errors.NewError("entitlement grant cannot be nil").Mark(errors.ErrValidation)
	}
	existing, err := s.InMemoryStore.Get(ctx, g.ID)
	if err != nil {
		return errors.WithError(err).
			WithHint("Entitlement grant not found for snapshot update").
			WithReportableDetails(map[string]interface{}{"id": g.ID}).
			Mark(errors.ErrNotFound)
	}
	existing.Usage = g.Usage
	existing.GrantStatus = g.GrantStatus
	existing.LastComputedAt = g.LastComputedAt
	existing.UpdatedAt = time.Now().UTC()
	return s.InMemoryStore.Update(ctx, g.ID, existing)
}

func (s *InMemoryEntitlementGrantStore) LatestWindowEndBySlot(
	ctx context.Context,
	customerID string,
	validToAfter time.Time,
) ([]entitlementgrant.SlotWindowEnd, error) {
	matches, err := s.InMemoryStore.List(ctx, nil, func(cctx context.Context, g *entitlementgrant.EntitlementGrant, _ interface{}) bool {
		return g != nil && g.CustomerID == customerID && g.ValidTo.After(validToAfter)
	}, entitlementGrantSortFn)
	if err != nil {
		return nil, err
	}
	latest := map[string]entitlementgrant.SlotWindowEnd{}
	for _, g := range matches {
		key := g.EntitlementConfigID + "/" + g.SubscriptionID
		if cur, ok := latest[key]; !ok || g.ValidTo.After(cur.ValidTo) {
			latest[key] = entitlementgrant.SlotWindowEnd{
				EntitlementConfigID: g.EntitlementConfigID,
				SubscriptionID:      g.SubscriptionID,
				ValidTo:             g.ValidTo,
			}
		}
	}
	return lo.Values(latest), nil
}

func (s *InMemoryEntitlementGrantStore) ListOpenOrUnfinalized(
	ctx context.Context,
	customerID string,
	at time.Time,
	validToAfter time.Time,
) ([]*entitlementgrant.EntitlementGrant, error) {
	return s.InMemoryStore.List(ctx, nil, func(cctx context.Context, g *entitlementgrant.EntitlementGrant, _ interface{}) bool {
		return g != nil &&
			g.CustomerID == customerID &&
			g.ValidTo.After(validToAfter) &&
			(g.ValidTo.After(at) || g.LastComputedAt == nil || g.LastComputedAt.Before(g.ValidTo))
	}, entitlementGrantSortFn)
}

func (s *InMemoryEntitlementGrantStore) FindLastBySlot(
	ctx context.Context,
	entitlementConfigID string,
	customerID string,
	subscriptionID string,
) (*entitlementgrant.EntitlementGrant, error) {
	matches, err := s.InMemoryStore.List(ctx, nil, func(cctx context.Context, g *entitlementgrant.EntitlementGrant, _ interface{}) bool {
		return g != nil &&
			g.EntitlementConfigID == entitlementConfigID &&
			g.CustomerID == customerID &&
			g.SubscriptionID == subscriptionID
	}, entitlementGrantSortFn) // newest valid_to first
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}
	return matches[0], nil
}
