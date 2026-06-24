package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	entCheckout "github.com/flexprice/flexprice/ent/checkoutsession"
	entSchema "github.com/flexprice/flexprice/ent/schema"
	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type checkoutSessionRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

func NewCheckoutSessionRepository(client postgres.IClient, logger *logger.Logger) domainCheckout.Repository {
	return &checkoutSessionRepository{client: client, logger: logger}
}

func (r *checkoutSessionRepository) Create(ctx context.Context, s *domainCheckout.CheckoutSession) error {
	span := StartRepositorySpan(ctx, "checkout_session", "create", map[string]interface{}{
		"id":          s.ID,
		"customer_id": s.CustomerID,
		"action":      s.Action,
	})
	defer FinishSpan(span)

	_, err := r.client.Writer(ctx).CheckoutSession.Create().
		SetID(s.ID).
		SetTenantID(s.TenantID).
		SetEnvironmentID(s.EnvironmentID).
		SetCustomerID(s.CustomerID).
		SetAction(s.Action).
		SetCheckoutStatus(s.CheckoutStatus).
		SetNillablePaymentProvider(s.PaymentProvider).
		SetNillableCheckoutInvoiceID(s.CheckoutInvoiceID).
		SetNillableCheckoutPaymentID(s.CheckoutPaymentID).
		SetConfiguration(s.Configuration).
		SetResult(s.Result).
		SetProviderResult(s.ProviderResult).
		SetNillableIdempotencyKey(s.IdempotencyKey).
		SetNillableSuccessURL(s.SuccessURL).
		SetNillableFailureURL(s.FailureURL).
		SetNillableCancelURL(s.CancelURL).
		SetNillableExpiresAt(s.ExpiresAt).
		SetNillableCompletedAt(s.CompletedAt).
		SetNillableCancelledAt(s.CancelledAt).
		SetNillableFailureReason(s.FailureReason).
		SetMetadata(s.Metadata).
		SetStatus(string(s.Status)).
		SetCreatedBy(s.CreatedBy).
		SetUpdatedBy(s.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Constraint == entSchema.Idx_checkout_session_idempotency_key_active {
				return ierr.WithError(err).
					WithHint("An active checkout session with this idempotency key already exists").
					WithReportableDetails(map[string]any{"idempotency_key": s.IdempotencyKey}).
					Mark(ierr.ErrAlreadyExists)
			}
			return ierr.WithError(err).
				WithHint("checkout session creation failed due to constraint violation").
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).WithHint("checkout session creation failed").Mark(ierr.ErrDatabase)
	}
	return nil
}

func (r *checkoutSessionRepository) Get(ctx context.Context, id string) (*domainCheckout.CheckoutSession, error) {
	span := StartRepositorySpan(ctx, "checkout_session", "get", map[string]interface{}{"id": id})
	defer FinishSpan(span)

	e, err := r.client.Reader(ctx).CheckoutSession.Query().
		Where(
			entCheckout.ID(id),
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("checkout session %s not found", id).
				WithReportableDetails(map[string]any{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("get checkout session failed").Mark(ierr.ErrDatabase)
	}
	return domainCheckout.FromEnt(e), nil
}

func (r *checkoutSessionRepository) Update(ctx context.Context, s *domainCheckout.CheckoutSession) error {
	span := StartRepositorySpan(ctx, "checkout_session", "update", map[string]interface{}{"id": s.ID})
	defer FinishSpan(span)

	n, err := r.client.Writer(ctx).CheckoutSession.Update().
		Where(
			entCheckout.ID(s.ID),
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetCheckoutStatus(s.CheckoutStatus).
		SetNillablePaymentProvider(s.PaymentProvider).
		SetNillableCheckoutInvoiceID(s.CheckoutInvoiceID).
		SetNillableCheckoutPaymentID(s.CheckoutPaymentID).
		SetResult(s.Result).
		SetProviderResult(s.ProviderResult).
		SetNillableCompletedAt(s.CompletedAt).
		SetNillableCancelledAt(s.CancelledAt).
		SetNillableFailureReason(s.FailureReason).
		SetMetadata(s.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).WithHint("checkout session update failed").Mark(ierr.ErrDatabase)
	}
	if n == 0 {
		return ierr.NewError("checkout session not found").
			WithHintf("checkout session %s not found or already archived", s.ID).
			Mark(ierr.ErrNotFound)
	}
	return nil
}

func (r *checkoutSessionRepository) List(ctx context.Context, filter *types.CheckoutSessionFilter) ([]*domainCheckout.CheckoutSession, error) {
	span := StartRepositorySpan(ctx, "checkout_session", "list", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	query := r.client.Reader(ctx).CheckoutSession.Query().
		Where(
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	query = applyCheckoutFilters(query, filter)

	if filter.GetLimit() > 0 {
		query = query.Limit(filter.GetLimit())
	}
	query = query.Offset(filter.GetOffset())

	orderFunc := ent.Desc
	if filter.GetOrder() == "asc" {
		orderFunc = ent.Asc
	}
	query = query.Order(orderFunc(entCheckout.FieldCreatedAt))

	entities, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("list checkout sessions failed").Mark(ierr.ErrDatabase)
	}

	result := make([]*domainCheckout.CheckoutSession, len(entities))
	for i, e := range entities {
		result[i] = domainCheckout.FromEnt(e)
	}
	return result, nil
}

func (r *checkoutSessionRepository) Count(ctx context.Context, filter *types.CheckoutSessionFilter) (int, error) {
	span := StartRepositorySpan(ctx, "checkout_session", "count", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		return 0, err
	}

	query := r.client.Reader(ctx).CheckoutSession.Query().
		Where(
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
			entCheckout.StatusEQ(string(types.StatusPublished)),
		)

	query = applyCheckoutFilters(query, filter)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).WithHint("count checkout sessions failed").Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (r *checkoutSessionRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainCheckout.CheckoutSession, error) {
	span := StartRepositorySpan(ctx, "checkout_session", "get_by_idempotency_key", map[string]interface{}{
		"idempotency_key": key,
	})
	defer FinishSpan(span)

	e, err := r.client.Reader(ctx).CheckoutSession.Query().
		Where(
			entCheckout.IdempotencyKeyEQ(key),
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
			entCheckout.StatusEQ(string(types.StatusPublished)),
			entCheckout.CheckoutStatusIn(
				types.CheckoutStatusInitiated,
				types.CheckoutStatusPending,
			),
		).
		First(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("no active checkout session found for idempotency key").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("get by idempotency key failed").Mark(ierr.ErrDatabase)
	}
	return domainCheckout.FromEnt(e), nil
}

func applyCheckoutFilters(query *ent.CheckoutSessionQuery, filter *types.CheckoutSessionFilter) *ent.CheckoutSessionQuery {
	if filter.CustomerID != nil {
		query = query.Where(entCheckout.CustomerID(*filter.CustomerID))
	}
	if len(filter.Statuses) > 0 {
		query = query.Where(entCheckout.CheckoutStatusIn(filter.Statuses...))
	}
	if len(filter.Actions) > 0 {
		query = query.Where(entCheckout.ActionIn(filter.Actions...))
	}
	if filter.PaymentProvider != nil {
		query = query.Where(entCheckout.PaymentProviderEQ(*filter.PaymentProvider))
	}
	return query
}
