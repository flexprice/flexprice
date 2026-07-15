package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/secret"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemorySecretStore implements secret.Repository
type InMemorySecretStore struct {
	*InMemoryStore[*secret.Secret]
}

// NewInMemorySecretStore creates a new in-memory secret store
func NewInMemorySecretStore() *InMemorySecretStore {
	return &InMemorySecretStore{
		InMemoryStore: NewInMemoryStore[*secret.Secret](),
	}
}

// secretFilterFn implements filtering logic for secrets
func secretFilterFn(ctx context.Context, s *secret.Secret, filter interface{}) bool {
	if s == nil {
		return false
	}

	filter_, ok := filter.(*types.SecretFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if s.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, s.EnvironmentID) {
		return false
	}

	// Filter by status
	if filter_.GetStatus() != "" && string(s.Status) != filter_.GetStatus() {
		return false
	}

	// Filter by type
	if filter_.Type != nil && string(s.Type) != string(*filter_.Type) {
		return false
	}

	// Filter by provider
	if filter_.Provider != nil && string(s.Provider) != string(*filter_.Provider) {
		return false
	}

	// Filter by user_id
	if filter_.UserID != nil && s.UserID != *filter_.UserID {
		return false
	}

	// Filter by not-expired: expires_at IS NULL OR expires_at > NotExpiredAt
	if filter_.NotExpiredAt != nil {
		if s.ExpiresAt != nil && !s.ExpiresAt.After(*filter_.NotExpiredAt) {
			return false
		}
	}

	// Filter by time range
	if filter_.TimeRangeFilter != nil {
		if filter_.StartTime != nil && s.CreatedAt.Before(*filter_.StartTime) {
			return false
		}
		if filter_.EndTime != nil && s.CreatedAt.After(*filter_.EndTime) {
			return false
		}
	}

	return true
}

// secretSortFn implements sorting logic for secrets
func secretSortFn(i, j *secret.Secret) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemorySecretStore) Create(ctx context.Context, secret *secret.Secret) error {
	if secret == nil {
		return ierr.NewError("secret cannot be nil").
			WithHint("Please provide a valid secret").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if secret.EnvironmentID == "" {
		secret.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, secret.ID, secret)
}

func (s *InMemorySecretStore) Get(ctx context.Context, id string) (*secret.Secret, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemorySecretStore) List(ctx context.Context, filter *types.SecretFilter) ([]*secret.Secret, error) {
	return s.InMemoryStore.List(ctx, filter, secretFilterFn, secretSortFn)
}

func (s *InMemorySecretStore) Count(ctx context.Context, filter *types.SecretFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, secretFilterFn)
}

func (s *InMemorySecretStore) ListAll(ctx context.Context, filter *types.SecretFilter) ([]*secret.Secret, error) {
	if filter == nil {
		filter = types.NewNoLimitSecretFilter()
	}

	unlimitedFilter := &types.SecretFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		TimeRangeFilter: filter.TimeRangeFilter,
		Type:            filter.Type,
		Provider:        filter.Provider,
		Prefix:          filter.Prefix,
		UserID:          filter.UserID,
		NotExpiredAt:    filter.NotExpiredAt,
	}
	if filter.QueryFilter != nil && filter.QueryFilter.Status != nil {
		unlimitedFilter.QueryFilter.Status = filter.QueryFilter.Status
	}

	return s.List(ctx, unlimitedFilter)
}

func (s *InMemorySecretStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemorySecretStore) DeletePublishedByUserID(ctx context.Context, userID string) (int, error) {
	if userID == "" {
		return 0, ierr.NewError("user ID is required").
			WithHint("Provide a valid user ID").
			Mark(ierr.ErrValidation)
	}

	filter := &types.SecretFilter{
		QueryFilter: &types.QueryFilter{
			Status: lo.ToPtr(types.StatusPublished),
		},
		UserID: &userID,
	}
	secrets, err := s.ListAll(ctx, filter)
	if err != nil {
		return 0, err
	}

	for _, sec := range secrets {
		sec.Status = types.StatusDeleted
		if err := s.InMemoryStore.Update(ctx, sec.ID, sec); err != nil {
			return 0, err
		}
	}
	return len(secrets), nil
}

func (s *InMemorySecretStore) GetAPIKeyByValue(ctx context.Context, value string) (*secret.Secret, error) {
	secrets, err := s.ListAll(ctx, &types.SecretFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	})
	if err != nil {
		return nil, err
	}

	for _, secret := range secrets {
		if secret.Value == value && secret.Status == types.StatusPublished && secret.Type == types.SecretTypePrivateKey {
			return secret, nil
		}
	}

	return nil, ierr.NewError("invalid secret").
		WithHint("Invalid secret").
		WithReportableDetails(map[string]interface{}{
			"value": value,
		}).
		Mark(ierr.ErrNotFound)
}

func (s *InMemorySecretStore) UpdateLastUsed(ctx context.Context, id string) error {
	secret, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	secret.LastUsedAt = &now
	return s.InMemoryStore.Update(ctx, id, secret)
}

// Clear clears the secret store
func (s *InMemorySecretStore) Clear() {
	s.InMemoryStore.Clear()
}
