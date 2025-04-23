package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/secret"
	"github.com/flexprice/flexprice/internal/cache"
	domainSecret "github.com/flexprice/flexprice/internal/domain/secret"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type secretRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts SecretQueryOptions
	cache     cache.Cache
}

func NewSecretRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainSecret.Repository {
	return &secretRepository{
		client:    client,
		log:       log,
		queryOpts: SecretQueryOptions{},
		cache:     cache,
	}
}

func (r *secretRepository) Create(ctx context.Context, s *domainSecret.Secret) error {
	span := StartRepositorySpan(ctx, "Secret", "Create", map[string]interface{}{
		"secret_id": s.ID,
		"tenant_id": s.TenantID,
		"type":      string(s.Type),
		"provider":  string(s.Provider),
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("creating secret",
		"secret_id", s.ID,
		"tenant_id", s.TenantID,
		"type", s.Type,
		"provider", s.Provider,
	)

	if s.EnvironmentID == "" {
		s.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	create := client.Secret.Create().
		SetID(s.ID).
		SetTenantID(s.TenantID).
		SetName(s.Name).
		SetType(string(s.Type)).
		SetProvider(string(s.Provider)).
		SetValue(s.Value).
		SetDisplayID(s.DisplayID).
		SetEnvironmentID(s.EnvironmentID).
		SetPermissions(s.Permissions).
		SetStatus(string(s.Status)).
		SetCreatedAt(s.CreatedAt).
		SetUpdatedAt(s.UpdatedAt).
		SetCreatedBy(s.CreatedBy).
		SetUpdatedBy(s.UpdatedBy)

	if s.ProviderData != nil {
		create.SetProviderData(s.ProviderData)
	}

	if s.ExpiresAt != nil {
		create.SetExpiresAt(*s.ExpiresAt)
	}

	if s.LastUsedAt != nil {
		create.SetLastUsedAt(*s.LastUsedAt)
	}

	secret, err := create.Save(ctx)

	if err != nil {
		if ent.IsConstraintError(err) {
			wrappedErr := ierr.WithError(err).
				WithHint("Api key with same name already exists").
				WithReportableDetails(map[string]interface{}{
					"secret_id": s.ID,
					"type":      s.Type,
					"provider":  s.Provider,
				}).
				Mark(ierr.ErrAlreadyExists)
			SetSpanError(span, wrappedErr)
			return wrappedErr
		}
		wrappedErr := ierr.WithError(err).
			WithHint("Failed to create secret").
			WithReportableDetails(map[string]interface{}{
				"secret_id": s.ID,
			}).
			Mark(ierr.ErrDatabase)
		SetSpanError(span, wrappedErr)
		return wrappedErr
	}
	*s = *domainSecret.FromEnt(secret)
	SetSpanSuccess(span)
	return nil
}

func (r *secretRepository) Get(ctx context.Context, id string) (*domainSecret.Secret, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting secret", "secret_id", id)

	s, err := client.Secret.Query().
		Where(
			secret.ID(id),
			secret.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Secret not found").
				WithReportableDetails(map[string]interface{}{
					"secret_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve secret").
			WithReportableDetails(map[string]interface{}{
				"secret_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainSecret.FromEnt(s), nil
}

func (r *secretRepository) GetAPIKeyByValue(ctx context.Context, value string) (*domainSecret.Secret, error) {
	// Generate cache key
	if cachedSecret := r.GetCache(ctx, value); cachedSecret != nil {
		return cachedSecret, nil
	}

	// Not found in cache, fetch from database
	client := r.client.Querier(ctx)

	// Get tenant ID from context, but don't require it for API key verification
	tenantID := types.GetTenantID(ctx)

	// Build query
	query := client.Secret.Query().
		Where(
			secret.Value(value),
			secret.Status(string(types.StatusPublished)),
			secret.Type(string(types.SecretTypePrivateKey)),
		)

	// Only filter by tenant ID if it's available
	if tenantID != "" {
		query = query.Where(secret.TenantID(tenantID))
	}

	s, err := query.Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Invalid API key").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to verify API key").
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain model
	result := domainSecret.FromEnt(s)
	// Store in cache for future use (default expiry)
	r.SetCache(ctx, result)
	return result, nil
}

func (r *secretRepository) UpdateLastUsed(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating last used timestamp", "secret_id", id)

	// Update without tenant ID check since this is called during API key verification
	// where we might not have the tenant ID in the context yet
	_, err := client.Secret.UpdateOneID(id).
		SetLastUsedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Secret not found").
				WithReportableDetails(map[string]interface{}{
					"secret_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update last used timestamp").
			WithReportableDetails(map[string]interface{}{
				"secret_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	r.DeleteCache(ctx, id)
	return nil
}

func (r *secretRepository) List(ctx context.Context, filter *types.SecretFilter) ([]*domainSecret.Secret, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("listing secrets")

	query := client.Secret.Query()
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	secrets, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list secrets").
			Mark(ierr.ErrDatabase)
	}

	return domainSecret.FromEntList(secrets), nil
}

func (r *secretRepository) Count(ctx context.Context, filter *types.SecretFilter) (int, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("counting secrets")

	query := client.Secret.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count secrets").
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *secretRepository) ListAll(ctx context.Context, filter *types.SecretFilter) ([]*domainSecret.Secret, error) {
	if filter == nil {
		filter = types.NewNoLimitSecretFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	return r.List(ctx, filter)
}

func (r *secretRepository) Delete(ctx context.Context, id string) error {
	// Get the secret first to invalidate cache
	secret, err := r.Get(ctx, id)
	if err != nil {
		return err
	}

	client := r.client.Querier(ctx)
	r.log.Debugw("deleting secret", "secret_id", id)

	err = client.Secret.UpdateOneID(id).
		SetStatus(string(types.StatusDeleted)).
		Exec(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete secret").
			WithReportableDetails(map[string]interface{}{
				"secret_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, secret.Value)
	return nil
}

type SecretQuery = *ent.SecretQuery

type SecretQueryOptions struct{}

func (o SecretQueryOptions) ApplyTenantFilter(ctx context.Context, query SecretQuery) SecretQuery {
	return query.Where(secret.TenantID(types.GetTenantID(ctx)))
}

func (o SecretQueryOptions) ApplyStatusFilter(query SecretQuery, status string) SecretQuery {
	if status != "" {
		return query.Where(secret.Status(status))
	}
	return query
}

func (o SecretQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query SecretQuery) SecretQuery {
	if types.GetEnvironmentID(ctx) != "" {
		return query.Where(secret.EnvironmentID(types.GetEnvironmentID(ctx)))
	}
	return query
}

func (o SecretQueryOptions) ApplyTypeFilter(query SecretQuery, secretType string) SecretQuery {
	if secretType != "" {
		return query.Where(secret.Type(secretType))
	}
	return query
}

func (o SecretQueryOptions) ApplySortFilter(query SecretQuery, field string, order string) SecretQuery {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return query
	}

	if order == "desc" {
		return query.Order(ent.Desc(fieldName))
	}
	return query.Order(ent.Asc(fieldName))
}

func (o SecretQueryOptions) ApplyPaginationFilter(query SecretQuery, limit int, offset int) SecretQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o SecretQueryOptions) GetFieldName(field string) string {
	switch field {
	case "id":
		return secret.FieldID
	case "name":
		return secret.FieldName
	case "type":
		return secret.FieldType
	case "provider":
		return secret.FieldProvider
	case "display_id":
		return secret.FieldDisplayID
	case "expires_at":
		return secret.FieldExpiresAt
	case "last_used_at":
		return secret.FieldLastUsedAt
	case "created_at":
		return secret.FieldCreatedAt
	case "updated_at":
		return secret.FieldUpdatedAt
	default:
		return ""
	}
}

func (o SecretQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.SecretFilter, query SecretQuery) SecretQuery {
	if f == nil {
		return query
	}

	// Apply type filter if specified
	if f.Type != nil {
		query = query.Where(secret.Type(string(*f.Type)))
	}

	// Apply key filter if specified
	if f.Provider != nil {
		query = query.Where(secret.Provider(string(*f.Provider)))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(secret.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(secret.CreatedAtLTE(*f.EndTime))
		}
	}

	return query
}

func (r *secretRepository) SetCache(ctx context.Context, secret *domainSecret.Secret) {
	cacheKey := cache.GenerateKey(cache.PrefixSecret, secret.Value)
	r.cache.Set(ctx, cacheKey, secret, cache.ExpiryDefaultInMemory)
}

func (r *secretRepository) GetCache(ctx context.Context, key string) *domainSecret.Secret {
	cacheKey := cache.GenerateKey(cache.PrefixSecret, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainSecret.Secret)
	}
	return nil
}

func (r *secretRepository) DeleteCache(ctx context.Context, key string) {
	cacheKey := cache.GenerateKey(cache.PrefixSecret, key)
	r.cache.Delete(ctx, cacheKey)
}
