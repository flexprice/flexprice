package contract

import (
	"context"
	"errors"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	entContract "github.com/flexprice/flexprice/ent/contract"
	entContractAssociation "github.com/flexprice/flexprice/ent/contractassociation"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	domainContract "github.com/flexprice/flexprice/internal/domain/contract"
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

func (r *repository) CreateWithAssociation(ctx context.Context, c *domainContract.Contract, a *domainContract.Association) error {
	span := entRepo.StartRepositorySpan(ctx, "contract", "create_with_association", map[string]interface{}{
		"contract_id": c.ID(),
		"entity_type": a.EntityType(),
		"entity_id":   a.EntityID(),
	})
	defer entRepo.FinishSpan(span)

	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		_, err := r.client.Writer(txCtx).Contract.Create().
			SetID(c.ID()).
			SetTenantID(types.GetTenantID(txCtx)).
			SetEnvironmentID(types.GetEnvironmentID(txCtx)).
			SetStatus(string(c.Status())).
			SetCustomerID(c.CustomerID()).
			SetCurrency(c.Currency()).
			SetNillableTermStart(c.TermStart()).
			SetNillableTermEnd(c.TermEnd()).
			SetNillableEndedAt(c.EndedAt()).
			SetContractStatus(string(c.ContractStatus())).
			SetCreatedBy(types.GetUserID(txCtx)).
			SetUpdatedBy(types.GetUserID(txCtx)).
			SetCreatedAt(c.CreatedAt()).
			SetUpdatedAt(c.UpdatedAt()).
			Save(txCtx)
		if err != nil {
			return err
		}

		_, err = r.client.Writer(txCtx).ContractAssociation.Create().
			SetID(a.ID()).
			SetTenantID(types.GetTenantID(txCtx)).
			SetEnvironmentID(types.GetEnvironmentID(txCtx)).
			SetStatus(string(a.Status())).
			SetContractID(c.ID()).
			SetEntityType(string(a.EntityType())).
			SetEntityID(a.EntityID()).
			SetCreatedBy(types.GetUserID(txCtx)).
			SetUpdatedBy(types.GetUserID(txCtx)).
			SetCreatedAt(a.CreatedAt()).
			SetUpdatedAt(a.CreatedAt()).
			Save(txCtx)
		return err
	})
	if err != nil {
		entRepo.SetSpanError(span, err)
		var pqErr *pq.Error
		if ent.IsConstraintError(err) && errors.As(err, &pqErr) &&
			pqErr.Constraint == schema.ContractAssociationUniqueConstraint {
			return ierr.WithError(err).
				WithHint("Entity already belongs to a contract").
				WithReportableDetails(map[string]any{
					"entity_type": a.EntityType(),
					"entity_id":   a.EntityID(),
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create contract").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) GetByID(ctx context.Context, id string) (*domainContract.Contract, error) {
	span := entRepo.StartRepositorySpan(ctx, "contract", "get", map[string]interface{}{
		"contract_id": id,
	})
	defer entRepo.FinishSpan(span)

	c, err := r.client.Reader(ctx).Contract.Query().
		Where(
			entContract.ID(id),
			entContract.TenantID(types.GetTenantID(ctx)),
			entContract.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, r.readError(err, "contract_id", id)
	}

	entRepo.SetSpanSuccess(span)
	return FromEnt(c), nil
}

// GetBySubscriptionID returns the contract linked to the subscription by a published association, in any status.
func (r *repository) GetBySubscriptionID(ctx context.Context, subscriptionID string) (*domainContract.Contract, error) {
	span := entRepo.StartRepositorySpan(ctx, "contract", "get_by_subscription_id", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer entRepo.FinishSpan(span)

	a, err := r.client.Reader(ctx).ContractAssociation.Query().
		Where(
			entContractAssociation.TenantID(types.GetTenantID(ctx)),
			entContractAssociation.EnvironmentID(types.GetEnvironmentID(ctx)),
			entContractAssociation.EntityType(string(types.RevRecEntityTypeSubscription)),
			entContractAssociation.EntityID(subscriptionID),
			entContractAssociation.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, r.readError(err, "subscription_id", subscriptionID)
	}

	entRepo.SetSpanSuccess(span)
	return r.GetByID(ctx, a.ContractID)
}

// ListAssociations returns the contract's published associations, oldest first.
func (r *repository) ListAssociations(ctx context.Context, contractID string) ([]*domainContract.Association, error) {
	span := entRepo.StartRepositorySpan(ctx, "contract", "list_associations", map[string]interface{}{
		"contract_id": contractID,
	})
	defer entRepo.FinishSpan(span)

	associations, err := r.client.Reader(ctx).ContractAssociation.Query().
		Where(
			entContractAssociation.TenantID(types.GetTenantID(ctx)),
			entContractAssociation.EnvironmentID(types.GetEnvironmentID(ctx)),
			entContractAssociation.ContractID(contractID),
			entContractAssociation.Status(string(types.StatusPublished)),
		).
		Order(ent.Asc(entContractAssociation.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list contract associations").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return AssociationFromEntList(associations), nil
}

func (r *repository) List(ctx context.Context, filter *Filter) ([]*domainContract.Contract, error) {
	if filter == nil {
		filter = NewFilter()
	}

	span := entRepo.StartRepositorySpan(ctx, "contract", "list", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, r.client.Reader(ctx).Contract.Query())
	if err != nil {
		return nil, err
	}
	query = entRepo.ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	contracts, err := query.All(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list contracts").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return FromEntList(contracts), nil
}

func (r *repository) Count(ctx context.Context, filter *Filter) (int, error) {
	span := entRepo.StartRepositorySpan(ctx, "contract", "count", map[string]interface{}{
		"filter": filter,
	})
	defer entRepo.FinishSpan(span)

	query := entRepo.ApplyBaseFilters(ctx, r.client.Reader(ctx).Contract.Query(), filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		entRepo.SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count contracts").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return count, nil
}

func (r *repository) Update(ctx context.Context, c *domainContract.Contract) error {
	span := entRepo.StartRepositorySpan(ctx, "contract", "update", map[string]interface{}{
		"contract_id": c.ID(),
	})
	defer entRepo.FinishSpan(span)

	update := r.client.Writer(ctx).Contract.UpdateOneID(c.ID()).
		Where(
			entContract.TenantID(types.GetTenantID(ctx)),
			entContract.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetContractStatus(string(c.ContractStatus())).
		SetStatus(string(c.Status())).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))
	if c.TermStart() == nil {
		update.ClearTermStart()
	} else {
		update.SetTermStart(*c.TermStart())
	}
	if c.TermEnd() == nil {
		update.ClearTermEnd()
	} else {
		update.SetTermEnd(*c.TermEnd())
	}
	if c.EndedAt() == nil {
		update.ClearEndedAt()
	} else {
		update.SetEndedAt(*c.EndedAt())
	}

	if _, err := update.Save(ctx); err != nil {
		entRepo.SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return r.readError(err, "contract_id", c.ID())
		}
		return ierr.WithError(err).
			WithHint("Failed to update contract").
			Mark(ierr.ErrDatabase)
	}

	entRepo.SetSpanSuccess(span)
	return nil
}

func (r *repository) readError(err error, key, value string) error {
	if ent.IsNotFound(err) {
		return ierr.WithError(err).
			WithHintf("Contract with %s %s was not found", key, value).
			WithReportableDetails(map[string]any{
				key: value,
			}).
			Mark(ierr.ErrNotFound)
	}
	return ierr.WithError(err).
		WithHintf("Failed to get contract with %s %s", key, value).
		Mark(ierr.ErrDatabase)
}

type contractQuery = *ent.ContractQuery

// queryOptions implements query options for contract filtering and sorting
type queryOptions struct{}

func (o queryOptions) ApplyTenantFilter(ctx context.Context, query contractQuery) contractQuery {
	return query.Where(entContract.TenantID(types.GetTenantID(ctx)))
}

func (o queryOptions) ApplyEnvironmentFilter(ctx context.Context, query contractQuery) contractQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entContract.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o queryOptions) ApplyStatusFilter(query contractQuery, status string) contractQuery {
	if status == "" {
		return query.Where(entContract.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(entContract.Status(status))
}

func (o queryOptions) ApplySortFilter(query contractQuery, field string, order string) contractQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o queryOptions) ApplyPaginationFilter(query contractQuery, limit int, offset int) contractQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o queryOptions) GetFieldName(field string) string {
	if entContract.ValidColumn(field) {
		return field
	}
	return ""
}

func (o queryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in contract query", st).
			WithHintf("Unknown field name '%s' in contract query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o queryOptions) applyEntityQueryOptions(ctx context.Context, f *Filter, query contractQuery) (contractQuery, error) {
	if f == nil {
		return query, nil
	}

	if len(f.ContractIDs) > 0 {
		query = query.Where(entContract.IDIn(f.ContractIDs...))
	}
	if len(f.CustomerIDs) > 0 {
		query = query.Where(entContract.CustomerIDIn(f.CustomerIDs...))
	}
	if len(f.ContractStatuses) > 0 {
		query = query.Where(entContract.ContractStatusIn(lo.Map(f.ContractStatuses, func(s types.ContractStatus, _ int) string {
			return string(s)
		})...))
	}
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(subscriptionsPredicate(ctx, f.SubscriptionIDs))
	}
	if f.EndedAtGTE != nil {
		query = query.Where(entContract.EndedAtGTE(*f.EndedAtGTE))
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(entContract.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(entContract.CreatedAtLTE(*f.EndTime))
		}
	}

	var err error
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[contractQuery, predicate.Contract](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Contract { return predicate.Contract(p) },
		)
		if err != nil {
			return nil, err
		}
	}
	if f.Sort != nil {
		query, err = dsl.ApplySorts[contractQuery, entContract.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) entContract.OrderOption { return entContract.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

// subscriptionsPredicate matches contracts with a published association to any of the subscriptions.
func subscriptionsPredicate(ctx context.Context, subscriptionIDs []string) predicate.Contract {
	return func(s *sql.Selector) {
		t := sql.Table(entContractAssociation.Table)
		s.Where(sql.In(s.C(entContract.FieldID),
			sql.Select(t.C(entContractAssociation.FieldContractID)).
				From(t).
				Where(sql.And(
					sql.EQ(t.C(entContractAssociation.FieldTenantID), types.GetTenantID(ctx)),
					sql.EQ(t.C(entContractAssociation.FieldEnvironmentID), types.GetEnvironmentID(ctx)),
					sql.EQ(t.C(entContractAssociation.FieldEntityType), string(types.RevRecEntityTypeSubscription)),
					sql.EQ(t.C(entContractAssociation.FieldStatus), string(types.StatusPublished)),
					sql.In(t.C(entContractAssociation.FieldEntityID), lo.ToAnySlice(subscriptionIDs)...),
				)),
		))
	}
}
