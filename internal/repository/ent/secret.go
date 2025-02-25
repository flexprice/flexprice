package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/secret"
	domainSecret "github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type secretRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts SecretQueryOptions
}

func NewSecretRepository(client postgres.IClient, log *logger.Logger) domainSecret.Repository {
	return &secretRepository{
		client:    client,
		log:       log,
		queryOpts: SecretQueryOptions{},
	}
}

func (r *secretRepository) Create(ctx context.Context, s *domainSecret.Secret) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating secret",
		"secret_id", s.ID,
		"tenant_id", s.TenantID,
		"type", s.Type,
		"provider", s.Provider,
	)

	create := client.Secret.Create().
		SetID(s.ID).
		SetTenantID(s.TenantID).
		SetName(s.Name).
		SetType(string(s.Type)).
		SetProvider(string(s.Provider)).
		SetValue(s.Value).
		SetDisplayID(s.DisplayID).
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
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to create secret")
	}

	*s = *domainSecret.FromEnt(secret)
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
			return nil, errors.Wrap(err, errors.ErrCodeNotFound, "secret not found")
		}
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to get secret")
	}

	return domainSecret.FromEnt(s), nil
}

func (r *secretRepository) VerifySecret(ctx context.Context, value string) (*domainSecret.Secret, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("verifying secret")

	s, err := client.Secret.Query().
		Where(
			secret.Value(value),
			secret.TenantID(types.GetTenantID(ctx)),
			secret.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.Wrap(err, errors.ErrCodeNotFound, "invalid secret")
		}
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to verify secret")
	}

	// Check if the secret has expired
	if s.ExpiresAt != nil && s.ExpiresAt.Before(time.Now()) {
		return nil, errors.New(errors.ErrCodeInvalidOperation, "secret has expired")
	}

	return domainSecret.FromEnt(s), nil
}

func (r *secretRepository) UpdateLastUsed(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating last used timestamp", "secret_id", id)

	_, err := client.Secret.UpdateOneID(id).
		SetLastUsedAt(time.Now().UTC()).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.Wrap(err, errors.ErrCodeNotFound, "secret not found")
		}
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to update last used timestamp")
	}

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
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to list secrets")
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
		return 0, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to count secrets")
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
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	return r.List(ctx, filter)
}

func (r *secretRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting secret", "secret_id", id)

	err := client.Secret.UpdateOneID(id).
		SetStatus(string(types.StatusDeleted)).
		Exec(ctx)

	if err != nil {
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to delete secret")
	}

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

func (o SecretQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.SecretFilter, query SecretQuery) SecretQuery {
	if f == nil {
		return query
	}

	// Apply feature IDs filter if specified
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
