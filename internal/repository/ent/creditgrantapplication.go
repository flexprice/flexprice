package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	cga "github.com/flexprice/flexprice/ent/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/cache"
	domain "github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type creditGrantApplicationRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CreditGrantApplicationQueryOptions
	cache     cache.Cache
}

func NewCreditGrantApplicationRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domain.Repository {
	return &creditGrantApplicationRepository{
		client:    client,
		log:       log,
		queryOpts: CreditGrantApplicationQueryOptions{},
		cache:     cache,
	}
}

func (r *creditGrantApplicationRepository) Create(ctx context.Context, a *domain.CreditGrantApplication) error {

	r.log.Debugw("creating credit grant application",
		"application_id", a.ID,
		"tenant_id", a.TenantID,
		"credit_grant_id", a.CreditGrantID,
		"subscription_id", a.SubscriptionID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "create", map[string]interface{}{
		"application_id":  a.ID,
		"credit_grant_id": a.CreditGrantID,
		"subscription_id": a.SubscriptionID,
	})
	defer FinishSpan(span)
	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.CreditGrantApplication.Create().
		SetID(a.ID).
		SetTenantID(a.TenantID).
		SetCreditGrantID(a.CreditGrantID).
		SetSubscriptionID(a.SubscriptionID).
		SetScheduledFor(a.ScheduledFor).
		SetNillablePeriodStart(a.PeriodStart).
		SetNillablePeriodEnd(a.PeriodEnd).
		SetApplicationStatus(a.ApplicationStatus).
		SetCredits(a.Credits).
		SetApplicationReason(a.ApplicationReason).
		SetSubscriptionStatusAtApplication(a.SubscriptionStatusAtApplication).
		SetRetryCount(a.RetryCount).
		SetMetadata(a.Metadata).
		SetIdempotencyKey(a.IdempotencyKey).
		SetEnvironmentID(a.EnvironmentID).
		SetStatus(string(a.Status)).
		SetCreatedAt(a.CreatedAt).
		SetUpdatedAt(a.UpdatedAt).
		SetCreatedBy(a.CreatedBy).
		SetUpdatedBy(a.UpdatedBy).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Failed to create credit grant application").
				WithReportableDetails(map[string]any{
					"application_id":  a.ID,
					"credit_grant_id": a.CreditGrantID,
					"subscription_id": a.SubscriptionID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}

		return ierr.WithError(err).
			WithHint("Failed to create credit grant application").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *creditGrantApplicationRepository) Get(ctx context.Context, id string) (*domain.CreditGrantApplication, error) {

	r.log.Debugw("getting credit grant application", "application_id", id)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "get", map[string]interface{}{
		"application_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedApplication := r.GetCache(ctx, id); cachedApplication != nil {
		return cachedApplication, nil
	}

	client := r.client.Querier(ctx)

	application, err := client.CreditGrantApplication.Query().
		Where(
			cga.ID(id),
			cga.TenantID(types.GetTenantID(ctx)),
			cga.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Credit grant application with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"application_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get credit grant application").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	domainApplication := domain.FromEnt(application)

	// Set cache
	r.SetCache(ctx, domainApplication)
	return domainApplication, nil
}

func (r *creditGrantApplicationRepository) List(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*domain.CreditGrantApplication, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	query := client.CreditGrantApplication.Query()
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list credit grant applications").
			Mark(ierr.ErrDatabase)
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	applications, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list credit grant applications").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEntList(applications), nil
}

func (r *creditGrantApplicationRepository) Count(ctx context.Context, filter *types.CreditGrantApplicationFilter) (int, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	query := client.CreditGrantApplication.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count credit grant applications").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *creditGrantApplicationRepository) ListAll(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*domain.CreditGrantApplication, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "list_all", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.CreditGrantApplication.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	applications, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list credit grant applications").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEntList(applications), nil
}

