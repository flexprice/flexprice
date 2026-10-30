package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	entcommitment "github.com/flexprice/flexprice/ent/commitment"
	"github.com/flexprice/flexprice/ent/predicate"
	entSchema "github.com/flexprice/flexprice/ent/schema"
	domain "github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type commitmentRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewCommitmentRepository(client postgres.IClient, log *logger.Logger) domain.Repository {
	return &commitmentRepository{
		client: client,
		log:    log,
	}
}

func (r *commitmentRepository) scoped(ctx context.Context) *ent.CommitmentQuery {
	return r.client.Reader(ctx).Commitment.Query().
		Where(
			entcommitment.TenantID(types.GetTenantID(ctx)),
			entcommitment.EnvironmentID(types.GetEnvironmentID(ctx)),
			entcommitment.Status(string(types.StatusPublished)),
		)
}

func (r *commitmentRepository) Create(ctx context.Context, c *domain.Commitment) (*domain.Commitment, error) {
	span := StartRepositorySpan(ctx, "commitment", "create", map[string]interface{}{
		"commitment_id": c.ID,
	})
	defer FinishSpan(span)

	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	create := r.client.Writer(ctx).Commitment.Create().
		SetID(c.ID).
		SetSubscriptionID(c.SubscriptionID).
		SetCustomerID(c.CustomerID).
		SetName(c.Name).
		SetInvoiceLabel(c.InvoiceLabel).
		SetNillableInvoiceDescription(c.InvoiceDescription).
		SetCurrency(c.Currency).
		SetScopeEntityType(c.ScopeEntityType).
		SetScopeEntityID(c.ScopeEntityID).
		SetType(c.Type).
		SetValue(c.Value).
		SetNillableCommittedPriceID(c.CommittedPriceID).
		SetNillableOveragePriceID(c.OveragePriceID).
		SetMeasurement(c.Measurement).
		SetActiveWindows(c.ActiveWindows).
		SetTimezone(c.Timezone).
		SetPriority(c.Priority).
		SetBinding(c.Binding).
		SetNillableCollectionMethod(c.CollectionMethod).
		SetNillableTotalAmount(c.TotalAmount).
		SetNillableInstalmentInvoicing(c.InstalmentInvoicing).
		SetStartDate(c.StartDate).
		SetNillableEndDate(c.EndDate).
		SetNillableOnEarlyTermination(c.OnEarlyTermination).
		SetNillableIdempotencyKey(c.IdempotencyKey).
		SetMetadata(c.Metadata).
		SetTenantID(c.TenantID).
		SetEnvironmentID(c.EnvironmentID).
		SetStatus(string(c.Status)).
		SetCreatedAt(c.CreatedAt).
		SetUpdatedAt(c.UpdatedAt).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy)

	if c.CommitmentStatus != "" {
		create = create.SetCommitmentStatus(c.CommitmentStatus)
	}

	created, err := create.Save(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if c.IdempotencyKey != nil && errors.As(err, &pqErr) && pqErr.Constraint == entSchema.CommitmentIdempotencyKeyConstraint {
				return nil, ierr.WithError(err).
					WithHint("A commitment with this idempotency key already exists").
					Mark(ierr.ErrAlreadyExists)
			}

			return nil, ierr.WithError(err).
				WithHint("Commitment violates a database constraint").
				Mark(ierr.ErrDatabase)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to create commitment").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEnt(created), nil
}

func (r *commitmentRepository) Get(ctx context.Context, id string) (*domain.Commitment, error) {
	span := StartRepositorySpan(ctx, "commitment", "get", map[string]interface{}{
		"commitment_id": id,
	})
	defer FinishSpan(span)

	row, err := r.scoped(ctx).
		Where(entcommitment.ID(id)).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Commitment %s not found", id).
				Mark(ierr.ErrNotFound)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to get commitment").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEnt(row), nil
}

func (r *commitmentRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Commitment, error) {
	span := StartRepositorySpan(ctx, "commitment", "get_by_idempotency_key", map[string]interface{}{
		"idempotency_key": key,
	})
	defer FinishSpan(span)

	row, err := r.scoped(ctx).
		Where(entcommitment.IdempotencyKey(key)).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Commitment not found").
				Mark(ierr.ErrNotFound)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to get commitment").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEnt(row), nil
}

// Update persists only the fields that may change after creation.
func (r *commitmentRepository) Update(ctx context.Context, c *domain.Commitment) (*domain.Commitment, error) {
	span := StartRepositorySpan(ctx, "commitment", "update", map[string]interface{}{
		"commitment_id": c.ID,
	})
	defer FinishSpan(span)

	upd := r.client.Writer(ctx).Commitment.UpdateOneID(c.ID).
		Where(
			entcommitment.TenantID(types.GetTenantID(ctx)),
			entcommitment.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(c.Name).
		SetInvoiceLabel(c.InvoiceLabel).
		SetMetadata(c.Metadata).
		SetNillableTerminatedAt(c.TerminatedAt).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))

	if c.CommitmentStatus != "" {
		upd = upd.SetCommitmentStatus(c.CommitmentStatus)
	}

	if c.InvoiceDescription != nil {
		upd = upd.SetInvoiceDescription(*c.InvoiceDescription)
	} else {
		upd = upd.ClearInvoiceDescription()
	}

	row, err := upd.Save(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Commitment %s not found", c.ID).
				Mark(ierr.ErrNotFound)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to update commitment").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEnt(row), nil
}

