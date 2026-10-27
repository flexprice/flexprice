package product

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/predicate"
	entProduct "github.com/flexprice/flexprice/ent/product"
	"github.com/flexprice/flexprice/ent/schema"
	domainProduct "github.com/flexprice/flexprice/internal/domain/product"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
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

func (r *repository) Create(ctx context.Context, p *domainProduct.Product) error {
	span := entRepo.StartRepositorySpan(ctx, "product", "create", map[string]interface{}{
		"product_id": p.ID(),
		"lookup_key": p.LookupKey(),
	})
	defer entRepo.FinishSpan(span)

	_, err := r.client.Writer(ctx).Product.Create().
		SetID(p.ID()).
		SetTenantID(types.GetTenantID(ctx)).
		SetEnvironmentID(types.GetEnvironmentID(ctx)).
		SetStatus(string(p.Status())).
		SetName(p.Name()).
		SetDescription(p.Description()).
		SetLookupKey(p.LookupKey()).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetCreatedAt(p.CreatedAt()).
		SetUpdatedAt(p.UpdatedAt()).
		Save(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		var pqErr *pq.Error
		if ent.IsConstraintError(err) && errors.As(err, &pqErr) &&
			pqErr.Constraint == schema.ProductTenantIDEnvironmentIDLookupKeyConstraint {
			return ierr.WithError(err).
				WithHint("Product with same lookup key already exists").
				WithReportableDetails(map[string]any{
					"lookup_key": p.LookupKey(),
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create product").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) GetByID(ctx context.Context, id string) (*domainProduct.Product, error) {
	span := entRepo.StartRepositorySpan(ctx, "product", "get", map[string]interface{}{
		"product_id": id,
	})
	defer entRepo.FinishSpan(span)

	p, err := r.client.Reader(ctx).Product.Query().
		Where(
			entProduct.ID(id),
			entProduct.TenantID(types.GetTenantID(ctx)),
			entProduct.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Product with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"product_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get product with ID %s", id).
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return FromEnt(p), nil
}

func (r *repository) List(ctx context.Context, filter *Filter) ([]*domainProduct.Product, error) {
	if filter == nil {
		filter = NewFilter()
	}

	span := entRepo.StartRepositorySpan(ctx, "product", "list", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, r.client.Reader(ctx).Product.Query())
	if err != nil {
		return nil, err
	}
	query = entRepo.ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	products, err := query.All(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list products").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return FromEntList(products), nil
}

func (r *repository) Count(ctx context.Context, filter *Filter) (int, error) {
	span := entRepo.StartRepositorySpan(ctx, "product", "count", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query := entRepo.ApplyBaseFilters(ctx, r.client.Reader(ctx).Product.Query(), filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count products").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return count, nil
}

func (r *repository) Update(ctx context.Context, p *domainProduct.Product) error {
	span := entRepo.StartRepositorySpan(ctx, "product", "update", map[string]interface{}{
		"product_id": p.ID(),
	})
	defer entRepo.FinishSpan(span)

	_, err := r.client.Writer(ctx).Product.UpdateOneID(p.ID()).
		Where(
			entProduct.TenantID(types.GetTenantID(ctx)),
			entProduct.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(p.Name()).
		SetDescription(p.Description()).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return r.writeError(err, p.ID(), "Failed to update product")
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) Delete(ctx context.Context, id string) error {
	span := entRepo.StartRepositorySpan(ctx, "product", "delete", map[string]interface{}{
		"product_id": id,
	})
	defer entRepo.FinishSpan(span)

	_, err := r.client.Writer(ctx).Product.UpdateOneID(id).
		Where(
			entProduct.TenantID(types.GetTenantID(ctx)),
			entProduct.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return r.writeError(err, id, "Failed to delete product")
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) writeError(err error, id, hint string) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).
			WithHintf("Product with ID %s was not found", id).
			WithReportableDetails(map[string]any{
				"product_id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return ierr.WithError(err).
		WithHint(hint).
		Mark(ierr.ErrDatabase)
}

type productQuery = *ent.ProductQuery

// queryOptions implements query options for product filtering and sorting
type queryOptions struct{}

func (o queryOptions) ApplyTenantFilter(ctx context.Context, query productQuery) productQuery {
	return query.Where(entProduct.TenantID(types.GetTenantID(ctx)))
}

func (o queryOptions) ApplyEnvironmentFilter(ctx context.Context, query productQuery) productQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entProduct.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o queryOptions) ApplyStatusFilter(query productQuery, status string) productQuery {
	if status == "" {
		return query.Where(entProduct.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(entProduct.Status(status))
}

func (o queryOptions) ApplySortFilter(query productQuery, field string, order string) productQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o queryOptions) ApplyPaginationFilter(query productQuery, limit int, offset int) productQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o queryOptions) GetFieldName(field string) string {
	if entProduct.ValidColumn(field) {
		return field
	}
	return ""
}

func (o queryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in product query", st).
			WithHintf("Unknown field name '%s' in product query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o queryOptions) applyEntityQueryOptions(_ context.Context, f *Filter, query productQuery) (productQuery, error) {
	if f == nil {
		return query, nil
	}

	if len(f.ProductIDs) > 0 {
		query = query.Where(entProduct.IDIn(f.ProductIDs...))
	}
	if len(f.LookupKeys) > 0 {
		query = query.Where(entProduct.LookupKeyIn(f.LookupKeys...))
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(entProduct.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(entProduct.CreatedAtLTE(*f.EndTime))
		}
	}

	var err error
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[productQuery, predicate.Product](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Product { return predicate.Product(p) },
		)
		if err != nil {
			return nil, err
		}
	}
	if f.Sort != nil {
		query, err = dsl.ApplySorts[productQuery, entProduct.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) entProduct.OrderOption { return entProduct.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}