func (r *creditGrantApplicationRepository) Update(ctx context.Context, a *domain.CreditGrantApplication) error {

	r.log.Debugw("updating credit grant application",
		"application_id", a.ID,
		"tenant_id", a.TenantID,
		"credit_grant_id", a.CreditGrantID,
		"subscription_id", a.SubscriptionID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "update", map[string]interface{}{
		"application_id":  a.ID,
		"credit_grant_id": a.CreditGrantID,
		"subscription_id": a.SubscriptionID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	_, err := client.CreditGrantApplication.Update().
		Where(
			cga.ID(a.ID),
			cga.TenantID(types.GetTenantID(ctx)),
			cga.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(a.Status)).
		SetScheduledFor(a.ScheduledFor).
		SetApplicationStatus(a.ApplicationStatus).
		SetCredits(a.Credits).
		SetSubscriptionStatusAtApplication(a.SubscriptionStatusAtApplication).
		SetRetryCount(a.RetryCount).
		SetMetadata(a.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetAppliedAt(lo.FromPtr(a.AppliedAt)).
		SetFailureReason(lo.FromPtr(a.FailureReason)).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Credit grant application with ID %s was not found", a.ID).
				WithReportableDetails(map[string]any{
					"application_id": a.ID,
				}).
				Mark(ierr.ErrNotFound)
		}

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Failed to update credit grant application due to constraint violation").
				WithReportableDetails(map[string]any{
					"application_id":  a.ID,
					"credit_grant_id": a.CreditGrantID,
					"subscription_id": a.SubscriptionID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to update credit grant application").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, a)
	return nil
}

func (r *creditGrantApplicationRepository) Delete(ctx context.Context, application *domain.CreditGrantApplication) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting credit grant application",
		"application_id", application.ID,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "delete", map[string]interface{}{
		"application_id": application.ID,
	})
	defer FinishSpan(span)

	_, err := client.CreditGrantApplication.Update().
		Where(
			cga.ID(application.ID),
			cga.TenantID(types.GetTenantID(ctx)),
			cga.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Credit grant application with ID %s was not found", application.ID).
				WithReportableDetails(map[string]any{
					"application_id": application.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete credit grant application").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, application)
	return nil
}

// This runs every 15 mins
// NOTE: THIS IS ONLY FOR CRON JOB SHOULD NOT BE USED ELSEWHERE IN OTHER WORKFLOWS
func (r *creditGrantApplicationRepository) FindAllScheduledApplications(ctx context.Context) ([]*domain.CreditGrantApplication, error) {
	span := StartRepositorySpan(ctx, "creditgrantapplication", "find_all_scheduled_applications", map[string]interface{}{})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	applications, err := client.CreditGrantApplication.Query().
		Where(
			cga.ApplicationStatusIn(
				types.ApplicationStatusPending,
				types.ApplicationStatusFailed,
			),
			cga.ScheduledForLT(time.Now().UTC()),
			cga.Status(string(types.StatusPublished)),
		).
		All(ctx)

	SetSpanSuccess(span)
	return domain.FromEntList(applications), err
}

func (r *creditGrantApplicationRepository) FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domain.CreditGrantApplication, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "find_by_idempotency_key", map[string]interface{}{
		"idempotency_key": idempotencyKey,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	application, err := client.CreditGrantApplication.Query().
		Where(
			cga.IdempotencyKey(idempotencyKey),
			cga.TenantID(types.GetTenantID(ctx)),
			cga.EnvironmentID(types.GetEnvironmentID(ctx)),
			cga.Status(string(types.StatusPublished)),
		).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			r.log.Debugw("credit grant application not found by idempotency key", "idempotency_key", idempotencyKey)
			return nil, ierr.NewErrorf("credit grant application not found by idempotency key %s", idempotencyKey).
				WithHint("credit grant application not found by idempotency key").
				WithReportableDetails(map[string]any{
					"idempotency_key": idempotencyKey,
				}).
				Mark(ierr.ErrNotFound)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to find credit grant application by idempotency key").
			WithReportableDetails(map[string]any{
				"idempotency_key": idempotencyKey,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domain.FromEnt(application), nil
}

// CreditGrantApplicationQuery type alias for better readability
type CreditGrantApplicationQuery = *ent.CreditGrantApplicationQuery

// CreditGrantApplicationQueryOptions implements BaseQueryOptions for credit grant application queries
type CreditGrantApplicationQueryOptions struct{}

func (o CreditGrantApplicationQueryOptions) ApplyTenantFilter(ctx context.Context, query CreditGrantApplicationQuery) CreditGrantApplicationQuery {
	return query.Where(cga.TenantIDEQ(types.GetTenantID(ctx)))
}

func (o CreditGrantApplicationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CreditGrantApplicationQuery) CreditGrantApplicationQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(cga.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o CreditGrantApplicationQueryOptions) ApplyStatusFilter(query CreditGrantApplicationQuery, status string) CreditGrantApplicationQuery {
	if status == "" {
		return query.Where(cga.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(cga.Status(status))
}

func (o CreditGrantApplicationQueryOptions) ApplySortFilter(query CreditGrantApplicationQuery, field string, order string) CreditGrantApplicationQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o CreditGrantApplicationQueryOptions) ApplyPaginationFilter(query CreditGrantApplicationQuery, limit int, offset int) CreditGrantApplicationQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o CreditGrantApplicationQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return cga.FieldCreatedAt
	case "updated_at":
		return cga.FieldUpdatedAt
	case "scheduled_for":
		return cga.FieldScheduledFor
	case "applied_at":
		return cga.FieldAppliedAt
	case "period_start":
		return cga.FieldPeriodStart
	case "period_end":
		return cga.FieldPeriodEnd
	case "application_status":
		return cga.FieldApplicationStatus
	case "credits":
		return cga.FieldCredits
	case "credit_grant_id":
		return cga.FieldCreditGrantID
	case "subscription_id":
		return cga.FieldSubscriptionID
	case "status":
		return cga.FieldStatus
	default:
		//unknown field
		return ""
	}
}

func (o CreditGrantApplicationQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in credit grant application query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o CreditGrantApplicationQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CreditGrantApplicationFilter, query CreditGrantApplicationQuery) (CreditGrantApplicationQuery, error) {
	if f == nil {
		return query, nil
	}

	if len(f.ApplicationIDs) > 0 {
		query = query.Where(cga.IDIn(f.ApplicationIDs...))
	}

	if len(f.CreditGrantIDs) > 0 {
		query = query.Where(cga.CreditGrantIDIn(f.CreditGrantIDs...))
	}

	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(cga.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	if f.ScheduledFor != nil {
		query = query.Where(cga.ScheduledFor(*f.ScheduledFor))
	}

	if f.AppliedAt != nil {
		query = query.Where(cga.AppliedAt(*f.AppliedAt))
	}

	if len(f.ApplicationStatuses) > 0 {
		query = query.Where(cga.ApplicationStatusIn(f.ApplicationStatuses...))
	}

	return query, nil
}

func (r *creditGrantApplicationRepository) SetCache(ctx context.Context, application *domain.CreditGrantApplication) {
	span := StartRepositorySpan(ctx, "creditgrantapplication", "set", map[string]interface{}{
		"application_id": application.ID,
		"tenant_id":      types.GetTenantID(ctx),
		"environment_id": types.GetEnvironmentID(ctx),
	})
	defer FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixCreditGrantApplication, tenantID, environmentID, application.ID)
	r.cache.Set(ctx, cacheKey, application, cache.ExpiryDefaultInMemory)

	r.log.Debugw("cache set", "key", cacheKey)
}

func (r *creditGrantApplicationRepository) GetCache(ctx context.Context, id string) *domain.CreditGrantApplication {
	span := cache.StartCacheSpan(ctx, "creditgrantapplication", "get", map[string]interface{}{
		"application_id": id,
		"tenant_id":      types.GetTenantID(ctx),
		"environment_id": types.GetEnvironmentID(ctx),
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixCreditGrantApplication, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), id)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if application, ok := value.(*domain.CreditGrantApplication); ok {
			r.log.Debugw("cache hit", "key", cacheKey)
			return application
		}
	}
	return nil
}

func (r *creditGrantApplicationRepository) DeleteCache(ctx context.Context, application *domain.CreditGrantApplication) {
	span := cache.StartCacheSpan(ctx, "creditgrantapplication", "delete", map[string]interface{}{
		"application_id": application.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixCreditGrantApplication, tenantID, environmentID, application.ID)
	r.cache.Delete(ctx, cacheKey)
	r.log.Debugw("cache deleted", "key", cacheKey)
}
