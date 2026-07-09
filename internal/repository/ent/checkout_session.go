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
	"github.com/samber/lo"
)

type checkoutSessionRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewCheckoutSessionRepository(client postgres.IClient, log *logger.Logger) domainCheckout.Repository {
	return &checkoutSessionRepository{client: client, log: log}
}

func (r *checkoutSessionRepository) Create(ctx context.Context, s *domainCheckout.CheckoutSession) error {
	r.log.Debug(ctx, "creating checkout session",
		"id", s.ID,
		"customer_id", s.CustomerID,
		"action", s.Action,
	)

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
		SetPaymentProvider(s.PaymentProvider).
		SetNillableCheckoutInvoiceID(s.CheckoutInvoiceID).
		SetNillableCheckoutPaymentID(s.CheckoutPaymentID).
		SetConfiguration(types.CheckoutConfiguration(s.Configuration)).
		SetPaymentProviderConfig((*types.CheckoutPaymentProviderConfig)(s.PaymentProviderConfig)).
		SetResult((*types.CheckoutResult)(s.Result)).
		SetProviderResult((*types.CheckoutProviderResult)(s.ProviderResult)).
		SetNillableIdempotencyKey(s.IdempotencyKey).
		SetNillableSuccessURL(s.SuccessURL).
		SetNillableFailureURL(s.FailureURL).
		SetNillableCancelURL(s.CancelURL).
		SetExpiresAt(s.ExpiresAt).
		SetNillableCompletedAt(s.CompletedAt).
		SetNillableCancelledAt(s.CancelledAt).
		SetNillableFailureReason(s.FailureReason).
		SetMetadata(map[string]string(s.Metadata)).
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

	SetSpanSuccess(span)
	return nil
}

func (r *checkoutSessionRepository) Get(ctx context.Context, id string) (*domainCheckout.CheckoutSession, error) {
	r.log.Debug(ctx, "getting checkout session", "id", id)

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

	SetSpanSuccess(span)
	return fromEntCheckout(e), nil
}

func (r *checkoutSessionRepository) Update(ctx context.Context, s *domainCheckout.CheckoutSession) error {
	r.log.Debug(ctx, "updating checkout session", "id", s.ID)

	span := StartRepositorySpan(ctx, "checkout_session", "update", map[string]interface{}{"id": s.ID})
	defer FinishSpan(span)

	n, err := r.client.Writer(ctx).CheckoutSession.Update().
		Where(
			entCheckout.ID(s.ID),
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(s.Status)).
		SetCheckoutStatus(s.CheckoutStatus).
		SetNillableCheckoutInvoiceID(s.CheckoutInvoiceID).
		SetNillableCheckoutPaymentID(s.CheckoutPaymentID).
		SetResult((*types.CheckoutResult)(s.Result)).
		SetProviderResult((*types.CheckoutProviderResult)(s.ProviderResult)).
		SetExpiresAt(s.ExpiresAt).
		SetNillableCompletedAt(s.CompletedAt).
		SetNillableCancelledAt(s.CancelledAt).
		SetNillableFailureReason(s.FailureReason).
		SetMetadata(map[string]string(s.Metadata)).
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

	SetSpanSuccess(span)
	return nil
}

