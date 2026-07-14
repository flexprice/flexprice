package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/refund"
	domainRefund "github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type refundRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts RefundQueryOptions
}

// NewRefundRepository creates a new Ent-backed Refund repository.
func NewRefundRepository(client postgres.IClient, log *logger.Logger) domainRefund.Repository {
	return &refundRepository{
		client:    client,
		log:       log,
		queryOpts: RefundQueryOptions{},
	}
}

func (r *refundRepository) Create(ctx context.Context, ref *domainRefund.Refund) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "creating refund",
		"refund_id", ref.ID,
		"tenant_id", ref.TenantID,
		"payment_id", ref.PaymentID,
	)

	span := StartRepositorySpan(ctx, "refund", "create", map[string]interface{}{
		"refund_id":  ref.ID,
		"tenant_id":  ref.TenantID,
		"payment_id": ref.PaymentID,
	})
	defer FinishSpan(span)

	if ref.EnvironmentID == "" {
		ref.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	created, err := client.Refund.Create().
		SetID(ref.ID).
		SetPaymentID(ref.PaymentID).
		SetPaymentGateway(ref.PaymentGateway).
		SetNillableGatewayRefundID(ref.GatewayRefundID).
		SetNillableGatewayTrackingID(ref.GatewayTrackingID).
		SetAmount(ref.Amount).
		SetCurrency(ref.Currency).
		SetRefundStatus(string(ref.RefundStatus)).
		SetRefundReason(string(ref.RefundReason)).
		SetIdempotencyKey(ref.IdempotencyKey).
		SetGatewayIdempotencyToken(ref.GatewayIdempotencyToken).
		SetNillableFailureReason(ref.FailureReason).
		SetMetadata(ref.Metadata).
		SetGatewayMetadata(ref.GatewayMetadata).
		SetNillableInitiatedAt(ref.InitiatedAt).
		SetNillableSucceededAt(ref.SucceededAt).
		SetNillableFailedAt(ref.FailedAt).
		SetNillableCancelledAt(ref.CancelledAt).
		SetTenantID(ref.TenantID).
		SetCreatedAt(ref.CreatedAt).
		SetUpdatedAt(ref.UpdatedAt).
		SetCreatedBy(ref.CreatedBy).
		SetUpdatedBy(ref.UpdatedBy).
		SetEnvironmentID(ref.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create refund").
			WithReportableDetails(map[string]interface{}{
				"refund_id":  ref.ID,
				"payment_id": ref.PaymentID,
			}).
			Mark(ierr.ErrDatabase)
	}

	*ref = *domainRefund.FromEnt(created)
	return nil
}

