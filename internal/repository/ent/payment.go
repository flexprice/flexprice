package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/payment"
	"github.com/flexprice/flexprice/ent/paymentattempt"
	domainPayment "github.com/flexprice/flexprice/internal/domain/payment"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type paymentRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts PaymentQueryOptions
}

func NewPaymentRepository(client postgres.IClient, log *logger.Logger) domainPayment.Repository {
	return &paymentRepository{
		client:    client,
		log:       log,
		queryOpts: PaymentQueryOptions{},
	}
}

func (r *paymentRepository) Create(ctx context.Context, p *domainPayment.Payment) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating payment",
		"payment_id", p.ID,
		"tenant_id", p.TenantID,
		"destination_type", p.DestinationType,
		"destination_id", p.DestinationID,
	)

	payment, err := client.Payment.Create().
		SetID(p.ID).
		SetIdempotencyKey(p.IdempotencyKey).
		SetDestinationType(string(p.DestinationType)).
		SetDestinationID(p.DestinationID).
		SetPaymentMethodType(string(p.PaymentMethodType)).
		SetPaymentMethodID(p.PaymentMethodID).
		SetNillablePaymentGateway(p.PaymentGateway).
		SetNillableGatewayPaymentID(p.GatewayPaymentID).
		SetAmount(p.Amount).
		SetCurrency(p.Currency).
		SetPaymentStatus(string(p.PaymentStatus)).
		SetTrackAttempts(p.TrackAttempts).
		SetMetadata(p.Metadata).
		SetNillableSucceededAt(p.SucceededAt).
		SetNillableFailedAt(p.FailedAt).
		SetNillableRefundedAt(p.RefundedAt).
		SetNillableErrorMessage(p.ErrorMessage).
		SetTenantID(p.TenantID).
		SetCreatedAt(p.CreatedAt).
		SetUpdatedAt(p.UpdatedAt).
		SetCreatedBy(p.CreatedBy).
		SetUpdatedBy(p.UpdatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create payment: %w", err)
	}

	*p = *domainPayment.FromEnt(payment)
	return nil
}

func (r *paymentRepository) Get(ctx context.Context, id string) (*domainPayment.Payment, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting payment",
		"payment_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	p, err := client.Payment.Query().
		Where(
			payment.ID(id),
			payment.TenantID(types.GetTenantID(ctx)),
		).
		WithAttempts().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New(errors.ErrCodeNotFound, "payment not found")
		}
		return nil, fmt.Errorf("failed to get payment: %w", err)
	}

	return domainPayment.FromEnt(p), nil
}

func (r *paymentRepository) List(ctx context.Context, filter *types.PaymentFilter) ([]*domainPayment.Payment, error) {
	if filter == nil {
		filter = &types.PaymentFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Querier(ctx)
	query := client.Payment.Query().WithAttempts()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	payments, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list payments: %w", err)
	}

	return domainPayment.FromEntList(payments), nil
}

func (r *paymentRepository) Count(ctx context.Context, filter *types.PaymentFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Payment.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count payments: %w", err)
	}

	return count, nil
}

func (r *paymentRepository) Update(ctx context.Context, p *domainPayment.Payment) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating payment",
		"payment_id", p.ID,
		"tenant_id", p.TenantID,
	)

	_, err := client.Payment.Update().
		Where(
			payment.ID(p.ID),
			payment.TenantID(p.TenantID),
		).
		SetPaymentStatus(string(p.PaymentStatus)).
		SetNillablePaymentGateway(p.PaymentGateway).
		SetNillableGatewayPaymentID(p.GatewayPaymentID).
		SetTrackAttempts(p.TrackAttempts).
		SetMetadata(p.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetNillableSucceededAt(p.SucceededAt).
		SetNillableFailedAt(p.FailedAt).
		SetNillableRefundedAt(p.RefundedAt).
		SetNillableErrorMessage(p.ErrorMessage).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.New(errors.ErrCodeNotFound, "payment not found")
		}
		return fmt.Errorf("failed to update payment: %w", err)
	}

	return nil
}

func (r *paymentRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting payment",
		"payment_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Payment.Update().
		Where(
			payment.ID(id),
			payment.TenantID(types.GetTenantID(ctx)),
		).
		SetPaymentStatus(string(types.StatusArchived)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.New(errors.ErrCodeNotFound, "payment not found")
		}
		return fmt.Errorf("failed to delete payment: %w", err)
	}

	return nil
}

func (r *paymentRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainPayment.Payment, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting payment by idempotency key",
		"idempotency_key", key,
		"tenant_id", types.GetTenantID(ctx),
	)

	p, err := client.Payment.Query().
		Where(
			payment.IdempotencyKey(key),
			payment.TenantID(types.GetTenantID(ctx)),
		).
		WithAttempts().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New(errors.ErrCodeNotFound, "payment not found")
		}
		return nil, fmt.Errorf("failed to get payment by idempotency key: %w", err)
	}

	return domainPayment.FromEnt(p), nil
}

// Payment attempt operations

func (r *paymentRepository) CreateAttempt(ctx context.Context, a *domainPayment.PaymentAttempt) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating payment attempt",
		"attempt_id", a.ID,
		"payment_id", a.PaymentID,
		"status", a.Status,
		"payment_status", a.PaymentStatus,
	)

	attempt, err := client.PaymentAttempt.Create().
		SetID(a.ID).
		SetPaymentID(a.PaymentID).
		SetAttemptNumber(a.AttemptNumber).
		SetPaymentStatus(string(a.PaymentStatus)).
		SetNillableGatewayAttemptID(a.GatewayAttemptID).
		SetNillableErrorMessage(a.ErrorMessage).
		SetMetadata(a.Metadata).
		SetTenantID(a.TenantID).
		SetStatus(string(a.Status)).
		SetCreatedAt(a.CreatedAt).
		SetUpdatedAt(a.UpdatedAt).
		SetCreatedBy(a.CreatedBy).
		SetUpdatedBy(a.UpdatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create payment attempt: %w", err)
	}

	*a = *domainPayment.FromEntAttempt(attempt)
	return nil
}

