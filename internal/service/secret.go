package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/secret"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// SecretService defines the interface for secret business logic
type SecretService interface {
	// API Key operations
	CreateAPIKey(ctx context.Context, req *dto.CreateAPIKeyRequest) (*secret.Secret, string, error)
	ListAPIKeys(ctx context.Context, filter *types.SecretFilter) (*dto.ListSecretsResponse, error)
	Delete(ctx context.Context, id string) error

	// Integration operations
	CreateIntegration(ctx context.Context, req *dto.CreateIntegrationRequest) (*secret.Secret, error)
	ListIntegrations(ctx context.Context, filter *types.SecretFilter) (*dto.ListSecretsResponse, error)

	// Verification operations
	VerifyAPIKey(ctx context.Context, apiKey string) (*secret.Secret, error)
	getIntegrationCredentials(ctx context.Context, provider string) ([]map[string]string, error)

	ListLinkedIntegrations(ctx context.Context) ([]string, error)
}

type secretService struct {
	repo              secret.Repository
	userRepo          user.Repository
	encryptionService security.EncryptionService
	config            *config.Configuration
	logger            *logger.Logger
}

// NewSecretService creates a new secret service
func NewSecretService(
	repo secret.Repository,
	userRepo user.Repository,
	config *config.Configuration,
	logger *logger.Logger,
) SecretService {
	encryptionService, err := security.NewEncryptionService(config, logger)
	if err != nil {
		logger.Fatalw("failed to create encryption service", "error", err)
	}

	return &secretService{
		repo:              repo,
		userRepo:          userRepo,
		encryptionService: encryptionService,
		config:            config,
		logger:            logger,
	}
}

// Helper functions

// generatePrefix generates a prefix for an API key based on its type
func generatePrefix(keyType types.SecretType) string {
	switch keyType {
	case types.SecretTypePrivateKey:
		return "sk"
	case types.SecretTypePublishableKey:
		return "pk"
	default:
		return "key"
	}
}

// generateDisplayID generates a unique display ID for the secret
func generateDisplayID(apiKey string) string {
	return fmt.Sprintf("%s***%s", apiKey[:5], apiKey[len(apiKey)-2:])
}

// generateAPIKey generates a new API key
func generateAPIKey(prefix string) string {
	// Generate a ULID for the key value
	return types.GenerateUUIDWithPrefix(prefix)
}

