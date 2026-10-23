package accountingperiod

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entAccountingPeriod "github.com/flexprice/flexprice/ent/accountingperiod"
	"github.com/flexprice/flexprice/ent/predicate"
	domainAccountingPeriod "github.com/flexprice/flexprice/internal/domain/accountingperiod"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type repository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts queryOptions
}

func NewRepository(client postgres.IClient, log *logger.Logger) Repository {
	return &repository{
		client:    client,
		log:       log,
		queryOpts: queryOptions{},
	}
}

// GetOrCreate inserts the period unless a published one exists, then reads it back, so concurrent
// callers converge on one row. Raw SQL: ent cannot express a partial-index ON CONFLICT.
func (r *repository) GetOrCreate(ctx context.Context, periodStart time.Time) (*domainAccountingPeriod.AccountingPeriod, error) {
	span := entRepo.StartRepositorySpan(ctx, "accounting_period", "get_or_create", map[string]interface{}{
		"period_start": periodStart,
	})
	defer entRepo.FinishSpan(span)

	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	now, userID := time.Now().UTC(), types.GetUserID(ctx)
	_, err := r.client.Writer(ctx).ExecContext(ctx, `
		INSERT INTO accounting_periods
			(id, tenant_id, environment_id, period_start, period_status, status, created_at, created_by, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7, $8)
		ON CONFLICT (tenant_id, environment_id, period_start) WHERE status = 'published'
		DO NOTHING`,
		types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ACCOUNTING_PERIOD), tenantID, environmentID, periodStart,
		string(types.AccountingPeriodStatusOpen), string(types.StatusPublished), now, userID,
	)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to create accounting period").
			Mark(ierr.ErrDatabase)
	}

	p, err := r.client.Writer(ctx).AccountingPeriod.Query().
		Where(
			entAccountingPeriod.TenantID(tenantID),
			entAccountingPeriod.EnvironmentID(environmentID),
			entAccountingPeriod.PeriodStart(periodStart),
			entAccountingPeriod.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get accounting period").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return FromEnt(p), nil
}

func (r *repository) List(ctx context.Context, filter *Filter) ([]*domainAccountingPeriod.AccountingPeriod, error) {
	if filter == nil {
		filter = NewFilter()
	}

	span := entRepo.StartRepositorySpan(ctx, "accounting_period", "list", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, r.client.Reader(ctx).AccountingPeriod.Query())
	if err != nil {
		return nil, err
	}
	query = entRepo.ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	periods, err := query.All(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list accounting periods").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return FromEntList(periods), nil
}

func (r *repository) Count(ctx context.Context, filter *Filter) (int, error) {
	span := entRepo.StartRepositorySpan(ctx, "accounting_period", "count", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query := entRepo.ApplyBaseFilters(ctx, r.client.Reader(ctx).AccountingPeriod.Query(), filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count accounting periods").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return count, nil
}

type accountingPeriodQuery = *ent.AccountingPeriodQuery

// queryOptions implements query options for accounting period filtering and sorting
type queryOptions struct{}

func (o queryOptions) ApplyTenantFilter(ctx context.Context, query accountingPeriodQuery) accountingPeriodQuery {
	return query.Where(entAccountingPeriod.TenantID(types.GetTenantID(ctx)))
}

func (o queryOptions) ApplyEnvironmentFilter(ctx context.Context, query accountingPeriodQuery) accountingPeriodQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entAccountingPeriod.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o queryOptions) ApplyStatusFilter(query accountingPeriodQuery, status string) accountingPeriodQuery {
	if status == "" {
		return query.Where(entAccountingPeriod.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(entAccountingPeriod.Status(status))
}

func (o queryOptions) ApplySortFilter(query accountingPeriodQuery, field string, order string) accountingPeriodQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o queryOptions) ApplyPaginationFilter(query accountingPeriodQuery, limit int, offset int) accountingPeriodQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o queryOptions) GetFieldName(field string) string {
	if entAccountingPeriod.ValidColumn(field) {
		return field
	}
	return ""
}

func (o queryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in accounting period query", st).
			WithHintf("Unknown field name '%s' in accounting period query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o queryOptions) applyEntityQueryOptions(_ context.Context, f *Filter, query accountingPeriodQuery) (accountingPeriodQuery, error) {
	if f == nil {
		return query, nil
	}

	if len(f.AccountingPeriodIDs) > 0 {
		query = query.Where(entAccountingPeriod.IDIn(f.AccountingPeriodIDs...))
	}
	if len(f.PeriodStatuses) > 0 {
		query = query.Where(entAccountingPeriod.PeriodStatusIn(lo.Map(f.PeriodStatuses, func(s types.AccountingPeriodStatus, _ int) string {
			return string(s)
		})...))
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(entAccountingPeriod.PeriodStartGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(entAccountingPeriod.PeriodStartLT(*f.EndTime))
		}
	}

	var err error
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[accountingPeriodQuery, predicate.AccountingPeriod](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.AccountingPeriod { return predicate.AccountingPeriod(p) },
		)
		if err != nil {
			return nil, err
		}
	}
	if f.Sort != nil {
		query, err = dsl.ApplySorts[accountingPeriodQuery, entAccountingPeriod.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) entAccountingPeriod.OrderOption { return entAccountingPeriod.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}
