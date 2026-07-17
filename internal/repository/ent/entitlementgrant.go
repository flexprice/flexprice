package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/entitlementgrant"
	"github.com/flexprice/flexprice/ent/predicate"
	domainGrant "github.com/flexprice/flexprice/internal/domain/entitlementgrant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// entitlementGrantRepository is the ent-backed implementation of the domain
// Repository interface. Follows the same shape as the other row-per-file
// repositories in this package (spans on entry, tenant/env filter helper on
// every query, domain marshalling at the boundary).
type entitlementGrantRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewEntitlementGrantRepository(client postgres.IClient, log *logger.Logger) domainGrant.Repository {
	return &entitlementGrantRepository{client: client, log: log}
}

// -----------------------------------------------------------------------------
// tenant/env scoping — pulled into one helper so every query gets the same
// filter and we can't accidentally leak across tenants.
// -----------------------------------------------------------------------------

func (r *entitlementGrantRepository) scoped(ctx context.Context) *ent.EntitlementGrantQuery {
	return r.client.Reader(ctx).EntitlementGrant.Query().
		Where(
			entitlementgrant.TenantID(types.GetTenantID(ctx)),
			entitlementgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		)
}

// -----------------------------------------------------------------------------
// Create / Get / Update / Delete
// -----------------------------------------------------------------------------