func (r *paymentRepository) GetAttempt(ctx context.Context, id string) (*domainPayment.PaymentAttempt, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting payment attempt",
		"attempt_id", id,
	)

	a, err := client.PaymentAttempt.Query().
		Where(
			paymentattempt.ID(id),
			paymentattempt.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New(errors.ErrCodeNotFound, "payment attempt not found")
		}
		return nil, fmt.Errorf("failed to get payment attempt: %w", err)
	}

	return domainPayment.FromEntAttempt(a), nil
}

func (r *paymentRepository) UpdateAttempt(ctx context.Context, a *domainPayment.PaymentAttempt) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating payment attempt",
		"attempt_id", a.ID,
		"payment_id", a.PaymentID,
		"status", a.Status,
		"payment_status", a.PaymentStatus,
	)

	_, err := client.PaymentAttempt.Update().
		Where(
			paymentattempt.ID(a.ID),
			paymentattempt.TenantID(a.TenantID),
		).
		SetPaymentStatus(string(a.PaymentStatus)).
		SetStatus(string(a.Status)).
		SetNillableGatewayAttemptID(a.GatewayAttemptID).
		SetNillableErrorMessage(a.ErrorMessage).
		SetMetadata(a.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.New(errors.ErrCodeNotFound, "payment attempt not found")
		}
		return fmt.Errorf("failed to update payment attempt: %w", err)
	}

	return nil
}

func (r *paymentRepository) ListAttempts(ctx context.Context, paymentID string) ([]*domainPayment.PaymentAttempt, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("listing payment attempts",
		"payment_id", paymentID,
	)

	attempts, err := client.PaymentAttempt.Query().
		Where(
			paymentattempt.PaymentID(paymentID),
			paymentattempt.TenantID(types.GetTenantID(ctx)),
		).
		Order(ent.Asc(paymentattempt.FieldAttemptNumber)).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to list payment attempts: %w", err)
	}

	return domainPayment.FromEntAttemptList(attempts), nil
}

func (r *paymentRepository) GetLatestAttempt(ctx context.Context, paymentID string) (*domainPayment.PaymentAttempt, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting latest payment attempt",
		"payment_id", paymentID,
	)

	a, err := client.PaymentAttempt.Query().
		Where(
			paymentattempt.PaymentID(paymentID),
			paymentattempt.TenantID(types.GetTenantID(ctx)),
		).
		Order(ent.Desc(paymentattempt.FieldAttemptNumber)).
		First(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.New(errors.ErrCodeNotFound, "payment attempt not found")
		}
		return nil, fmt.Errorf("failed to get latest payment attempt: %w", err)
	}

	return domainPayment.FromEntAttempt(a), nil
}

// PaymentQuery type alias for better readability
type PaymentQuery = *ent.PaymentQuery

// PaymentQueryOptions implements BaseQueryOptions for payment queries
type PaymentQueryOptions struct{}

func (o PaymentQueryOptions) ApplyTenantFilter(ctx context.Context, query PaymentQuery) PaymentQuery {
	return query.Where(payment.TenantID(types.GetTenantID(ctx)))
}

func (o PaymentQueryOptions) ApplyStatusFilter(query PaymentQuery, status string) PaymentQuery {
	if status == "" {
		return query.Where(payment.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(payment.Status(status))
}

func (o PaymentQueryOptions) ApplySortFilter(query PaymentQuery, field string, order string) PaymentQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o PaymentQueryOptions) ApplyPaginationFilter(query PaymentQuery, limit int, offset int) PaymentQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o PaymentQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return payment.FieldCreatedAt
	case "updated_at":
		return payment.FieldUpdatedAt
	case "payment_status":
		return payment.FieldPaymentStatus
	case "amount":
		return payment.FieldAmount
	default:
		return field
	}
}

func (o PaymentQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.PaymentFilter, query PaymentQuery) PaymentQuery {
	if f == nil {
		return query
	}

	// Apply payment IDs filter if specified
	if len(f.PaymentIDs) > 0 {
		query = query.Where(payment.IDIn(f.PaymentIDs...))
	}

	// Apply destination type filter if specified
	if f.DestinationType != nil {
		query = query.Where(payment.DestinationType(*f.DestinationType))
	}

	// Apply destination ID filter if specified
	if f.DestinationID != nil {
		query = query.Where(payment.DestinationID(*f.DestinationID))
	}

	// Apply payment method type filter if specified
	if f.PaymentMethodType != nil {
		query = query.Where(payment.PaymentMethodType(*f.PaymentMethodType))
	}

	// Apply payment status filter if specified
	if f.PaymentStatus != nil {
		query = query.Where(payment.PaymentStatus(*f.PaymentStatus))
	}

	// Apply payment gateway filter if specified
	if f.PaymentGateway != nil {
		query = query.Where(payment.PaymentGateway(*f.PaymentGateway))
	}

	// Apply currency filter if specified
	if f.Currency != nil {
		query = query.Where(payment.Currency(*f.Currency))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(payment.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(payment.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}
