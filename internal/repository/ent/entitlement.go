package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/entitlement"
	domainEntitlement "github.com/flexprice/flexprice/internal/domain/entitlement"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type entitlementRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts EntitlementQueryOptions
}

func NewEntitlementRepository(client postgres.IClient, log *logger.Logger) domainEntitlement.Repository {
	return &entitlementRepository{
		client:    client,
		log:       log,
		queryOpts: EntitlementQueryOptions{},
	}
}

func (r *entitlementRepository) Create(ctx context.Context, e *domainEntitlement.Entitlement) (*domainEntitlement.Entitlement, error) {
	if e == nil {
		return nil, ierr.NewError("entitlement cannot be nil").Mark(ierr.ErrValidation)
	}

	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if e.EnvironmentID == "" {
		e.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	result, err := client.Entitlement.Create().
		SetID(e.ID).
		SetPlanID(e.PlanID).
		SetFeatureID(e.FeatureID).
		SetFeatureType(string(e.FeatureType)).
		SetIsEnabled(e.IsEnabled).
		SetNillableUsageLimit(e.UsageLimit).
		SetUsageResetPeriod(string(e.UsageResetPeriod)).
		SetIsSoftLimit(e.IsSoftLimit).
		SetStaticValue(e.StaticValue).
		SetTenantID(e.TenantID).
		SetStatus(string(e.Status)).
		SetCreatedAt(e.CreatedAt).
		SetUpdatedAt(e.UpdatedAt).
		SetCreatedBy(e.CreatedBy).
		SetUpdatedBy(e.UpdatedBy).
		SetEnvironmentID(e.EnvironmentID).
		Save(ctx)

	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, ierr.WithError(err).
				WithHint("An entitlement with this plan and feature already exists").
				WithReportableDetails(map[string]interface{}{
					"plan_id":    e.PlanID,
					"feature_id": e.FeatureID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to create entitlement").
			WithReportableDetails(map[string]interface{}{
				"plan_id":    e.PlanID,
				"feature_id": e.FeatureID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEnt(result), nil
}

func (r *entitlementRepository) Get(ctx context.Context, id string) (*domainEntitlement.Entitlement, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting entitlement",
		"entitlement_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	result, err := client.Entitlement.Query().
		Where(
			entitlement.ID(id),
			entitlement.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Entitlement not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get entitlement").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEnt(result), nil
}

func (r *entitlementRepository) List(ctx context.Context, filter *types.EntitlementFilter) ([]*domainEntitlement.Entitlement, error) {
	if filter == nil {
		filter = &types.EntitlementFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Querier(ctx)
	query := client.Entitlement.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	results, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list entitlements").
			WithReportableDetails(map[string]interface{}{
				"plan_ids":     filter.PlanIDs,
				"feature_ids":  filter.FeatureIDs,
				"feature_type": filter.FeatureType,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEntList(results), nil
}

func (r *entitlementRepository) Count(ctx context.Context, filter *types.EntitlementFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Entitlement.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count entitlements").
			WithReportableDetails(map[string]interface{}{
				"plan_ids":     filter.PlanIDs,
				"feature_ids":  filter.FeatureIDs,
				"feature_type": filter.FeatureType,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *entitlementRepository) ListAll(ctx context.Context, filter *types.EntitlementFilter) ([]*domainEntitlement.Entitlement, error) {
	if filter == nil {
		filter = types.NewNoLimitEntitlementFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	if err := filter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	return r.List(ctx, filter)
}

func (r *entitlementRepository) Update(ctx context.Context, e *domainEntitlement.Entitlement) (*domainEntitlement.Entitlement, error) {
	if e == nil {
		return nil, ierr.NewError("entitlement cannot be nil").Mark(ierr.ErrValidation)
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("updating entitlement",
		"entitlement_id", e.ID,
		"tenant_id", e.TenantID,
	)

	result, err := client.Entitlement.UpdateOneID(e.ID).
		Where(entitlement.TenantID(e.TenantID)).
		SetPlanID(e.PlanID).
		SetFeatureID(e.FeatureID).
		SetFeatureType(string(e.FeatureType)).
		SetIsEnabled(e.IsEnabled).
		SetIsSoftLimit(e.IsSoftLimit).
		SetNillableUsageLimit(e.UsageLimit).
		SetUsageResetPeriod(string(e.UsageResetPeriod)).
		SetStaticValue(e.StaticValue).
		SetStatus(string(e.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Entitlement not found").
				WithReportableDetails(map[string]interface{}{"id": e.ID}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to update entitlement").
			WithReportableDetails(map[string]interface{}{"id": e.ID}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEnt(result), nil
}

func (r *entitlementRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting entitlement",
		"entitlement_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Entitlement.Update().
		Where(
			entitlement.ID(id),
			entitlement.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Entitlement not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete entitlement").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *entitlementRepository) CreateBulk(ctx context.Context, entitlements []*domainEntitlement.Entitlement) ([]*domainEntitlement.Entitlement, error) {
	if len(entitlements) == 0 {
		return []*domainEntitlement.Entitlement{}, nil
	}

	client := r.client.Querier(ctx)
	builders := make([]*ent.EntitlementCreate, len(entitlements))

	// Get environment ID from context
	environmentID := types.GetEnvironmentID(ctx)

	for i, e := range entitlements {
		// Set environment ID from context if not already set
		if e.EnvironmentID == "" {
			e.EnvironmentID = environmentID
		}

		builders[i] = client.Entitlement.Create().
			SetID(e.ID).
			SetPlanID(e.PlanID).
			SetFeatureID(e.FeatureID).
			SetFeatureType(string(e.FeatureType)).
			SetIsEnabled(e.IsEnabled).
			SetNillableUsageLimit(e.UsageLimit).
			SetUsageResetPeriod(string(e.UsageResetPeriod)).
			SetIsSoftLimit(e.IsSoftLimit).
			SetStaticValue(e.StaticValue).
			SetTenantID(e.TenantID).
			SetStatus(string(e.Status)).
			SetCreatedAt(e.CreatedAt).
			SetUpdatedAt(e.UpdatedAt).
			SetCreatedBy(e.CreatedBy).
			SetUpdatedBy(e.UpdatedBy).
			SetEnvironmentID(e.EnvironmentID)
	}

	results, err := client.Entitlement.CreateBulk(builders...).Save(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create entitlements in bulk").
			WithReportableDetails(map[string]interface{}{
				"count": len(entitlements),
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEntList(results), nil
}

func (r *entitlementRepository) DeleteBulk(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	r.log.Debugw("deleting entitlements in bulk", "count", len(ids))

	_, err := r.client.Querier(ctx).Entitlement.Update().
		Where(
			entitlement.IDIn(ids...),
			entitlement.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete entitlements in bulk").
			WithReportableDetails(map[string]interface{}{
				"count": len(ids),
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// EntitlementQuery type alias for better readability
type EntitlementQuery = *ent.EntitlementQuery

// EntitlementQueryOptions implements BaseQueryOptions for entitlement queries
type EntitlementQueryOptions struct{}

func (o EntitlementQueryOptions) ApplyTenantFilter(ctx context.Context, query EntitlementQuery) EntitlementQuery {
	return query.Where(entitlement.TenantID(types.GetTenantID(ctx)))
}

func (o EntitlementQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query EntitlementQuery) EntitlementQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entitlement.EnvironmentID(environmentID))
	}
	return query
}

func (o EntitlementQueryOptions) ApplyStatusFilter(query EntitlementQuery, status string) EntitlementQuery {
	if status == "" {
		return query.Where(entitlement.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(entitlement.Status(status))
}

func (o EntitlementQueryOptions) ApplySortFilter(query EntitlementQuery, field string, order string) EntitlementQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o EntitlementQueryOptions) ApplyPaginationFilter(query EntitlementQuery, limit int, offset int) EntitlementQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o EntitlementQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return entitlement.FieldCreatedAt
	case "updated_at":
		return entitlement.FieldUpdatedAt
	default:
		return field
	}
}

func (o EntitlementQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.EntitlementFilter, query EntitlementQuery) EntitlementQuery {
	if f == nil {
		return query
	}

	// Apply plan ID filter if specified
	if len(f.PlanIDs) > 0 {
		query = query.Where(entitlement.PlanIDIn(f.PlanIDs...))
	}

	// Apply feature IDs filter if specified
	if len(f.FeatureIDs) > 0 {
		query = query.Where(entitlement.FeatureIDIn(f.FeatureIDs...))
	}

	// Apply feature type filter if specified
	if f.FeatureType != nil {
		query = query.Where(entitlement.FeatureType(string(*f.FeatureType)))
	}

	// Apply is_enabled filter if specified
	if f.IsEnabled != nil {
		query = query.Where(entitlement.IsEnabled(*f.IsEnabled))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(entitlement.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(entitlement.CreatedAtLTE(*f.EndTime))
		}
	}

	return query
}
