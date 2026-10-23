package pob

import (
	"context"
	"errors"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	entContract "github.com/flexprice/flexprice/ent/contract"
	entPOB "github.com/flexprice/flexprice/ent/performanceobligation"
	entPOBAssociation "github.com/flexprice/flexprice/ent/performanceobligationassociation"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	domainPOB "github.com/flexprice/flexprice/internal/domain/pob"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
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

func (r *repository) CreateWithAssociation(ctx context.Context, p *domainPOB.PerformanceObligation, a *domainPOB.Association) error {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "create_with_association", map[string]interface{}{
		"pob_id":      p.ID(),
		"entity_type": a.EntityType(),
		"entity_id":   a.EntityID(),
	})
	defer entRepo.FinishSpan(span)

	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		_, err := r.client.Writer(txCtx).PerformanceObligation.Create().
			SetID(p.ID()).
			SetTenantID(types.GetTenantID(txCtx)).
			SetEnvironmentID(types.GetEnvironmentID(txCtx)).
			SetStatus(string(p.Status())).
			SetContractID(p.ContractID()).
			SetName(p.Name()).
			SetRecognitionMethod(string(p.RecognitionMethod())).
			SetAmortizationMethod(string(p.AmortizationMethod())).
			SetServiceStart(p.ServiceStart()).
			SetNillableServiceEnd(p.ServiceEnd()).
			SetPobStatus(string(p.POBStatus())).
			SetCreatedBy(types.GetUserID(txCtx)).
			SetUpdatedBy(types.GetUserID(txCtx)).
			SetCreatedAt(p.CreatedAt()).
			SetUpdatedAt(p.UpdatedAt()).
			Save(txCtx)
		if err != nil {
			return err
		}
		return r.createAssociation(txCtx, domainPOB.NewAssociationBuilder(a).WithPOBID(p.ID()).Build())
	})
	if err != nil {
		entRepo.SetSpanError(span, err)
		return r.createError(err, a, "Failed to create performance obligation")
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

// CreateAssociation links another entity to an existing performance obligation.
func (r *repository) CreateAssociation(ctx context.Context, a *domainPOB.Association) error {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "create_association", map[string]interface{}{
		"pob_id":      a.POBID(),
		"entity_type": a.EntityType(),
		"entity_id":   a.EntityID(),
	})
	defer entRepo.FinishSpan(span)

	if err := r.createAssociation(ctx, a); err != nil {
		entRepo.SetSpanError(span, err)
		return r.createError(err, a, "Failed to create performance obligation association")
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) createAssociation(ctx context.Context, a *domainPOB.Association) error {
	_, err := r.client.Writer(ctx).PerformanceObligationAssociation.Create().
		SetID(a.ID()).
		SetTenantID(types.GetTenantID(ctx)).
		SetEnvironmentID(types.GetEnvironmentID(ctx)).
		SetStatus(string(a.Status())).
		SetPobID(a.POBID()).
		SetEntityType(string(a.EntityType())).
		SetEntityID(a.EntityID()).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetCreatedAt(a.CreatedAt()).
		SetUpdatedAt(a.CreatedAt()).
		Save(ctx)
	return err
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

// GetByLineItemID returns the performance obligation linked to the line item by a published association, in any status.
func (r *repository) GetByLineItemID(ctx context.Context, lineItemID string) (*domainPOB.PerformanceObligation, error) {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "get_by_line_item_id", map[string]interface{}{
		"line_item_id": lineItemID,
	})
	defer entRepo.FinishSpan(span)

	a, err := r.client.Reader(ctx).PerformanceObligationAssociation.Query().
		Where(
			entPOBAssociation.TenantID(types.GetTenantID(ctx)),
			entPOBAssociation.EnvironmentID(types.GetEnvironmentID(ctx)),
			entPOBAssociation.EntityType(string(types.RevRecEntityTypeSubscriptionLineItem)),
			entPOBAssociation.EntityID(lineItemID),
			entPOBAssociation.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, r.readError(err, "line_item_id", lineItemID)
	}

	entRepo.SetSpanSuccess(span)
	return r.GetByID(ctx, a.PobID)
}

// ListAssociations returns the performance obligation's published associations, oldest first.
func (r *repository) ListAssociations(ctx context.Context, pobID string) ([]*domainPOB.Association, error) {
	span := entRepo.StartRepositorySpan(ctx, "performance_obligation", "list_associations", map[string]interface{}{
		"pob_id": pobID,
	})
	defer entRepo.FinishSpan(span)

	associations, err := r.client.Reader(ctx).PerformanceObligationAssociation.Query().
		Where(
			entPOBAssociation.TenantID(types.GetTenantID(ctx)),
			entPOBAssociation.EnvironmentID(types.GetEnvironmentID(ctx)),
			entPOBAssociation.PobID(pobID),
			entPOBAssociation.Status(string(types.StatusPublished)),
		).
		Order(ent.Asc(entPOBAssociation.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list performance obligation associations").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return AssociationFromEntList(associations), nil
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
		SetName(p.Name()).
		SetRecognitionMethod(string(p.RecognitionMethod())).
		SetAmortizationMethod(string(p.AmortizationMethod())).
		SetServiceStart(p.ServiceStart()).
		SetPobStatus(string(p.POBStatus())).
		SetStatus(string(p.Status())).
		SetUpdatedAt(time.Now().UTC()).
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

// createError marks a duplicate published association as ErrAlreadyExists.
func (r *repository) createError(err error, a *domainPOB.Association, hint string) error {
	var pqErr *pq.Error
	if ent.IsConstraintError(err) && errors.As(err, &pqErr) &&
		pqErr.Constraint == schema.PerformanceObligationAssociationUniqueConstraint {
		return ierr.WithError(err).
			WithHint("Entity already belongs to a performance obligation").
			WithReportableDetails(map[string]any{
				"entity_type": a.EntityType(),
				"entity_id":   a.EntityID(),
			}).
			Mark(ierr.ErrAlreadyExists)
	}
	return ierr.WithError(err).
		WithHint(hint).
		Mark(ierr.ErrDatabase)
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
