package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/payment"
	"github.com/flexprice/flexprice/ent/paymentattempt"
	"github.com/flexprice/flexprice/internal/cache"
	domainPayment "github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type paymentRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts PaymentQueryOptions
	cache     cache.Cache
}

func NewPaymentRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainPayment.Repository {
	return &paymentRepository{
		client:    client,
		log:       log,
		queryOpts: PaymentQueryOptions{},
		cache:     cache,
	}
}

func (r *paymentRepository) Create(ctx context.Context, p *domainPayment.Payment) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating payment",
		"payment_id", p.ID,
		"tenant_id", p.TenantID,
		"destination_type", p.DestinationType,
		"destination_id", p.DestinationID,
		"amount", p.Amount,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "create", map[string]interface{}{
		"payment_id":       p.ID,
		"tenant_id":        p.TenantID,
		"destination_type": p.DestinationType,
		"destination_id":   p.DestinationID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	payment, err := client.Payment.Create().
		SetID(p.ID).
		SetIdempotencyKey(p.IdempotencyKey).
		SetDestinationType(string(p.DestinationType)).
		SetDestinationID(p.DestinationID).
		SetPaymentMethodType(string(p.PaymentMethodType)).
		SetPaymentMethodID(p.PaymentMethodID).
		SetNillablePaymentGateway(p.PaymentGateway).
		SetNillableGatewayPaymentID(p.GatewayPaymentID).
		SetNillableGatewayTrackingID(p.GatewayTrackingID).
		SetGatewayMetadata(p.GatewayMetadata).
		SetAmount(p.Amount).
		SetCurrency(p.Currency).
		SetPaymentStatus(string(p.PaymentStatus)).
		SetTrackAttempts(p.TrackAttempts).
		SetMetadata(p.Metadata).
		SetNillableSucceededAt(p.SucceededAt).
		SetNillableFailedAt(p.FailedAt).
		SetNillableRefundedAt(p.RefundedAt).
		SetNillableErrorMessage(p.ErrorMessage).
		SetNillableRecordedAt(p.RecordedAt).
		SetTenantID(p.TenantID).
		SetCreatedAt(p.CreatedAt).
		SetUpdatedAt(p.UpdatedAt).
		SetCreatedBy(p.CreatedBy).
		SetUpdatedBy(p.UpdatedBy).
		SetEnvironmentID(p.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create payment").
			WithReportableDetails(map[string]interface{}{
				"payment_id":       p.ID,
				"destination_id":   p.DestinationID,
				"destination_type": p.DestinationType,
			}).
			Mark(ierr.ErrDatabase)
	}

	*p = *domainPayment.FromEnt(payment)
	return nil
}

