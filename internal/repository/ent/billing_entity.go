package ent

import (
	"context"
	"errors"

	"github.com/flexprice/flexprice/ent"
	entbillingentity "github.com/flexprice/flexprice/ent/billingentity"
	entSchema "github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/clock"
	domain "github.com/flexprice/flexprice/internal/domain/billingentity"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type billingEntityRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewBillingEntityRepository(client postgres.IClient, log *logger.Logger) domain.Repository {
	return &billingEntityRepository{
		client: client,
		log:    log,
	}
}

func (r *billingEntityRepository) scoped(ctx context.Context) *ent.BillingEntityQuery {
	return r.client.Reader(ctx).BillingEntity.Query().
		Where(
			entbillingentity.TenantID(types.GetTenantID(ctx)),
			entbillingentity.EnvironmentID(types.GetEnvironmentID(ctx)),
			entbillingentity.Status(string(types.StatusPublished)),
		)
}

func (r *billingEntityRepository) Create(ctx context.Context, e *domain.BillingEntity) error {
	span := StartRepositorySpan(ctx, "billing_entity", "create", map[string]interface{}{
		"billing_entity_id": e.ID,
	})
	defer FinishSpan(span)

	if e.EnvironmentID == "" {
		e.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	created, err := r.client.Writer(ctx).BillingEntity.Create().
		SetID(e.ID).
		SetName(e.Name).
		SetIsDefault(e.IsDefault).
		SetInvoiceNumberPrefix(e.InvoiceNumberPrefix).
		SetBillingDetails(e.BillingDetails).
		SetMetadata(e.Metadata).
		SetTenantID(e.TenantID).
		SetEnvironmentID(e.EnvironmentID).
		SetStatus(string(e.Status)).
		SetCreatedAt(e.CreatedAt).
		SetUpdatedAt(e.UpdatedAt).
		SetCreatedBy(e.CreatedBy).
		SetUpdatedBy(e.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return r.mapWriteError(err, e)
	}

	SetSpanSuccess(span)
	*e = *billingEntityFromEnt(created)
	return nil
}

func (r *billingEntityRepository) Get(ctx context.Context, id string) (*domain.BillingEntity, error) {
	span := StartRepositorySpan(ctx, "billing_entity", "get", map[string]interface{}{
		"billing_entity_id": id,
	})
	defer FinishSpan(span)

	row, err := r.scoped(ctx).Where(entbillingentity.ID(id)).Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Billing entity %s was not found", id).
				WithReportableDetails(map[string]any{"billing_entity_id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get billing entity").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingEntityFromEnt(row), nil
}

func (r *billingEntityRepository) GetDefault(ctx context.Context) (*domain.BillingEntity, error) {
	span := StartRepositorySpan(ctx, "billing_entity", "get_default", nil)
	defer FinishSpan(span)

	row, err := r.scoped(ctx).Where(entbillingentity.IsDefault(true)).Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("This environment has no default billing entity").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get default billing entity").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingEntityFromEnt(row), nil
}

func (r *billingEntityRepository) List(ctx context.Context, filter *types.BillingEntityFilter) ([]*domain.BillingEntity, error) {
	span := StartRepositorySpan(ctx, "billing_entity", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if filter == nil {
		filter = types.NewBillingEntityFilter()
	}

	query := applyBillingEntityFilter(r.scoped(ctx), filter)
	sortField := entbillingentity.FieldCreatedAt
	if entbillingentity.ValidColumn(filter.GetSort()) {
		sortField = filter.GetSort()
	}
	if filter.GetOrder() == types.OrderAsc {
		query = query.Order(ent.Asc(sortField))
	} else {
		query = query.Order(ent.Desc(sortField))
	}
	if !filter.IsUnlimited() {
		query = query.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}

	rows, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list billing entities").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingEntitiesFromEnt(rows), nil
}

func (r *billingEntityRepository) Count(ctx context.Context, filter *types.BillingEntityFilter) (int, error) {
	span := StartRepositorySpan(ctx, "billing_entity", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if filter == nil {
		filter = types.NewBillingEntityFilter()
	}

	count, err := applyBillingEntityFilter(r.scoped(ctx), filter).Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count billing entities").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *billingEntityRepository) ListByIDs(ctx context.Context, ids []string) ([]*domain.BillingEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	span := StartRepositorySpan(ctx, "billing_entity", "list_by_ids", map[string]interface{}{
		"count": len(ids),
	})
	defer FinishSpan(span)

	rows, err := r.scoped(ctx).Where(entbillingentity.IDIn(ids...)).All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list billing entities").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return billingEntitiesFromEnt(rows), nil
}

func (r *billingEntityRepository) Update(ctx context.Context, e *domain.BillingEntity) error {
	span := StartRepositorySpan(ctx, "billing_entity", "update", map[string]interface{}{
		"billing_entity_id": e.ID,
	})
	defer FinishSpan(span)

	updated, err := r.client.Writer(ctx).BillingEntity.UpdateOneID(e.ID).
		Where(
			entbillingentity.TenantID(types.GetTenantID(ctx)),
			entbillingentity.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(e.Name).
		SetInvoiceNumberPrefix(e.InvoiceNumberPrefix).
		SetBillingDetails(e.BillingDetails).
		SetMetadata(e.Metadata).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Billing entity %s was not found", e.ID).
				Mark(ierr.ErrNotFound)
		}
		return r.mapWriteError(err, e)
	}

	SetSpanSuccess(span)
	*e = *billingEntityFromEnt(updated)
	return nil
}

func (r *billingEntityRepository) mapWriteError(err error, e *domain.BillingEntity) error {
	var pqErr *pq.Error
	if ent.IsConstraintError(err) && errors.As(err, &pqErr) {
		switch pqErr.Constraint {
		case entSchema.BillingEntityPrefixUniqueConstraint:
			return ierr.WithError(err).
				WithHintf("Invoice prefix %s is already used by another billing entity", e.InvoiceNumberPrefix).
				WithReportableDetails(map[string]any{"invoice_number_prefix": e.InvoiceNumberPrefix}).
				Mark(ierr.ErrAlreadyExists)
		case entSchema.BillingEntityDefaultUniqueConstraint:
			return ierr.WithError(err).
				WithHint("This environment already has a default billing entity").
				Mark(ierr.ErrAlreadyExists)
		}
	}
	return ierr.WithError(err).
		WithHint("Failed to save billing entity").
		Mark(ierr.ErrDatabase)
}

func applyBillingEntityFilter(query *ent.BillingEntityQuery, f *types.BillingEntityFilter) *ent.BillingEntityQuery {
	if len(f.BillingEntityIDs) > 0 {
		query = query.Where(entbillingentity.IDIn(f.BillingEntityIDs...))
	}
	if f.IsDefault != nil {
		query = query.Where(entbillingentity.IsDefault(*f.IsDefault))
	}
	return query
}

func billingEntityFromEnt(e *ent.BillingEntity) *domain.BillingEntity {
	if e == nil {
		return nil
	}
	return &domain.BillingEntity{
		ID:                  e.ID,
		Name:                e.Name,
		IsDefault:           e.IsDefault,
		InvoiceNumberPrefix: e.InvoiceNumberPrefix,
		BillingDetails:      e.BillingDetails,
		Metadata:            e.Metadata,
		EnvironmentID:       e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

func billingEntitiesFromEnt(rows []*ent.BillingEntity) []*domain.BillingEntity {
	if rows == nil {
		return nil
	}
	entities := make([]*domain.BillingEntity, len(rows))
	for i, row := range rows {
		entities[i] = billingEntityFromEnt(row)
	}
	return entities
}