func (r *refundRepository) Get(ctx context.Context, id string) (*domainRefund.Refund, error) {
	span := StartRepositorySpan(ctx, "refund", "get", map[string]interface{}{
		"refund_id": id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	ref, err := client.Refund.Query().
		Where(
			refund.ID(id),
			refund.EnvironmentID(types.GetEnvironmentID(ctx)),
			refund.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Refund not found").
				WithReportableDetails(map[string]interface{}{
					"refund_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve refund").
			WithReportableDetails(map[string]interface{}{
				"refund_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainRefund.FromEnt(ref), nil
}

func (r *refundRepository) Update(ctx context.Context, ref *domainRefund.Refund) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "updating refund",
		"refund_id", ref.ID,
		"tenant_id", ref.TenantID,
	)

	span := StartRepositorySpan(ctx, "refund", "update", map[string]interface{}{
		"refund_id": ref.ID,
		"tenant_id": ref.TenantID,
	})
	defer FinishSpan(span)

	_, err := client.Refund.Update().
		Where(
			refund.EnvironmentID(types.GetEnvironmentID(ctx)),
			refund.ID(ref.ID),
			refund.TenantID(ref.TenantID),
		).
		SetRefundStatus(string(ref.RefundStatus)).
		SetNillableGatewayRefundID(ref.GatewayRefundID).
		SetNillableGatewayTrackingID(ref.GatewayTrackingID).
		SetNillableFailureReason(ref.FailureReason).
		SetGatewayMetadata(ref.GatewayMetadata).
		SetMetadata(ref.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetNillableSucceededAt(ref.SucceededAt).
		SetNillableFailedAt(ref.FailedAt).
		SetNillableCancelledAt(ref.CancelledAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Refund not found").
				WithReportableDetails(map[string]interface{}{
					"refund_id": ref.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update refund").
			WithReportableDetails(map[string]interface{}{
				"refund_id": ref.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *refundRepository) Delete(ctx context.Context, id string) error {
	span := StartRepositorySpan(ctx, "refund", "delete", map[string]interface{}{
		"refund_id": id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "deleting refund",
		"refund_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Refund.Update().
		Where(
			refund.EnvironmentID(types.GetEnvironmentID(ctx)),
			refund.ID(id),
			refund.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Refund not found").
				WithReportableDetails(map[string]interface{}{
					"refund_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete refund").
			WithReportableDetails(map[string]interface{}{
				"refund_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *refundRepository) List(ctx context.Context, filter *types.RefundFilter) ([]*domainRefund.Refund, error) {
	if filter == nil {
		filter = &types.RefundFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "refund", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.Refund.Query()
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	refunds, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list refunds").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainRefund.FromEntList(refunds), nil
}

func (r *refundRepository) Count(ctx context.Context, filter *types.RefundFilter) (int, error) {
	client := r.client.Reader(ctx)

	span := StartRepositorySpan(ctx, "refund", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.Refund.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count refunds").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *refundRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainRefund.Refund, error) {
	span := StartRepositorySpan(ctx, "refund", "get_by_idempotency_key", map[string]interface{}{
		"idempotency_key": key,
		"tenant_id":       types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)

	ref, err := client.Refund.Query().
		Where(
			refund.IdempotencyKey(key),
			refund.EnvironmentID(types.GetEnvironmentID(ctx)),
			refund.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Refund not found").
				WithReportableDetails(map[string]interface{}{
					"idempotency_key": key,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get refund by idempotency key").
			WithReportableDetails(map[string]interface{}{
				"idempotency_key": key,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainRefund.FromEnt(ref), nil
}

// RefundQuery type alias for better readability
type RefundQuery = *ent.RefundQuery

// RefundQueryOptions implements BaseQueryOptions for refund queries
type RefundQueryOptions struct{}

func (o RefundQueryOptions) ApplyTenantFilter(ctx context.Context, query RefundQuery) RefundQuery {
	return query.Where(refund.TenantID(types.GetTenantID(ctx)))
}

func (o RefundQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query RefundQuery) RefundQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(refund.EnvironmentID(environmentID))
	}
	return query
}

func (o RefundQueryOptions) ApplyStatusFilter(query RefundQuery, status string) RefundQuery {
	if status == "" {
		return query.Where(refund.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(refund.Status(status))
}

func (o RefundQueryOptions) ApplySortFilter(query RefundQuery, field string, order string) RefundQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o RefundQueryOptions) ApplyPaginationFilter(query RefundQuery, limit int, offset int) RefundQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName returns the ent field name for the given field string.
func (o RefundQueryOptions) GetFieldName(field string) string {
	if refund.ValidColumn(field) {
		return field
	}
	return ""
}

func (o RefundQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.RefundFilter, query RefundQuery) RefundQuery {
	if f == nil {
		return query
	}

	if f.PaymentID != nil {
		query = query.Where(refund.PaymentID(*f.PaymentID))
	}

	if f.Status != nil {
		query = query.Where(refund.RefundStatus(string(*f.Status)))
	}

	if f.Gateway != nil {
		query = query.Where(refund.PaymentGateway(*f.Gateway))
	}

	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(refund.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(refund.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}