func (r *paymentRepository) Get(ctx context.Context, id string) (*domainPayment.Payment, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "get", map[string]interface{}{
		"payment_id": id,
		"tenant_id":  types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedPayment := r.GetCache(ctx, id); cachedPayment != nil {
		return cachedPayment, nil
	}

	client := r.client.Reader(ctx)

	r.log.Debugw("getting payment",
		"payment_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	p, err := client.Payment.Query().
		Where(
			payment.ID(id),
			payment.EnvironmentID(types.GetEnvironmentID(ctx)),
			payment.TenantID(types.GetTenantID(ctx)),
		).
		WithAttempts().
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Payment not found").
				WithReportableDetails(map[string]interface{}{
					"payment_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve payment").
			WithReportableDetails(map[string]interface{}{
				"payment_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	paymentData := domainPayment.FromEnt(p)
	r.SetCache(ctx, paymentData)
	return paymentData, nil
}

func (r *paymentRepository) List(ctx context.Context, filter *types.PaymentFilter) ([]*domainPayment.Payment, error) {
	if filter == nil {
		filter = &types.PaymentFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.Payment.Query().WithAttempts()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	payments, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list payments").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainPayment.FromEntList(payments), nil
}

func (r *paymentRepository) Count(ctx context.Context, filter *types.PaymentFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.Payment.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count payments").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *paymentRepository) Update(ctx context.Context, p *domainPayment.Payment) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating payment",
		"payment_id", p.ID,
		"tenant_id", p.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "update", map[string]interface{}{
		"payment_id": p.ID,
		"tenant_id":  p.TenantID,
	})
	defer FinishSpan(span)

	_, err := client.Payment.Update().
		Where(
			payment.EnvironmentID(types.GetEnvironmentID(ctx)),
			payment.ID(p.ID),
			payment.TenantID(p.TenantID),
		).
		SetPaymentStatus(string(p.PaymentStatus)).
		SetPaymentMethodID(p.PaymentMethodID).
		SetNillablePaymentGateway(p.PaymentGateway).
		SetNillableGatewayPaymentID(p.GatewayPaymentID).
		SetNillableGatewayTrackingID(p.GatewayTrackingID).
		SetGatewayMetadata(p.GatewayMetadata).
		SetTrackAttempts(p.TrackAttempts).
		SetMetadata(p.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetNillableRecordedAt(p.RecordedAt).
		SetNillableSucceededAt(p.SucceededAt).
		SetNillableFailedAt(p.FailedAt).
		SetNillableRefundedAt(p.RefundedAt).
		SetNillableErrorMessage(p.ErrorMessage).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Payment not found").
				WithReportableDetails(map[string]interface{}{
					"payment_id": p.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update payment").
			WithReportableDetails(map[string]interface{}{
				"payment_id": p.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, p.ID)
	return nil
}

func (r *paymentRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "delete", map[string]interface{}{
		"payment_id": id,
		"tenant_id":  types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	r.log.Debugw("deleting payment",
		"payment_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Payment.Update().
		Where(
			payment.EnvironmentID(types.GetEnvironmentID(ctx)),
			payment.ID(id),
			payment.TenantID(types.GetTenantID(ctx)),
		).
		SetPaymentStatus(string(types.StatusArchived)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Payment not found").
				WithReportableDetails(map[string]interface{}{
					"payment_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete payment").
			WithReportableDetails(map[string]interface{}{
				"payment_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, id)
	return nil
}

func (r *paymentRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainPayment.Payment, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "get_by_idempotency_key", map[string]interface{}{
		"idempotency_key": key,
		"tenant_id":       types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	r.log.Debugw("getting payment by idempotency key",
		"idempotency_key", key,
		"tenant_id", types.GetTenantID(ctx),
	)

	p, err := client.Payment.Query().
		Where(
			payment.IdempotencyKey(key),
			payment.EnvironmentID(types.GetEnvironmentID(ctx)),
			payment.TenantID(types.GetTenantID(ctx)),
		).
		WithAttempts().
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Payment not found").
				WithReportableDetails(map[string]interface{}{
					"idempotency_key": key,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get payment by idempotency key").
			WithReportableDetails(map[string]interface{}{
				"idempotency_key": key,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainPayment.FromEnt(p), nil
}

// Payment attempt operations

func (r *paymentRepository) CreateAttempt(ctx context.Context, a *domainPayment.PaymentAttempt) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "create_attempt", map[string]interface{}{
		"attempt_id": a.ID,
		"payment_id": a.PaymentID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	r.log.Debugw("creating payment attempt",
		"attempt_id", a.ID,
		"payment_id", a.PaymentID,
		"status", a.Status,
		"payment_status", a.PaymentStatus,
	)

	// Set environment ID from context if not already set
	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

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
		SetEnvironmentID(a.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create payment attempt").
			WithReportableDetails(map[string]interface{}{
				"attempt_id": a.ID,
				"payment_id": a.PaymentID,
			}).
			Mark(ierr.ErrDatabase)
	}

	*a = *domainPayment.FromEntAttempt(attempt)
	return nil
}

func (r *paymentRepository) GetAttempt(ctx context.Context, id string) (*domainPayment.PaymentAttempt, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "get_attempt", map[string]interface{}{
		"attempt_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	r.log.Debugw("getting payment attempt",
		"attempt_id", id,
	)

	a, err := client.PaymentAttempt.Query().
		Where(
			paymentattempt.ID(id),
			paymentattempt.TenantID(types.GetTenantID(ctx)),
			paymentattempt.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Payment attempt not found").
				WithReportableDetails(map[string]interface{}{
					"attempt_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get payment attempt").
			WithReportableDetails(map[string]interface{}{
				"attempt_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainPayment.FromEntAttempt(a), nil
}

func (r *paymentRepository) UpdateAttempt(ctx context.Context, a *domainPayment.PaymentAttempt) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "update_attempt", map[string]interface{}{
		"attempt_id": a.ID,
		"payment_id": a.PaymentID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	r.log.Debugw("updating payment attempt",
		"attempt_id", a.ID,
		"payment_id", a.PaymentID,
		"status", a.Status,
		"payment_status", a.PaymentStatus,
	)

	_, err := client.PaymentAttempt.Update().
		Where(
			paymentattempt.EnvironmentID(types.GetEnvironmentID(ctx)),
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
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Payment attempt not found").
				WithReportableDetails(map[string]interface{}{
					"attempt_id": a.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update payment attempt").
			WithReportableDetails(map[string]interface{}{
				"attempt_id": a.ID,
				"payment_id": a.PaymentID,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, a.ID)
	return nil
}

func (r *paymentRepository) ListAttempts(ctx context.Context, paymentID string) ([]*domainPayment.PaymentAttempt, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "list_attempts", map[string]interface{}{
		"payment_id": paymentID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	r.log.Debugw("listing payment attempts",
		"payment_id", paymentID,
	)

	attempts, err := client.PaymentAttempt.Query().
		Where(
			paymentattempt.PaymentID(paymentID),
			paymentattempt.TenantID(types.GetTenantID(ctx)),
			paymentattempt.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Order(ent.Asc(paymentattempt.FieldAttemptNumber)).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list payment attempts").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainPayment.FromEntAttemptList(attempts), nil
}

func (r *paymentRepository) GetLatestAttempt(ctx context.Context, paymentID string) (*domainPayment.PaymentAttempt, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "payment", "get_latest_attempt", map[string]interface{}{
		"payment_id": paymentID,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	r.log.Debugw("getting latest payment attempt",
		"payment_id", paymentID,
	)

	a, err := client.PaymentAttempt.Query().
		Where(
			paymentattempt.EnvironmentID(types.GetEnvironmentID(ctx)),
			paymentattempt.PaymentID(paymentID),
			paymentattempt.TenantID(types.GetTenantID(ctx)),
		).
		Order(ent.Desc(paymentattempt.FieldAttemptNumber)).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Payment attempt not found").
				WithReportableDetails(map[string]interface{}{
					"payment_id": paymentID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get latest payment attempt").
			WithReportableDetails(map[string]interface{}{
				"payment_id": paymentID,
			}).
			Mark(ierr.ErrDatabase)
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

func (o PaymentQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query PaymentQuery) PaymentQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(payment.EnvironmentID(environmentID))
	}
	return query
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

func (o PaymentQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.PaymentFilter, query PaymentQuery) PaymentQuery {
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

	// Apply gateway payment ID filter if specified
	if f.GatewayPaymentID != nil {
		query = query.Where(payment.GatewayPaymentID(*f.GatewayPaymentID))
	}

	// Apply gateway tracking ID filter if specified
	if f.GatewayTrackingID != nil {
		query = query.Where(payment.GatewayTrackingID(*f.GatewayTrackingID))
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

func (r *paymentRepository) SetCache(ctx context.Context, payment *domainPayment.Payment) {
	span := cache.StartCacheSpan(ctx, "payment", "set", map[string]interface{}{
		"payment_id": payment.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPayment, tenantID, environmentID, payment.ID)
	r.cache.Set(ctx, cacheKey, payment, cache.ExpiryDefaultInMemory)
}

func (r *paymentRepository) GetCache(ctx context.Context, key string) *domainPayment.Payment {
	span := cache.StartCacheSpan(ctx, "payment", "get", map[string]interface{}{
		"payment_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPayment, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainPayment.Payment)
	}
	return nil
}

func (r *paymentRepository) DeleteCache(ctx context.Context, paymentID string) {
	span := cache.StartCacheSpan(ctx, "payment", "delete", map[string]interface{}{
		"payment_id": paymentID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPayment, tenantID, environmentID, paymentID)
	r.cache.Delete(ctx, cacheKey)
}