func (r *entitlementGrantRepository) Create(ctx context.Context, g *domainGrant.EntitlementGrant) (*domainGrant.EntitlementGrant, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "create", map[string]interface{}{
		"entitlement_config_id": g.EntitlementConfigID,
		"customer_id":           g.CustomerID,
		"tenant_id":             g.TenantID,
	})
	defer FinishSpan(span)

	if g.EnvironmentID == "" {
		g.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	client := r.client.Writer(ctx)
	created, err := client.EntitlementGrant.Create().
		SetID(g.ID).
		SetEntitlementConfigID(g.EntitlementConfigID).
		SetCustomerID(g.CustomerID).
		SetSubscriptionID(g.SubscriptionID).
		SetScopeEntityType(defaultedScopeEntityType(g.ScopeEntityType)).
		SetScopeEntityID(g.ScopeEntityID).
		SetMeasure(g.Measure).
		SetQuota(g.Quota).
		SetUsage(g.Usage).
		SetValidFrom(g.ValidFrom).
		SetValidTo(g.ValidTo).
		SetGrantStatus(defaultedGrantStatus(g.GrantStatus)).
		SetNillableLastAlertPct(g.LastAlertPct).
		SetNillableLastAlertAt(g.LastAlertAt).
		SetNillableLastComputedAt(g.LastComputedAt).
		SetTenantID(g.TenantID).
		SetEnvironmentID(g.EnvironmentID).
		SetStatus(string(g.Status)).
		SetCreatedAt(g.CreatedAt).
		SetUpdatedAt(g.UpdatedAt).
		SetCreatedBy(g.CreatedBy).
		SetUpdatedBy(g.UpdatedBy).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			// A live grant (active or exhausted) already exists for this
			// (config, customer). ensureGrants handles this by reading the
			// winning row; other callers get a typed error.
			return nil, ierr.WithError(err).
				WithHint("A live grant already exists for this (entitlement_config, customer)").
				WithReportableDetails(map[string]interface{}{
					"entitlement_config_id": g.EntitlementConfigID,
					"customer_id":           g.CustomerID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to create entitlement grant").
			Mark(ierr.ErrDatabase)
	}
	return domainGrant.FromEnt(created), nil
}

func (r *entitlementGrantRepository) Get(ctx context.Context, id string) (*domainGrant.EntitlementGrant, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "get", map[string]interface{}{
		"id":        id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	row, err := r.scoped(ctx).Where(entitlementgrant.ID(id)).Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Entitlement grant not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to load entitlement grant").
			Mark(ierr.ErrDatabase)
	}
	return domainGrant.FromEnt(row), nil
}

func (r *entitlementGrantRepository) Update(ctx context.Context, g *domainGrant.EntitlementGrant) (*domainGrant.EntitlementGrant, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "update", map[string]interface{}{
		"id":        g.ID,
		"tenant_id": g.TenantID,
	})
	defer FinishSpan(span)

	updated, err := r.client.Writer(ctx).EntitlementGrant.UpdateOneID(g.ID).
		Where(
			entitlementgrant.TenantID(g.TenantID),
			entitlementgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetUsage(g.Usage).
		SetValidTo(g.ValidTo).
		SetGrantStatus(defaultedGrantStatus(g.GrantStatus)).
		SetNillableLastAlertPct(g.LastAlertPct).
		SetNillableLastAlertAt(g.LastAlertAt).
		SetNillableLastComputedAt(g.LastComputedAt).
		SetStatus(string(g.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Entitlement grant not found").
				WithReportableDetails(map[string]interface{}{"id": g.ID}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to update entitlement grant").
			Mark(ierr.ErrDatabase)
	}
	return domainGrant.FromEnt(updated), nil
}

func (r *entitlementGrantRepository) Delete(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "entitlement_grant", "delete", map[string]interface{}{
		"id":        id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).EntitlementGrant.Delete().
		Where(
			entitlementgrant.ID(id),
			entitlementgrant.TenantID(types.GetTenantID(ctx)),
			entitlementgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exec(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete entitlement grant").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Workflow-hot writes: single-column-set updates that skip the full Update.
// -----------------------------------------------------------------------------

func (r *entitlementGrantRepository) UpdateSnapshot(ctx context.Context, g *domainGrant.EntitlementGrant) error {
	span := StartRepositorySpan(ctx, "entitlement_grant", "update_snapshot", map[string]interface{}{
		"id":        g.ID,
		"tenant_id": g.TenantID,
	})
	defer FinishSpan(span)

	q := r.client.Writer(ctx).EntitlementGrant.UpdateOneID(g.ID).
		Where(
			entitlementgrant.TenantID(g.TenantID),
			entitlementgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetUsage(g.Usage).
		SetGrantStatus(defaultedGrantStatus(g.GrantStatus)).
		SetNillableLastAlertPct(g.LastAlertPct).
		SetNillableLastAlertAt(g.LastAlertAt).
		SetNillableLastComputedAt(g.LastComputedAt).
		SetUpdatedAt(time.Now().UTC())

	if err := q.Exec(ctx); err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Entitlement grant not found for snapshot update").
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update entitlement grant snapshot").
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (r *entitlementGrantRepository) ExpireLiveByConfigAndCustomer(
	ctx context.Context,
	entitlementConfigID string,
	customerID string,
	at time.Time,
) (int, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "expire_live", map[string]interface{}{
		"entitlement_config_id": entitlementConfigID,
		"customer_id":           customerID,
	})
	defer FinishSpan(span)

	// Transition any live-but-closed grant on this slot to expired. The partial
	// unique index would otherwise reject the next INSERT.
	n, err := r.client.Writer(ctx).EntitlementGrant.Update().
		Where(
			entitlementgrant.TenantID(types.GetTenantID(ctx)),
			entitlementgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
			entitlementgrant.EntitlementConfigID(entitlementConfigID),
			entitlementgrant.CustomerID(customerID),
			entitlementgrant.GrantStatusIn(types.LiveEntitlementGrantStatuses...),
			entitlementgrant.ValidToLTE(at),
		).
		SetGrantStatus(types.EntitlementGrantStatusExpired).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to expire stale entitlement grants").
			Mark(ierr.ErrDatabase)
	}
	return n, nil
}

func (r *entitlementGrantRepository) FindLastByConfigAndCustomer(
	ctx context.Context,
	entitlementConfigID string,
	customerID string,
) (*domainGrant.EntitlementGrant, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "find_last", map[string]interface{}{
		"entitlement_config_id": entitlementConfigID,
		"customer_id":           customerID,
	})
	defer FinishSpan(span)

	row, err := r.scoped(ctx).
		Where(
			entitlementgrant.EntitlementConfigID(entitlementConfigID),
			entitlementgrant.CustomerID(customerID),
		).
		Order(ent.Desc(entitlementgrant.FieldValidTo)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to look up last entitlement grant for slot").
			Mark(ierr.ErrDatabase)
	}
	return domainGrant.FromEnt(row), nil
}

func (r *entitlementGrantRepository) FindLiveByConfigAndCustomer(
	ctx context.Context,
	entitlementConfigID string,
	customerID string,
) (*domainGrant.EntitlementGrant, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "find_live", map[string]interface{}{
		"entitlement_config_id": entitlementConfigID,
		"customer_id":           customerID,
	})
	defer FinishSpan(span)

	row, err := r.scoped(ctx).
		Where(
			entitlementgrant.EntitlementConfigID(entitlementConfigID),
			entitlementgrant.CustomerID(customerID),
			entitlementgrant.GrantStatusIn(types.LiveEntitlementGrantStatuses...),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to look up live entitlement grant for slot").
			Mark(ierr.ErrDatabase)
	}
	return domainGrant.FromEnt(row), nil
}

// -----------------------------------------------------------------------------
// List / Count
// -----------------------------------------------------------------------------

func (r *entitlementGrantRepository) List(ctx context.Context, filter *types.EntitlementGrantFilter) ([]*domainGrant.EntitlementGrant, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if filter == nil {
		filter = types.NewNoLimitEntitlementGrantFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

	q := applyEntitlementGrantFilter(r.scoped(ctx), filter)
	if !filter.IsUnlimited() {
		q = q.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}

	rows, err := q.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list entitlement grants").
			Mark(ierr.ErrDatabase)
	}
	return domainGrant.FromEntList(rows), nil
}

func (r *entitlementGrantRepository) Count(ctx context.Context, filter *types.EntitlementGrantFilter) (int, error) {
	span := StartRepositorySpan(ctx, "entitlement_grant", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if filter == nil {
		filter = types.NewNoLimitEntitlementGrantFilter()
	}
	if err := filter.Validate(); err != nil {
		return 0, err
	}

	n, err := applyEntitlementGrantFilter(r.scoped(ctx), filter).Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count entitlement grants").
			Mark(ierr.ErrDatabase)
	}
	return n, nil
}

// -----------------------------------------------------------------------------
// Shared filter application. Centralised so List and Count agree on semantics.
// -----------------------------------------------------------------------------

func applyEntitlementGrantFilter(q *ent.EntitlementGrantQuery, f *types.EntitlementGrantFilter) *ent.EntitlementGrantQuery {
	preds := []predicate.EntitlementGrant{}

	if len(f.IDs) > 0 {
		preds = append(preds, entitlementgrant.IDIn(f.IDs...))
	}
	if len(f.EntitlementConfigIDs) > 0 {
		preds = append(preds, entitlementgrant.EntitlementConfigIDIn(f.EntitlementConfigIDs...))
	}
	if len(f.CustomerIDs) > 0 {
		preds = append(preds, entitlementgrant.CustomerIDIn(f.CustomerIDs...))
	}
	if len(f.SubscriptionIDs) > 0 {
		preds = append(preds, entitlementgrant.SubscriptionIDIn(f.SubscriptionIDs...))
	}
	if f.ScopeEntityType != nil {
		preds = append(preds, entitlementgrant.ScopeEntityTypeEQ(*f.ScopeEntityType))
	}
	if len(f.ScopeEntityIDs) > 0 {
		preds = append(preds, entitlementgrant.ScopeEntityIDIn(f.ScopeEntityIDs...))
	}
	if len(f.Statuses) > 0 {
		preds = append(preds, entitlementgrant.GrantStatusIn(f.Statuses...))
	}
	if f.Measure != nil {
		preds = append(preds, entitlementgrant.MeasureEQ(*f.Measure))
	}

	// Alert-path shorthand: "grant currently in window."
	if f.ValidAtOrAfter != nil {
		preds = append(preds, entitlementgrant.ValidToGT(*f.ValidAtOrAfter))
		preds = append(preds, entitlementgrant.ValidFromLTE(*f.ValidAtOrAfter))
	}

	// Billing-path shorthand: "grant window overlaps [cycleStart, cycleEnd)."
	// Two half-open intervals overlap iff a.start < b.end AND b.start < a.end.
	if f.ValidFromBefore != nil {
		preds = append(preds, entitlementgrant.ValidFromLT(*f.ValidFromBefore))
	}
	if f.ValidToAfter != nil {
		preds = append(preds, entitlementgrant.ValidToGT(*f.ValidToAfter))
	}

	// Row-level created_at range from TimeRangeFilter — separate from window predicates.
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			preds = append(preds, entitlementgrant.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			preds = append(preds, entitlementgrant.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	if len(preds) > 0 {
		q = q.Where(preds...)
	}
	return q
}

// defaultedGrantStatus mirrors defaultedGrantType (see entitlement.go): a zero
// value from a badly-constructed domain object lands as active at the DB level,
// which is the safe default for a freshly-opened grant.
func defaultedGrantStatus(s types.EntitlementGrantStatus) types.EntitlementGrantStatus {
	if s == "" {
		return types.EntitlementGrantStatusActive
	}
	return s
}

// defaultedScopeEntityType lands an unset scope on feature, which is the only
// shape Phase 1 writes. Ent's DB-level default handles the same case for
// legacy rows during the pending migration; centralising it here keeps the
// service layer from ever writing an empty string.
func defaultedScopeEntityType(t types.EntitlementGrantScopeEntityType) types.EntitlementGrantScopeEntityType {
	if t == "" {
		return types.EntitlementGrantScopeFeature
	}
	return t
}
