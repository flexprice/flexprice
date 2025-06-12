package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/creditgrantapplication"
	"github.com/flexprice/flexprice/internal/cache"
	domainCreditGrantApplication "github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type creditGrantApplicationRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CreditGrantApplicationQueryOptions
	cache     cache.Cache
}

func NewCreditGrantApplicationRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCreditGrantApplication.Repository {
	return &creditGrantApplicationRepository{
		client:    client,
		log:       log,
		queryOpts: CreditGrantApplicationQueryOptions{},
		cache:     cache,
	}
}

func (r *creditGrantApplicationRepository) Create(ctx context.Context, application *domainCreditGrantApplication.CreditGrantApplication) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating credit grant application",
		"application_id", application.ID,
		"tenant_id", application.TenantID,
		"credit_grant_id", application.CreditGrantID,
		"subscription_id", application.SubscriptionID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "create", map[string]interface{}{
		"application_id":  application.ID,
		"credit_grant_id": application.CreditGrantID,
		"subscription_id": application.SubscriptionID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if application.EnvironmentID == "" {
		application.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	createBuilder := client.CreditGrantApplication.Create().
		SetID(application.ID).
		SetTenantID(application.TenantID).
		SetCreditGrantID(application.CreditGrantID).
		SetSubscriptionID(application.SubscriptionID).
		SetScheduledFor(application.ScheduledFor).
		SetPeriodStart(application.PeriodStart).
		SetPeriodEnd(application.PeriodEnd).
		SetApplicationStatus(string(application.ApplicationStatus)).
		SetCreditsApplied(application.CreditsApplied).
		SetCurrency(application.Currency).
		SetApplicationReason(application.ApplicationReason).
		SetSubscriptionStatusAtApplication(application.SubscriptionStatusAtApplication).
		SetIsProrated(application.IsProrated).
		SetRetryCount(application.RetryCount).
		SetMetadata(application.Metadata).
		SetStatus(string(application.Status)).
		SetCreatedAt(application.CreatedAt).
		SetUpdatedAt(application.UpdatedAt).
		SetCreatedBy(application.CreatedBy).
		SetUpdatedBy(application.UpdatedBy).
		SetIdempotencyKey(application.IdempotencyKey).
		SetEnvironmentID(application.EnvironmentID)

	// Set optional fields
	if application.AppliedAt != nil {
		createBuilder = createBuilder.SetAppliedAt(*application.AppliedAt)
	}
	if application.ProrationFactor != nil {
		createBuilder = createBuilder.SetProrationFactor(*application.ProrationFactor)
	}
	if application.FullPeriodAmount != nil {
		createBuilder = createBuilder.SetFullPeriodAmount(*application.FullPeriodAmount)
	}
	if application.FailureReason != nil {
		createBuilder = createBuilder.SetFailureReason(*application.FailureReason)
	}
	if application.NextRetryAt != nil {
		createBuilder = createBuilder.SetNextRetryAt(*application.NextRetryAt)
	}

	savedApplication, err := createBuilder.Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Failed to create credit grant application").
				WithReportableDetails(map[string]any{
					"application_id":  application.ID,
					"credit_grant_id": application.CreditGrantID,
					"subscription_id": application.SubscriptionID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create credit grant application").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*application = *domainCreditGrantApplication.FromEnt(savedApplication)
	return nil
}

func (r *creditGrantApplicationRepository) Get(ctx context.Context, id string) (*domainCreditGrantApplication.CreditGrantApplication, error) {
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
	r.log.Debugw("getting credit grant application", "application_id", id)

	application, err := client.CreditGrantApplication.Query().
		Where(
			creditgrantapplication.ID(id),
			creditgrantapplication.TenantID(types.GetTenantID(ctx)),
			creditgrantapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
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
	domainApplication := domainCreditGrantApplication.FromEnt(application)

	// Set cache
	r.SetCache(ctx, domainApplication)
	return domainApplication, nil
}

func (r *creditGrantApplicationRepository) List(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*domainCreditGrantApplication.CreditGrantApplication, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

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
	return domainCreditGrantApplication.FromEntList(applications), nil
}

func (r *creditGrantApplicationRepository) Count(ctx context.Context, filter *types.CreditGrantApplicationFilter) (int, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

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

func (r *creditGrantApplicationRepository) ListAll(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*domainCreditGrantApplication.CreditGrantApplication, error) {
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
	return domainCreditGrantApplication.FromEntList(applications), nil
}

func (r *creditGrantApplicationRepository) Update(ctx context.Context, application *domainCreditGrantApplication.CreditGrantApplication) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating credit grant application",
		"application_id", application.ID,
		"tenant_id", application.TenantID,
		"credit_grant_id", application.CreditGrantID,
		"subscription_id", application.SubscriptionID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "update", map[string]interface{}{
		"application_id":  application.ID,
		"credit_grant_id": application.CreditGrantID,
		"subscription_id": application.SubscriptionID,
	})
	defer FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	_, err := client.CreditGrantApplication.Update().
		Where(
			creditgrantapplication.ID(application.ID),
			creditgrantapplication.TenantID(tenantID),
			creditgrantapplication.EnvironmentID(environmentID),
		).
		SetStatus(string(application.Status)).
		SetScheduledFor(application.ScheduledFor).
		SetPeriodStart(application.PeriodStart).
		SetPeriodEnd(application.PeriodEnd).
		SetApplicationStatus(string(application.ApplicationStatus)).
		SetCreditsApplied(application.CreditsApplied).
		SetCurrency(application.Currency).
		SetApplicationReason(application.ApplicationReason).
		SetSubscriptionStatusAtApplication(application.SubscriptionStatusAtApplication).
		SetIsProrated(application.IsProrated).
		SetRetryCount(application.RetryCount).
		SetMetadata(application.Metadata).
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
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Failed to update credit grant application due to constraint violation").
				WithReportableDetails(map[string]any{
					"application_id":  application.ID,
					"credit_grant_id": application.CreditGrantID,
					"subscription_id": application.SubscriptionID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to update credit grant application").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, application)
	return nil
}

func (r *creditGrantApplicationRepository) Delete(ctx context.Context, application *domainCreditGrantApplication.CreditGrantApplication) error {
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
			creditgrantapplication.ID(application.ID),
			creditgrantapplication.TenantID(types.GetTenantID(ctx)),
			creditgrantapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
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

// CreditGrantApplicationQuery type alias for better readability
type CreditGrantApplicationQuery = *ent.CreditGrantApplicationQuery

// CreditGrantApplicationQueryOptions implements BaseQueryOptions for credit grant application queries
type CreditGrantApplicationQueryOptions struct{}

func (o CreditGrantApplicationQueryOptions) ApplyTenantFilter(ctx context.Context, query CreditGrantApplicationQuery) CreditGrantApplicationQuery {
	return query.Where(creditgrantapplication.TenantIDEQ(types.GetTenantID(ctx)))
}

func (o CreditGrantApplicationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CreditGrantApplicationQuery) CreditGrantApplicationQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(creditgrantapplication.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o CreditGrantApplicationQueryOptions) ApplyStatusFilter(query CreditGrantApplicationQuery, status string) CreditGrantApplicationQuery {
	if status == "" {
		return query.Where(creditgrantapplication.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(creditgrantapplication.Status(status))
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
		return creditgrantapplication.FieldCreatedAt
	case "updated_at":
		return creditgrantapplication.FieldUpdatedAt
	case "scheduled_for":
		return creditgrantapplication.FieldScheduledFor
	case "applied_at":
		return creditgrantapplication.FieldAppliedAt
	case "period_start":
		return creditgrantapplication.FieldPeriodStart
	case "period_end":
		return creditgrantapplication.FieldPeriodEnd
	case "application_status":
		return creditgrantapplication.FieldApplicationStatus
	case "credits_applied":
		return creditgrantapplication.FieldCreditsApplied
	case "currency":
		return creditgrantapplication.FieldCurrency
	case "credit_grant_id":
		return creditgrantapplication.FieldCreditGrantID
	case "subscription_id":
		return creditgrantapplication.FieldSubscriptionID
	case "status":
		return creditgrantapplication.FieldStatus
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
		query = query.Where(creditgrantapplication.IDIn(f.ApplicationIDs...))
	}

	if len(f.CreditGrantIDs) > 0 {
		query = query.Where(creditgrantapplication.CreditGrantIDIn(f.CreditGrantIDs...))
	}

	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(creditgrantapplication.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	if f.ScheduledFor != nil {
		query = query.Where(creditgrantapplication.ScheduledFor(*f.ScheduledFor))
	}

	if f.AppliedAt != nil {
		query = query.Where(creditgrantapplication.AppliedAt(*f.AppliedAt))
	}

	if f.ApplicationStatus != "" {
		query = query.Where(creditgrantapplication.ApplicationStatus(string(f.ApplicationStatus)))
	}

	return query, nil
}

func (r *creditGrantApplicationRepository) ExistsForPeriod(ctx context.Context, grantID, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	client := r.client.Querier(ctx)

	count, err := client.CreditGrantApplication.Query().
		Where(
			creditgrantapplication.CreditGrantID(grantID),
			creditgrantapplication.SubscriptionID(subscriptionID),
			creditgrantapplication.PeriodStart(periodStart),
			creditgrantapplication.PeriodEnd(periodEnd),
			creditgrantapplication.TenantID(types.GetTenantID(ctx)),
			creditgrantapplication.ApplicationStatusNotIn(
				string(types.ApplicationStatusCancelled),
				string(types.ApplicationStatusFailed),
			),
		).
		Count(ctx)

	return count > 0, err
}

// This runs every 15 mins
// NOTE: THIS IS ONLY FOR CRON JOB SHOULD NOT BE USED ELSEWHERE IN OTHER WORKFLOWS
func (r *creditGrantApplicationRepository) FindAllScheduledApplications(ctx context.Context) ([]*domainCreditGrantApplication.CreditGrantApplication, error) {
	span := cache.StartCacheSpan(ctx, "creditgrantapplication", "find_all_scheduled_applications", map[string]interface{}{})
	defer cache.FinishSpan(span)

	client := r.client.Querier(ctx)

	applications, err := client.CreditGrantApplication.Query().
		Where(
			creditgrantapplication.ApplicationStatusIn(
				string(types.ApplicationStatusScheduled),
				string(types.ApplicationStatusPending),
				string(types.ApplicationStatusFailed),
			),
			// TODO: Rethink this and have a better way to handle this
			creditgrantapplication.ScheduledForLT(time.Now().UTC()),
		).
		All(ctx)

	return domainCreditGrantApplication.FromEntList(applications), err
}

func (r *creditGrantApplicationRepository) FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*domainCreditGrantApplication.CreditGrantApplication, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrantapplication", "find_by_idempotency_key", map[string]interface{}{
		"idempotency_key": idempotencyKey,
	})
	defer FinishSpan(span)

	application, err := client.CreditGrantApplication.Query().
		Where(
			creditgrantapplication.IdempotencyKey(idempotencyKey),
			creditgrantapplication.TenantID(types.GetTenantID(ctx)),
			creditgrantapplication.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, nil // Not found, return nil without error
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to find credit grant application by idempotency key").
			WithReportableDetails(map[string]any{
				"idempotency_key": idempotencyKey,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCreditGrantApplication.FromEnt(application), nil
}

func (r *creditGrantApplicationRepository) SetCache(ctx context.Context, application *domainCreditGrantApplication.CreditGrantApplication) {
	span := cache.StartCacheSpan(ctx, "creditgrantapplication", "set", map[string]interface{}{
		"application_id": application.ID,
		"tenant_id":      types.GetTenantID(ctx),
		"environment_id": types.GetEnvironmentID(ctx),
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixCreditGrantApplication, tenantID, environmentID, application.ID)
	r.cache.Set(ctx, cacheKey, application, cache.ExpiryDefaultInMemory)

	r.log.Debugw("cache set", "key", cacheKey)
}

func (r *creditGrantApplicationRepository) GetCache(ctx context.Context, id string) *domainCreditGrantApplication.CreditGrantApplication {
	span := cache.StartCacheSpan(ctx, "creditgrantapplication", "get", map[string]interface{}{
		"application_id": id,
		"tenant_id":      types.GetTenantID(ctx),
		"environment_id": types.GetEnvironmentID(ctx),
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixCreditGrantApplication, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), id)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if application, ok := value.(*domainCreditGrantApplication.CreditGrantApplication); ok {
			r.log.Debugw("cache hit", "key", cacheKey)
			return application
		}
	}
	return nil
}

func (r *creditGrantApplicationRepository) DeleteCache(ctx context.Context, application *domainCreditGrantApplication.CreditGrantApplication) {
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