func (r *commitmentRepository) Delete(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "commitment", "delete", map[string]interface{}{
		"commitment_id": id,
	})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).Commitment.UpdateOneID(id).
		Where(
			entcommitment.TenantID(types.GetTenantID(ctx)),
			entcommitment.EnvironmentID(types.GetEnvironmentID(ctx)),
			entcommitment.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Commitment %s not found", id).
				Mark(ierr.ErrNotFound)
		}

		return ierr.WithError(err).
			WithHint("Failed to delete commitment").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *commitmentRepository) List(ctx context.Context, filter *domain.Filter) ([]*domain.Commitment, error) {
	if filter == nil {
		filter = domain.NewNoLimitFilter()
	}

	span := StartRepositorySpan(ctx, "commitment", "list", map[string]interface{}{
		"limit":  filter.GetLimit(),
		"offset": filter.GetOffset(),
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	q, err := applyCommitmentFilter(r.scoped(ctx), filter)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	q, err = applyCommitmentSort(q, filter.Sort)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	if !filter.IsUnlimited() {
		q = q.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}

	rows, err := q.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list commitments").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEntList(rows), nil
}

func (r *commitmentRepository) Count(ctx context.Context, filter *domain.Filter) (int, error) {
	if filter == nil {
		filter = domain.NewNoLimitFilter()
	}

	if err := filter.Validate(); err != nil {
		return 0, err
	}

	span := StartRepositorySpan(ctx, "commitment", "count", map[string]interface{}{})
	defer FinishSpan(span)

	q, err := applyCommitmentFilter(r.scoped(ctx), filter)
	if err != nil {
		SetSpanError(span, err)
		return 0, err
	}

	n, err := q.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count commitments").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return n, nil
}

func applyCommitmentFilter(q *ent.CommitmentQuery, f *domain.Filter) (*ent.CommitmentQuery, error) {
	preds := []predicate.Commitment{}
	if len(f.IDs) > 0 {
		preds = append(preds, entcommitment.IDIn(f.IDs...))
	}

	if len(f.SubscriptionIDs) > 0 {
		preds = append(preds, entcommitment.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	if len(f.CustomerIDs) > 0 {
		preds = append(preds, entcommitment.CustomerIDIn(f.CustomerIDs...))
	}

	if len(f.Statuses) > 0 {
		preds = append(preds, entcommitment.CommitmentStatusIn(f.Statuses...))
	}

	if f.ScopeType != nil {
		preds = append(preds, entcommitment.ScopeEntityTypeEQ(*f.ScopeType))
	}

	if len(f.ScopeIDs) > 0 {
		preds = append(preds, entcommitment.ScopeEntityIDIn(f.ScopeIDs...))
	}

	if f.ActiveFrom != nil {
		preds = append(preds, entcommitment.Or(entcommitment.EndDateIsNil(), entcommitment.EndDateGT(*f.ActiveFrom)))
	}

	if f.ActiveTo != nil {
		preds = append(preds, entcommitment.StartDateLT(*f.ActiveTo))
	}

	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			preds = append(preds, entcommitment.CreatedAtGTE(*f.StartTime))
		}

		if f.EndTime != nil {
			preds = append(preds, entcommitment.CreatedAtLTE(*f.EndTime))
		}
	}

	if len(preds) > 0 {
		q = q.Where(preds...)
	}

	return dsl.ApplyFilters[*ent.CommitmentQuery, predicate.Commitment](
		q,
		f.Filters,
		resolveCommitmentField,
		func(p dsl.Predicate) predicate.Commitment { return predicate.Commitment(p) },
	)
}

// applyCommitmentSort orders by the requested fields, else by priority, start date and id.
func applyCommitmentSort(q *ent.CommitmentQuery, sort []*types.SortCondition) (*ent.CommitmentQuery, error) {
	if len(sort) == 0 {
		return q.Order(
			ent.Asc(entcommitment.FieldPriority),
			ent.Asc(entcommitment.FieldStartDate),
			ent.Asc(entcommitment.FieldID),
		), nil
	}

	return dsl.ApplySorts[*ent.CommitmentQuery, entcommitment.OrderOption](
		q,
		sort,
		resolveCommitmentField,
		func(o dsl.OrderFunc) entcommitment.OrderOption { return entcommitment.OrderOption(o) },
	)
}

func resolveCommitmentField(field string) (string, error) {
	if !entcommitment.ValidColumn(field) {
		return "", ierr.NewErrorf("unknown field name '%s' in commitment query", field).
			Mark(ierr.ErrValidation)
	}

	return field, nil
}