func (r *checkoutSessionRepository) List(ctx context.Context, filter *types.CheckoutSessionFilter) ([]*domainCheckout.CheckoutSession, error) {
	span := StartRepositorySpan(ctx, "checkout_session", "list", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	if filter == nil {
		filter = types.NewDefaultCheckoutSessionFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, err
	}

	queryOpts := CheckoutSessionQueryOptions{}
	query := r.client.Reader(ctx).CheckoutSession.Query()
	query, err := queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	query = ApplyQueryOptions(ctx, query, filter, queryOpts)

	entities, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("list checkout sessions failed").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	result := make([]*domainCheckout.CheckoutSession, len(entities))
	for i, e := range entities {
		result[i] = fromEntCheckout(e)
	}
	return result, nil
}

func (r *checkoutSessionRepository) Count(ctx context.Context, filter *types.CheckoutSessionFilter) (int, error) {
	span := StartRepositorySpan(ctx, "checkout_session", "count", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	if filter == nil {
		filter = types.NewDefaultCheckoutSessionFilter()
	}

	if err := filter.Validate(); err != nil {
		return 0, err
	}

	queryOpts := CheckoutSessionQueryOptions{}
	query := r.client.Reader(ctx).CheckoutSession.Query()
	query = ApplyBaseFilters(ctx, query, filter, queryOpts)
	query, err := queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).WithHint("count checkout sessions failed").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *checkoutSessionRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domainCheckout.CheckoutSession, error) {
	r.log.Debug(ctx, "getting checkout session by idempotency key", "idempotency_key", key)

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

	SetSpanSuccess(span)
	return fromEntCheckout(e), nil
}

func (r *checkoutSessionRepository) Delete(ctx context.Context, id string) error {
	r.log.Debug(ctx, "deleting checkout session", "id", id)

	span := StartRepositorySpan(ctx, "checkout_session", "delete", map[string]interface{}{"id": id})
	defer FinishSpan(span)

	n, err := r.client.Writer(ctx).CheckoutSession.Update().
		Where(
			entCheckout.ID(id),
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("checkout session delete failed").
			WithReportableDetails(map[string]any{"id": id}).
			Mark(ierr.ErrDatabase)
	}
	if n == 0 {
		return ierr.NewError("checkout session not found").
			WithHintf("checkout session %s not found or already archived", id).
			Mark(ierr.ErrNotFound)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *checkoutSessionRepository) MarkCompleted(ctx context.Context, sessionID string, completedAt time.Time, providerResult *types.CheckoutProviderResult) (bool, error) {
	r.log.Debug(ctx, "marking checkout session completed", "id", sessionID)

	span := StartRepositorySpan(ctx, "checkout_session", "mark_completed", map[string]interface{}{"id": sessionID})
	defer FinishSpan(span)

	n, err := r.client.Writer(ctx).CheckoutSession.Update().
		Where(
			entCheckout.ID(sessionID),
			entCheckout.TenantID(types.GetTenantID(ctx)),
			entCheckout.EnvironmentID(types.GetEnvironmentID(ctx)),
			entCheckout.CheckoutStatusIn(types.CheckoutStatusPending, types.CheckoutStatusInitiated),
		).
		SetCheckoutStatus(types.CheckoutStatusCompleted).
		SetCompletedAt(completedAt).
		SetProviderResult(providerResult).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return false, ierr.WithError(err).WithHint("failed to mark checkout session completed").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return n > 0, nil
}

// ListExpiredCheckoutSessions returns active (initiated|pending) sessions whose ExpiresAt is before
// effectiveDate within the tenant+environment in ctx, ordered by expires_at asc.
func (r *checkoutSessionRepository) ListExpiredCheckoutSessions(ctx context.Context, effectiveDate time.Time, limit, offset int) ([]*domainCheckout.CheckoutSession, error) {
	span := StartRepositorySpan(ctx, "checkout_session", "list_expired", map[string]interface{}{
		"effective_date": effectiveDate,
		"limit":          limit,
		"offset":         offset,
	})
	defer FinishSpan(span)

	query := r.client.Reader(ctx).CheckoutSession.Query().
		Where(
			entCheckout.StatusNotIn(string(types.StatusDeleted)),
			entCheckout.CheckoutStatusIn(
				types.CheckoutStatusInitiated,
				types.CheckoutStatusPending,
			),
			entCheckout.ExpiresAtLT(effectiveDate),
		).
		Order(ent.Asc(entCheckout.FieldExpiresAt)).
		Limit(limit).
		Offset(offset)

	entities, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("list expired checkout sessions failed").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	result := make([]*domainCheckout.CheckoutSession, len(entities))
	for i, e := range entities {
		result[i] = fromEntCheckout(e)
	}
	return result, nil
}

// CheckoutSessionQuery type alias for better readability
type CheckoutSessionQuery = *ent.CheckoutSessionQuery

// CheckoutSessionQueryOptions implements BaseQueryOptions for checkout session queries
type CheckoutSessionQueryOptions struct{}

func (o CheckoutSessionQueryOptions) ApplyTenantFilter(ctx context.Context, query CheckoutSessionQuery) CheckoutSessionQuery {
	return query.Where(entCheckout.TenantID(types.GetTenantID(ctx)))
}

func (o CheckoutSessionQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CheckoutSessionQuery) CheckoutSessionQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entCheckout.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o CheckoutSessionQueryOptions) ApplyStatusFilter(query CheckoutSessionQuery, status string) CheckoutSessionQuery {
	if status == "" {
		return query.Where(entCheckout.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(entCheckout.StatusEQ(status))
}

func (o CheckoutSessionQueryOptions) ApplySortFilter(query CheckoutSessionQuery, field string, order string) CheckoutSessionQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	if field == "" {
		field = entCheckout.FieldCreatedAt
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o CheckoutSessionQueryOptions) ApplyPaginationFilter(query CheckoutSessionQuery, limit int, offset int) CheckoutSessionQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o CheckoutSessionQueryOptions) GetFieldName(field string) string {
	if entCheckout.ValidColumn(field) {
		return field
	}
	return ""
}

func (o CheckoutSessionQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CheckoutSessionFilter, query CheckoutSessionQuery) (CheckoutSessionQuery, error) {
	if f == nil {
		return query, nil
	}
	if len(f.CustomerIDs) > 0 {
		query = query.Where(entCheckout.CustomerIDIn(f.CustomerIDs...))
	}
	if len(f.Actions) > 0 {
		query = query.Where(entCheckout.ActionIn(f.Actions...))
	}
	if len(f.PaymentProviders) > 0 {
		query = query.Where(entCheckout.PaymentProviderIn(f.PaymentProviders...))
	}
	if len(f.CheckoutStatuses) > 0 {
		query = query.Where(entCheckout.CheckoutStatusIn(f.CheckoutStatuses...))
	}
	if f.ExpiresAtLT != nil {
		query = query.Where(entCheckout.ExpiresAtLT(lo.FromPtr(f.ExpiresAtLT)))
	}
	if len(f.CheckoutInvoiceIDs) > 0 {
		query = query.Where(entCheckout.CheckoutInvoiceIDIn(f.CheckoutInvoiceIDs...))
	}
	if len(f.CheckoutPaymentIDs) > 0 {
		query = query.Where(entCheckout.CheckoutPaymentIDIn(f.CheckoutPaymentIDs...))
	}
	return query, nil
}

func fromEntCheckout(e *ent.CheckoutSession) *domainCheckout.CheckoutSession {
	s := &domainCheckout.CheckoutSession{
		ID:                    e.ID,
		EnvironmentID:         e.EnvironmentID,
		CustomerID:            e.CustomerID,
		Action:                e.Action,
		CheckoutStatus:        types.CheckoutStatus(e.CheckoutStatus),
		PaymentProvider:       e.PaymentProvider,
		CheckoutInvoiceID:     e.CheckoutInvoiceID,
		CheckoutPaymentID:     e.CheckoutPaymentID,
		Configuration:         domainCheckout.JSONBCheckoutConfiguration(e.Configuration),
		PaymentProviderConfig: (*domainCheckout.JSONBCheckoutPaymentProviderConfig)(e.PaymentProviderConfig),
		Result:                (*domainCheckout.JSONBCheckoutResult)(e.Result),
		ProviderResult:        (*domainCheckout.JSONBCheckoutProviderResult)(e.ProviderResult),
		IdempotencyKey:        e.IdempotencyKey,
		SuccessURL:            e.SuccessURL,
		FailureURL:            e.FailureURL,
		CancelURL:             e.CancelURL,
		CompletedAt:           e.CompletedAt,
		CancelledAt:           e.CancelledAt,
		FailureReason:         e.FailureReason,
		Metadata:              types.Metadata(e.Metadata),
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
	if e.ExpiresAt != nil {
		s.ExpiresAt = *e.ExpiresAt
	}
	return s
}
