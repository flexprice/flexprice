package pob

import (
	"context"

	"github.com/flexprice/flexprice/internal/clock"

	"entgo.io/ent/dialect/sql"
	"github.com/samber/lo"

	"github.com/flexprice/flexprice/ent"
	entContract "github.com/flexprice/flexprice/ent/contract"
	entPOB "github.com/flexprice/flexprice/ent/performanceobligation"
	"github.com/flexprice/flexprice/ent/predicate"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
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

func (r *repository) Create(ctx context.Context, p *domainPOB.PerformanceObligation) error {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "create", map[string]interface{}{
		"pob_id": p.ID(),
	})
	defer entRepo.FinishSpan(span)

	_, err := r.client.Writer(ctx).PerformanceObligation.Create().
		SetID(p.ID()).
		SetTenantID(types.GetTenantID(ctx)).
		SetEnvironmentID(types.GetEnvironmentID(ctx)).
		SetStatus(string(p.Status())).
		SetContractID(p.ContractID()).
		SetProductID(p.ProductID()).
		SetAllocatedAmount(p.AllocatedAmount()).
		SetName(p.Name()).
		SetRecognitionMethod(string(p.RecognitionMethod())).
		SetAmortizationMethod(string(p.AmortizationMethod())).
		SetServiceStart(p.ServiceStart()).
		SetNillableServiceEnd(p.ServiceEnd()).
		SetPobStatus(string(p.POBStatus())).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetCreatedAt(p.CreatedAt()).
		SetUpdatedAt(p.UpdatedAt()).
		Save(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create performance obligation").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) GetByID(ctx context.Context, id string) (*domainPOB.PerformanceObligation, error) {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "get", map[string]interface{}{
		"pob_id": id,
	})
	defer entRepo.FinishSpan(span)

	p, err := r.client.Reader(ctx).PerformanceObligation.Query().
		Where(
			entPOB.ID(id),
			entPOB.TenantID(types.GetTenantID(ctx)),
			entPOB.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, r.readError(err, "pob_id", id)
	}

	entRepo.SetSpanSuccess(span)
	return FromEnt(p), nil
}

func (r *repository) List(ctx context.Context, filter *Filter) ([]*domainPOB.PerformanceObligation, error) {
	if filter == nil {
		filter = NewFilter()
	}

	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "list", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, r.client.Reader(ctx).PerformanceObligation.Query())
	if err != nil {
		return nil, err
	}
	query = entRepo.ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	pobs, err := query.All(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list performance obligations").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return FromEntList(pobs), nil
}

func (r *repository) Count(ctx context.Context, filter *Filter) (int, error) {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "count", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query := entRepo.ApplyBaseFilters(ctx, r.client.Reader(ctx).PerformanceObligation.Query(), filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count performance obligations").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return count, nil
}

func (r *repository) Update(ctx context.Context, p *domainPOB.PerformanceObligation) error {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "update", map[string]interface{}{
		"pob_id": p.ID(),
	})
	defer entRepo.FinishSpan(span)

	update := r.client.Writer(ctx).PerformanceObligation.UpdateOneID(p.ID()).
		Where(
			entPOB.TenantID(types.GetTenantID(ctx)),
			entPOB.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetAllocatedAmount(p.AllocatedAmount()).
		SetName(p.Name()).
		SetRecognitionMethod(string(p.RecognitionMethod())).
		SetAmortizationMethod(string(p.AmortizationMethod())).
		SetServiceStart(p.ServiceStart()).
		SetPobStatus(string(p.POBStatus())).
		SetStatus(string(p.Status())).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx))
	if p.ServiceEnd() == nil {
		update.ClearServiceEnd()
	} else {
		update.SetServiceEnd(*p.ServiceEnd())
	}

	if _, err := update.Save(ctx); err != nil {
		entRepo.SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return r.readError(err, "pob_id", p.ID())
		}
		return ierr.WithError(err).
			WithHint("Failed to update performance obligation").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) readError(err error, key, value string) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).
			WithHintf("Performance obligation with %s %s was not found", key, value).
			WithReportableDetails(map[string]any{
				key: value,
			}).
			Mark(ierr.ErrNotFound)
	}
	return ierr.WithError(err).
		WithHintf("Failed to get performance obligation with %s %s", key, value).
		Mark(ierr.ErrDatabase)
}

type pobQuery = *ent.PerformanceObligationQuery

// queryOptions implements query options for performance obligation filtering and sorting
type queryOptions struct{}

func (o queryOptions) ApplyTenantFilter(ctx context.Context, query pobQuery) pobQuery {
	return query.Where(entPOB.TenantID(types.GetTenantID(ctx)))
}

func (o queryOptions) ApplyEnvironmentFilter(ctx context.Context, query pobQuery) pobQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entPOB.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o queryOptions) ApplyStatusFilter(query pobQuery, status string) pobQuery {
	if status == "" {
		return query.Where(entPOB.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(entPOB.Status(status))
}

func (o queryOptions) ApplySortFilter(query pobQuery, field string, order string) pobQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o queryOptions) ApplyPaginationFilter(query pobQuery, limit int, offset int) pobQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o queryOptions) GetFieldName(field string) string {
	if entPOB.ValidColumn(field) {
		return field
	}
	return ""
}

func (o queryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in performance obligation query", st).
			WithHintf("Unknown field name '%s' in performance obligation query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o queryOptions) applyEntityQueryOptions(ctx context.Context, f *Filter, query pobQuery) (pobQuery, error) {
	if f == nil {
		return query, nil
	}

	if len(f.PerformanceObligationIDs) > 0 {
		query = query.Where(entPOB.IDIn(f.PerformanceObligationIDs...))
	}
	if len(f.ContractIDs) > 0 {
		query = query.Where(entPOB.ContractIDIn(f.ContractIDs...))
	}
	if len(f.POBStatuses) > 0 {
		query = query.Where(entPOB.PobStatusIn(lo.Map(f.POBStatuses, func(s types.PerformanceObligationStatus, _ int) string {
			return string(s)
		})...))
	}
	if len(f.RecognitionMethods) > 0 {
		query = query.Where(entPOB.RecognitionMethodIn(lo.Map(f.RecognitionMethods, func(m types.RecognitionMethod, _ int) string {
			return string(m)
		})...))
	}
	if len(f.CustomerIDs) > 0 {
		query = query.Where(customersPredicate(ctx, f.CustomerIDs))
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(entPOB.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(entPOB.CreatedAtLTE(*f.EndTime))
		}
	}

	var err error
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[pobQuery, predicate.PerformanceObligation](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.PerformanceObligation { return predicate.PerformanceObligation(p) },
		)
		if err != nil {
			return nil, err
		}
	}
	if f.Sort != nil {
		query, err = dsl.ApplySorts[pobQuery, entPOB.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) entPOB.OrderOption { return entPOB.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

// customersPredicate matches performance obligations whose contract belongs to any of the customers.
func customersPredicate(ctx context.Context, customerIDs []string) predicate.PerformanceObligation {
	return func(s *sql.Selector) {
		t := sql.Table(entContract.Table)
		s.Where(sql.In(s.C(entPOB.FieldContractID),
			sql.Select(t.C(entContract.FieldID)).
				From(t).
				Where(sql.And(
					sql.EQ(t.C(entContract.FieldTenantID), types.GetTenantID(ctx)),
					sql.EQ(t.C(entContract.FieldEnvironmentID), types.GetEnvironmentID(ctx)),
					sql.In(t.C(entContract.FieldCustomerID), lo.ToAnySlice(customerIDs)...),
				)),
		))
	}
}
