package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/meter"
	domainMeter "github.com/flexprice/flexprice/internal/domain/meter"
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
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create meter: %w", err)
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
			return nil, fmt.Errorf("meter not found")
		}
		return nil, fmt.Errorf("failed to get meter: %w", err)
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
		return nil, fmt.Errorf("failed to list meters: %w", err)
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

	return query.Count(ctx)
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
			return fmt.Errorf("meter not found")
		}
		return fmt.Errorf("failed to disable meter: %w", err)
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
			return fmt.Errorf("meter not found")
		}
		return fmt.Errorf("failed to update meter: %w", err)
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

func (o MeterQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.MeterFilter, query MeterQuery) MeterQuery {
	if f == nil {
		return query
	}

	if f.EventName != "" {
		query = query.Where(meter.EventName(string(f.EventName)))
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