func (s *secretService) CreateAPIKey(ctx context.Context, req *dto.CreateAPIKeyRequest) (*secret.Secret, string, error) {
	if err := req.Validate(); err != nil {
		return nil, "", err
	}

	// Generate API key
	prefix := generatePrefix(req.Type)
	apiKey := generateAPIKey(prefix)

	// Hash the entire API key for storage
	hashedKey := s.encryptionService.Hash(apiKey)

	// Set default permissions if none provided
	permissions := req.Permissions
	if len(permissions) == 0 {
		permissions = []string{"read", "write"}
	}

	// Determine which user to get roles from
	userID := req.UserID
	if userID == "" {
		// No user_id provided - use authenticated user from context
		userID = types.GetUserID(ctx)
	}

	// Default values for roles and user_type
	var roles []string
	userType := "user"

	// If we have a user_id, fetch the user and copy roles
	if userID != "" {
		user, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			s.logger.Warnw("failed to fetch user for role copying", "error", err, "user_id", userID)
			// Continue without roles - backward compatibility
			roles = []string{}
		} else if user != nil {
			roles = user.Roles
			userType = user.Type

			// If user_id was explicitly provided, verify it's a service_account
			if req.UserID != "" && userType != "service_account" {
				return nil, "", ierr.NewError("provided user_id must be a service_account").
					WithHint("Only service account user IDs can be explicitly provided").
					WithReportableDetails(map[string]interface{}{
						"user_id":   userID,
						"user_type": userType,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	} else {
		// No user ID at all - create API key with empty roles (full access)
		roles = []string{}
	}

	// Create secret entity
	secretEntity := &secret.Secret{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SECRET),
		Name:          req.Name,
		Type:          req.Type,
		EnvironmentID: types.GetEnvironmentID(ctx),
		Provider:      types.SecretProviderFlexPrice,
		Value:         hashedKey,
		DisplayID:     generateDisplayID(apiKey),
		Permissions:   permissions,
		ExpiresAt:     req.ExpiresAt,
		Roles:         roles,
		UserType:      userType,
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	// Save to repository
	if err := s.repo.Create(ctx, secretEntity); err != nil {
		return nil, "", err
	}

	return secretEntity, apiKey, nil
}

func (s *secretService) ListAPIKeys(ctx context.Context, filter *types.SecretFilter) (*dto.ListSecretsResponse, error) {
	if filter == nil {
		filter = &types.SecretFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Set type filter for API keys
	filter.Type = lo.ToPtr(types.SecretTypePrivateKey)
	filter.Provider = lo.ToPtr(types.SecretProviderFlexPrice)
	filter.Status = lo.ToPtr(types.StatusPublished)

	secrets, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	return &dto.ListSecretsResponse{
		Items:      dto.ToSecretResponseList(secrets),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *secretService) CreateIntegration(ctx context.Context, req *dto.CreateIntegrationRequest) (*secret.Secret, error) {
	// Validate required fields
	if req.Name == "" {
		return nil, ierr.NewError("validation failed: name is required").
			WithHint("Name is required").
			Mark(ierr.ErrValidation)
	}
	if len(req.Credentials) == 0 {
		return nil, ierr.NewError("validation failed: credentials are required").
			Mark(ierr.ErrValidation)
	}
	if req.Provider == types.SecretProviderFlexPrice {
		return nil, ierr.NewError("validation failed: invalid provider").
			WithHint("Invalid provider").
			Mark(ierr.ErrValidation)
	}

	// Encrypt each credential
	encryptedCreds := make(map[string]string)
	for key, value := range req.Credentials {
		encrypted, err := s.encryptionService.Encrypt(value)
		if err != nil {
			return nil, err
		}
		encryptedCreds[key] = encrypted
	}

	// Generate a display ID for the integration
	displayID := types.GenerateUUIDWithPrefix("int")[:5]

	// Create secret entity
	secretEntity := &secret.Secret{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SECRET),
		Name:          req.Name,
		Type:          types.SecretTypeIntegration,
		Provider:      req.Provider,
		Value:         "", // Empty for integrations
		DisplayID:     displayID,
		ProviderData:  encryptedCreds,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}

	// Save to repository
	if err := s.repo.Create(ctx, secretEntity); err != nil {
		return nil, err
	}

	return secretEntity, nil
}

func (s *secretService) ListIntegrations(ctx context.Context, filter *types.SecretFilter) (*dto.ListSecretsResponse, error) {
	if filter == nil {
		filter = &types.SecretFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Set type filter for integrations
	filter.Type = lo.ToPtr(types.SecretTypeIntegration)
	filter.Status = lo.ToPtr(types.StatusPublished)

	secrets, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	return &dto.ListSecretsResponse{
		Items:      dto.ToSecretResponseList(secrets),
		Pagination: types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset()),
	}, nil
}

func (s *secretService) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	return nil
}

func (s *secretService) VerifyAPIKey(ctx context.Context, apiKey string) (*secret.Secret, error) {
	if apiKey == "" {
		return nil, ierr.NewError("validation failed: API key is required").
			WithHint("API key is required").
			Mark(ierr.ErrValidation)
	}

	// Hash the entire API key for verification
	hashedKey := s.encryptionService.Hash(apiKey)

	// Get secret by value
	secretEntity, err := s.repo.GetAPIKeyByValue(ctx, hashedKey)
	if err != nil {
		return nil, ierr.NewError("invalid API key").
			WithHint("Invalid API key").
			Mark(ierr.ErrValidation)
	}

	// Check if expired
	if secretEntity.IsExpired() {
		return nil, ierr.NewError("API key has expired").
			WithHint("API key has expired").
			Mark(ierr.ErrValidation)
	}

	// Check if secret is active
	if !secretEntity.IsActive() {
		return nil, ierr.NewError("API key is not active").
			WithHint("API key is not active").
			Mark(ierr.ErrValidation)
	}

	// Check if secret is an API key
	if !secretEntity.IsAPIKey() {
		return nil, ierr.NewError("invalid API key type").
			WithHint("Invalid API key type").
			Mark(ierr.ErrValidation)
	}

	// Check if the secret has expired
	if secretEntity.ExpiresAt != nil && secretEntity.ExpiresAt.Before(time.Now()) {
		return nil, ierr.NewError("secret has expired").
			WithHint("Secret has expired").
			Mark(ierr.ErrValidation)
	}

	// Update last used timestamp
	// TODO: Uncomment this when we have a way to efficiently update the last used timestamp
	// if err := s.repo.UpdateLastUsed(ctx, secretEntity.ID); err != nil {
	// 	s.logger.Warnw("failed to update last used timestamp", "error", err)
	// }

	return secretEntity, nil
}

// getIntegrationCredentials returns all integration credentials for a provider
func (s *secretService) getIntegrationCredentials(ctx context.Context, provider string) ([]map[string]string, error) {
	filter := &types.SecretFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
		Type:        lo.ToPtr(types.SecretTypeIntegration),
		Provider:    lo.ToPtr(types.SecretProvider(provider)),
	}

	secrets, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(secrets) == 0 {
		return nil, ierr.NewError(fmt.Sprintf("%s integration not configured", provider)).
			Mark(ierr.ErrNotFound)
	}

	// Decrypt credentials for all integrations
	allCredentials := make([]map[string]string, len(secrets))
	for i, secretEntity := range secrets {
		decryptedCreds := make(map[string]string)
		for key, encryptedValue := range secretEntity.ProviderData {
			decrypted, err := s.encryptionService.Decrypt(encryptedValue)
			if err != nil {
				return nil, err
			}
			decryptedCreds[key] = decrypted
		}
		allCredentials[i] = decryptedCreds
	}

	return allCredentials, nil
}

// ListLinkedIntegrations returns a list of unique providers which have a valid linked integration secret
func (s *secretService) ListLinkedIntegrations(ctx context.Context) ([]string, error) {
	filter := &types.SecretFilter{
		QueryFilter: types.NewNoLimitPublishedQueryFilter(),
		Type:        lo.ToPtr(types.SecretTypeIntegration),
	}

	secrets, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Extract unique providers
	providerMap := make(map[string]bool)
	for _, secret := range secrets {
		providerMap[string(secret.Provider)] = true
	}

	// Convert map keys to slice
	providers := make([]string, 0, len(providerMap))
	for provider := range providerMap {
		providers = append(providers, provider)
	}

	return providers, nil
}
