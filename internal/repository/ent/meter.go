package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/meter"
	domainMeter "github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type meterRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts MeterQueryOptions
}

func NewMeterRepository(client postgres.IClient, logger *logger.Logger) domainMeter.Repository {
	return &meterRepository{
		client:    client,
		logger:    logger,
		queryOpts: MeterQueryOptions{},
	}
}

func (r *meterRepository) CreateMeter(ctx context.Context, m *domainMeter.Meter) error {
	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if m.EnvironmentID == "" {
		m.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	meter, err := client.Meter.Create().
		SetID(m.ID).
		SetTenantID(m.TenantID).
		SetEventName(m.EventName).
		SetName(m.Name).
		SetAggregation(m.ToEntAggregation()).
		SetFilters(m.ToEntFilters()).
		SetResetUsage(string(m.ResetUsage)).
		SetStatus(string(m.Status)).
		SetCreatedAt(m.CreatedAt).
		SetUpdatedAt(m.UpdatedAt).
		SetCreatedBy(m.CreatedBy).
		SetUpdatedBy(m.UpdatedBy).
		SetEnvironmentID(m.EnvironmentID).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithMessage("failed to create meter").
			WithHint("Failed to create meter").
			WithReportableDetails(map[string]any{
				"meter_id": m.ID,
				"tenant_id": m.TenantID,
			}).
			Mark(ierr.ErrDatabase)
	}

	*m = *domainMeter.FromEnt(meter)
	return nil
}

func (r *meterRepository) GetMeter(ctx context.Context, id string) (*domainMeter.Meter, error) {
	client := r.client.Querier(ctx)

	m, err := client.Meter.Query().
		Where(
			meter.ID(id),
			meter.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithMessage("meter not found").
				WithHint("Meter not found").
				WithReportableDetails(map[string]any{
					"meter_id": id,
					"tenant_id": types.GetTenantID(ctx),
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithMessage("failed to get meter").
			WithHint("Failed to retrieve meter").
			WithReportableDetails(map[string]any{
				"meter_id": id,
				"tenant_id": types.GetTenantID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainMeter.FromEnt(m), nil
}

func (r *meterRepository) List(ctx context.Context, filter *types.MeterFilter) ([]*domainMeter.Meter, error) {
	client := r.client.Querier(ctx)
	query := client.Meter.Query()

	// Apply base filters
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Execute query
	meters, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithMessage("failed to list meters").
			WithHint("Failed to retrieve meters").
			WithReportableDetails(map[string]any{
				"tenant_id": types.GetTenantID(ctx),
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models
	result := make([]*domainMeter.Meter, len(meters))
	for i, m := range meters {
		result[i] = domainMeter.FromEnt(m)
	}

	return result, nil
}

func (r *meterRepository) ListAll(ctx context.Context, filter *types.MeterFilter) ([]*domainMeter.Meter, error) {
	if filter == nil {
		filter = types.NewNoLimitMeterFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	return r.List(ctx, filter)
}

func (r *meterRepository) Count(ctx context.Context, filter *types.MeterFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Meter.Query()

	// Apply base filters
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithMessage("failed to count meters").
			WithHint("Failed to count meters").
			WithReportableDetails(map[string]any{
				"tenant_id": types.GetTenantID(ctx),
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *meterRepository) DisableMeter(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	_, err := client.Meter.Update().
		Where(
			meter.ID(id),
			meter.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithMessage("meter not found").
				WithHint("Meter not found").
				WithReportableDetails(map[string]any{
					"meter_id": id,
					"tenant_id": types.GetTenantID(ctx),
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithMessage("failed to disable meter").
			WithHint("Failed to disable meter").
			WithReportableDetails(map[string]any{
				"meter_id": id,
				"tenant_id": types.GetTenantID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *meterRepository) UpdateMeter(ctx context.Context, id string, filters []domainMeter.Filter) error {
	client := r.client.Querier(ctx)

	r.logger.Debugw("updating meter",
		"meter_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	m := &domainMeter.Meter{Filters: filters}
	_, err := client.Meter.Update().
		Where(
			meter.ID(id),
			meter.TenantID(types.GetTenantID(ctx)),
		).
		SetFilters(m.ToEntFilters()).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithMessage("meter not found").
				WithHint("Meter not found").
				WithReportableDetails(map[string]any{
					"meter_id": id,
					"tenant_id": types.GetTenantID(ctx),
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithMessage("failed to update meter").
			WithHint("Failed to update meter").
			WithReportableDetails(map[string]any{
				"meter_id": id,
				"tenant_id": types.GetTenantID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// Query option methods
type MeterQuery = *ent.MeterQuery

// MeterQueryOptions implements BaseQueryOptions for meter queries
type MeterQueryOptions struct{}

func (o MeterQueryOptions) ApplyTenantFilter(ctx context.Context, query MeterQuery) MeterQuery {
	return query.Where(meter.TenantID(types.GetTenantID(ctx)))
}

func (o MeterQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query MeterQuery) MeterQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(meter.EnvironmentID(environmentID))
	}
	return query
}

func (o MeterQueryOptions) ApplyStatusFilter(query MeterQuery, status string) MeterQuery {
	if status == "" {
		return query.Where(meter.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(meter.Status(status))
}

func (o MeterQueryOptions) ApplySortFilter(query MeterQuery, field string, order string) MeterQuery {
	orderFunc := ent.Desc
	if order == types.OrderAsc {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o MeterQueryOptions) ApplyPaginationFilter(query MeterQuery, limit int, offset int) MeterQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o MeterQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return meter.FieldCreatedAt
	case "updated_at":
		return meter.FieldUpdatedAt
	default:
		return field
	}
}

func (o MeterQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.MeterFilter, query MeterQuery) MeterQuery {
	if f == nil {
		return query
	}

	if f.EventName != "" {
		query = query.Where(meter.EventName(string(f.EventName)))
	}

	if len(f.MeterIDs) > 0 {
		query = query.Where(meter.IDIn(f.MeterIDs...))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(meter.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(meter.CreatedAtLTE(*f.EndTime))
		}
	}

	return query
}
