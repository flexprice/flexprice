package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/types"
)

// ConnectionService defines the interface for connection operations
type ConnectionService interface {
	CreateConnection(ctx context.Context, req dto.CreateConnectionRequest) (*dto.ConnectionResponse, error)
	GetConnection(ctx context.Context, id string) (*dto.ConnectionResponse, error)
	GetConnections(ctx context.Context, filter *types.ConnectionFilter) (*dto.ListConnectionsResponse, error)
	UpdateConnection(ctx context.Context, id string, req dto.UpdateConnectionRequest) (*dto.ConnectionResponse, error)
	DeleteConnection(ctx context.Context, id string) error
}

type connectionService struct {
	ServiceParams
	encryptionService security.EncryptionService
}

// NewConnectionService creates a new connection service
func NewConnectionService(params ServiceParams, encryptionService security.EncryptionService) ConnectionService {
	return &connectionService{
		ServiceParams:     params,
		encryptionService: encryptionService,
	}
}

// encryptMetadata encrypts the structured encrypted secret data
func (s *connectionService) encryptMetadata(encryptedSecretData types.ConnectionMetadata, providerType types.SecretProvider) (types.ConnectionMetadata, error) {
	encryptedMetadata := encryptedSecretData

	switch providerType {
	case types.SecretProviderStripe:
		if encryptedSecretData.Stripe != nil {
			encryptedPublishableKey, err := s.encryptionService.Encrypt(encryptedSecretData.Stripe.PublishableKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			encryptedSecretKey, err := s.encryptionService.Encrypt(encryptedSecretData.Stripe.SecretKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			encryptedWebhookSecret, err := s.encryptionService.Encrypt(encryptedSecretData.Stripe.WebhookSecret)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			encryptedMetadata.Stripe = &types.StripeConnectionMetadata{
				PublishableKey: encryptedPublishableKey,
				SecretKey:      encryptedSecretKey,
				WebhookSecret:  encryptedWebhookSecret,
				AccountID:      encryptedSecretData.Stripe.AccountID, // Account ID is not sensitive
			}
		}

	case types.SecretProviderS3:
		if encryptedSecretData.S3 != nil {
			encryptedAccessKeyID, err := s.encryptionService.Encrypt(encryptedSecretData.S3.AWSAccessKeyID)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			encryptedSecretAccessKey, err := s.encryptionService.Encrypt(encryptedSecretData.S3.AWSSecretAccessKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			// Encrypt session token if provided (for temporary credentials)
			var encryptedSessionToken string
			if encryptedSecretData.S3.AWSSessionToken != "" {
				encryptedSessionToken, err = s.encryptionService.Encrypt(encryptedSecretData.S3.AWSSessionToken)
				if err != nil {
					return types.ConnectionMetadata{}, err
				}
			}

			encryptedMetadata.S3 = &types.S3ConnectionMetadata{
				AWSAccessKeyID:     encryptedAccessKeyID,
				AWSSecretAccessKey: encryptedSecretAccessKey,
				AWSSessionToken:    encryptedSessionToken,
			}
		}

	case types.SecretProviderHubSpot:
		if encryptedSecretData.HubSpot != nil {
			encryptedAccessToken, err := s.encryptionService.Encrypt(encryptedSecretData.HubSpot.AccessToken)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			encryptedClientSecret, err := s.encryptionService.Encrypt(encryptedSecretData.HubSpot.ClientSecret)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			encryptedMetadata.HubSpot = &types.HubSpotConnectionMetadata{
				AccessToken:  encryptedAccessToken,
				ClientSecret: encryptedClientSecret,
				AppID:        encryptedSecretData.HubSpot.AppID, // App ID is not sensitive
			}
		}

	case types.SecretProviderChargebee:
		if encryptedSecretData.Chargebee != nil {
			encryptedAPIKey, err := s.encryptionService.Encrypt(encryptedSecretData.Chargebee.APIKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			// Encrypt webhook secret if provided
			var encryptedWebhookSecret string
			if encryptedSecretData.Chargebee.WebhookSecret != "" {
				encryptedWebhookSecret, err = s.encryptionService.Encrypt(encryptedSecretData.Chargebee.WebhookSecret)
				if err != nil {
					return types.ConnectionMetadata{}, err
				}
			}

			// Encrypt webhook username if provided
			var encryptedWebhookUsername string
			if encryptedSecretData.Chargebee.WebhookUsername != "" {
				encryptedWebhookUsername, err = s.encryptionService.Encrypt(encryptedSecretData.Chargebee.WebhookUsername)
				if err != nil {
					return types.ConnectionMetadata{}, err
				}
			}

			// Encrypt webhook password if provided
			var encryptedWebhookPassword string
			if encryptedSecretData.Chargebee.WebhookPassword != "" {
				encryptedWebhookPassword, err = s.encryptionService.Encrypt(encryptedSecretData.Chargebee.WebhookPassword)
				if err != nil {
					return types.ConnectionMetadata{}, err
				}
			}

			encryptedMetadata.Chargebee = &types.ChargebeeConnectionMetadata{
				Site:            encryptedSecretData.Chargebee.Site, // Site name is not sensitive
				APIKey:          encryptedAPIKey,
				WebhookSecret:   encryptedWebhookSecret,
				WebhookUsername: encryptedWebhookUsername,
				WebhookPassword: encryptedWebhookPassword,
			}
		}

	case types.SecretProviderRazorpay:
		if encryptedSecretData.Razorpay != nil {
			encryptedKeyID, err := s.encryptionService.Encrypt(encryptedSecretData.Razorpay.KeyID)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			encryptedSecretKey, err := s.encryptionService.Encrypt(encryptedSecretData.Razorpay.SecretKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			// Encrypt webhook secret if provided (optional)
			var encryptedWebhookSecret string
			if encryptedSecretData.Razorpay.WebhookSecret != "" {
				encryptedWebhookSecret, err = s.encryptionService.Encrypt(encryptedSecretData.Razorpay.WebhookSecret)
				if err != nil {
					return types.ConnectionMetadata{}, err
				}
			}

			encryptedMetadata.Razorpay = &types.RazorpayConnectionMetadata{
				KeyID:         encryptedKeyID,
				SecretKey:     encryptedSecretKey,
				WebhookSecret: encryptedWebhookSecret,
			}
		}

	case types.SecretProviderQuickBooks:
		if encryptedSecretData.QuickBooks == nil {
			s.Logger.Warnw("QuickBooks metadata is nil, cannot encrypt", "provider_type", providerType)
			return types.ConnectionMetadata{}, ierr.NewError("QuickBooks metadata is required").
				WithHint("QuickBooks connection requires encrypted_secret_data with client_id, client_secret, realm_id, and environment").
				Mark(ierr.ErrValidation)
		}
		// Encrypt client credentials
		encryptedClientID, err := s.encryptionService.Encrypt(encryptedSecretData.QuickBooks.ClientID)
		if err != nil {
			return types.ConnectionMetadata{}, err
		}
		encryptedClientSecret, err := s.encryptionService.Encrypt(encryptedSecretData.QuickBooks.ClientSecret)
		if err != nil {
			return types.ConnectionMetadata{}, err
		}

		// Encrypt optional auth_code if provided (for initial setup)
		var encryptedAuthCode string
		if encryptedSecretData.QuickBooks.AuthCode != "" {
			encryptedAuthCode, err = s.encryptionService.Encrypt(encryptedSecretData.QuickBooks.AuthCode)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
		}

		// Encrypt tokens if already present (for connection updates or manual token provision)
		var encryptedAccessToken, encryptedRefreshToken string
		if encryptedSecretData.QuickBooks.AccessToken != "" {
			encryptedAccessToken, err = s.encryptionService.Encrypt(encryptedSecretData.QuickBooks.AccessToken)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
		}
		if encryptedSecretData.QuickBooks.RefreshToken != "" {
			encryptedRefreshToken, err = s.encryptionService.Encrypt(encryptedSecretData.QuickBooks.RefreshToken)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
		}

		// Encrypt webhook verifier token if provided (optional, for webhook security)
		var encryptedWebhookVerifierToken string
		if encryptedSecretData.QuickBooks.WebhookVerifierToken != "" {
			encryptedWebhookVerifierToken, err = s.encryptionService.Encrypt(encryptedSecretData.QuickBooks.WebhookVerifierToken)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
		}

		encryptedMetadata.QuickBooks = &types.QuickBooksConnectionMetadata{
			ClientID:             encryptedClientID,
			ClientSecret:         encryptedClientSecret,
			AuthCode:             encryptedAuthCode,
			RedirectURI:          encryptedSecretData.QuickBooks.RedirectURI,
			AccessToken:          encryptedAccessToken,
			RefreshToken:         encryptedRefreshToken,
			RealmID:              encryptedSecretData.QuickBooks.RealmID,
			Environment:          encryptedSecretData.QuickBooks.Environment,
			IncomeAccountID:      encryptedSecretData.QuickBooks.IncomeAccountID,
			WebhookVerifierToken: encryptedWebhookVerifierToken,
		}

	case types.SecretProviderNomod:
		if encryptedSecretData.Nomod == nil {
			s.Logger.Warnw("Nomod metadata is nil, cannot encrypt", "provider_type", providerType)
			return types.ConnectionMetadata{}, ierr.NewError("Nomod metadata is required").
				WithHint("Nomod connection requires encrypted_secret_data with api_key").
				Mark(ierr.ErrValidation)
		}
		// Encrypt API key
		encryptedAPIKey, err := s.encryptionService.Encrypt(encryptedSecretData.Nomod.APIKey)
		if err != nil {
			return types.ConnectionMetadata{}, err
		}

		nomodMeta := &types.NomodConnectionMetadata{
			APIKey: encryptedAPIKey,
		}

		// Encrypt webhook secret if provided
		if encryptedSecretData.Nomod.WebhookSecret != "" {
			encryptedWebhookSecret, err := s.encryptionService.Encrypt(encryptedSecretData.Nomod.WebhookSecret)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			nomodMeta.WebhookSecret = encryptedWebhookSecret
		}

		encryptedMetadata.Nomod = nomodMeta

	default:
		// For other providers or unknown types, use generic format
		if encryptedSecretData.Generic != nil {
			encryptedData := make(map[string]interface{})
			for key, value := range encryptedSecretData.Generic.Data {
				if strValue, ok := value.(string); ok {
					encryptedValue, err := s.encryptionService.Encrypt(strValue)
					if err != nil {
						return types.ConnectionMetadata{}, err
					}
					encryptedData[key] = encryptedValue
				} else {
					encryptedData[key] = value
				}
			}
			encryptedMetadata.Generic = &types.GenericConnectionMetadata{
				Data: encryptedData,
			}
		}
	}

	return encryptedMetadata, nil
}

func (s *connectionService) CreateConnection(ctx context.Context, req dto.CreateConnectionRequest) (*dto.ConnectionResponse, error) {
	s.Logger.Debugw("creating connection",
		"name", req.Name,
		"provider_type", req.ProviderType,
	)

	// Validate the request
	if err := req.ProviderType.Validate(); err != nil {
		return nil, err
	}

	if err := req.SyncConfig.Validate(); err != nil {
		return nil, err
	}

	// Check for existing published connection with same provider, tenant, and environment
	existingFilter := &types.ConnectionFilter{
		ProviderType: req.ProviderType,
	}

	existingConnections, err := s.ConnectionRepo.List(ctx, existingFilter)
	if err != nil {
		s.Logger.Errorw("failed to check for existing connections", "error", err)
		return nil, err
	}

	// Check if there's already a published connection for this provider, tenant, and environment
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	for _, existingConn := range existingConnections {
		if existingConn.TenantID == tenantID &&
			existingConn.EnvironmentID == environmentID &&
			existingConn.ProviderType == req.ProviderType &&
			existingConn.Status == types.StatusPublished {
			return nil, ierr.NewError("connection already exists").
				WithHintf("A published connection for provider '%s' already exists in this environment", req.ProviderType).
				WithReportableDetails(map[string]interface{}{
					"provider_type":          req.ProviderType,
					"tenant_id":              tenantID,
					"environment_id":         environmentID,
					"existing_connection_id": existingConn.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
	}

	// Convert DTO to domain model
	conn := req.ToConnection()

	// Set required fields
	conn.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONNECTION)
	conn.TenantID = types.GetTenantID(ctx)
	conn.EnvironmentID = types.GetEnvironmentID(ctx)
	conn.Status = types.StatusPublished
	conn.CreatedAt = time.Now()
	conn.UpdatedAt = time.Now()
	conn.CreatedBy = types.GetUserID(ctx)
	conn.UpdatedBy = types.GetUserID(ctx)

	// Check if this is a Flexprice-managed S3 connection
	if conn.ProviderType == types.SecretProviderS3 && conn.SyncConfig != nil && conn.SyncConfig.S3 != nil && conn.SyncConfig.S3.IsFlexpriceManaged {
		s.Logger.Infow("creating flexprice-managed S3 connection",
			"tenant_id", conn.TenantID,
			"connection_id", conn.ID)

		// Validate that Flexprice config has required credentials
		if s.Config.FlexpriceS3Exports.AWSAccessKeyID == "" || s.Config.FlexpriceS3Exports.AWSSecretAccessKey == "" {
			return nil, ierr.NewError("flexprice S3 exports not configured").
				WithHint("FlexpriceS3Exports credentials are missing from configuration").
				Mark(ierr.ErrSystem)
		}

		// Inject Flexprice credentials from config
		conn.EncryptedSecretData.S3 = &types.S3ConnectionMetadata{
			AWSAccessKeyID:     s.Config.FlexpriceS3Exports.AWSAccessKeyID,
			AWSSecretAccessKey: s.Config.FlexpriceS3Exports.AWSSecretAccessKey,
			AWSSessionToken:    s.Config.FlexpriceS3Exports.AWSSessionToken,
		}

		// Set bucket and region from config
		conn.SyncConfig.S3.Bucket = s.Config.FlexpriceS3Exports.Bucket
		conn.SyncConfig.S3.Region = s.Config.FlexpriceS3Exports.Region
		conn.SyncConfig.S3.KeyPrefix = conn.TenantID // Tenant isolation via tenant_id as key prefix

		s.Logger.Infow("injected flexprice S3 credentials",
			"bucket", conn.SyncConfig.S3.Bucket,
			"region", conn.SyncConfig.S3.Region,
			"key_prefix", conn.SyncConfig.S3.KeyPrefix)
	}

	// Encrypt metadata
	s.Logger.Debugw("encrypting metadata",
		"provider_type", conn.ProviderType,
		"has_quickbooks", conn.EncryptedSecretData.QuickBooks != nil,
		"has_stripe", conn.EncryptedSecretData.Stripe != nil,
		"has_chargebee", conn.EncryptedSecretData.Chargebee != nil,
		"has_s3", conn.EncryptedSecretData.S3 != nil)
	encryptedMetadata, err := s.encryptMetadata(conn.EncryptedSecretData, conn.ProviderType)
	if err != nil {
		s.Logger.Errorw("failed to encrypt metadata", "error", err)
		return nil, err
	}
	conn.EncryptedSecretData = encryptedMetadata

	// Create the connection
	if err := s.ConnectionRepo.Create(ctx, conn); err != nil {
		s.Logger.Errorw("failed to create connection", "error", err)
		return nil, err
	}

	s.Logger.Infow("connection created successfully", "connection_id", conn.ID)

	// For QuickBooks connections with auth_code, exchange it immediately for tokens
	// OAuth 2.0 auth codes expire quickly (typically 10 minutes), so we must exchange them ASAP
	if conn.ProviderType == types.SecretProviderQuickBooks && s.IntegrationFactory != nil {
		qbIntegration, err := s.IntegrationFactory.GetQuickBooksIntegration(ctx)
		if err != nil {
			s.Logger.Errorw("failed to get QuickBooks integration after connection creation",
				"connection_id", conn.ID,
				"error", err)
			// Don't fail connection creation, but log the error
		} else {
			// Try to ensure valid access token (will exchange auth_code if present)
			if err := qbIntegration.Client.EnsureValidAccessToken(ctx); err != nil {
				s.Logger.Errorw("failed to exchange QuickBooks auth code for tokens",
					"connection_id", conn.ID,
					"error", err)
				// Don't fail connection creation, but log the error
				// User will need to re-authenticate
			} else {
				s.Logger.Infow("successfully exchanged QuickBooks auth code for tokens",
					"connection_id", conn.ID)
			}
		}
	}

	return dto.ToConnectionResponse(conn), nil
}

func (s *connectionService) GetConnection(ctx context.Context, id string) (*dto.ConnectionResponse, error) {
	s.Logger.Debugw("getting connection", "connection_id", id)

	conn, err := s.ConnectionRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get connection", "error", err, "connection_id", id)
		return nil, err
	}

	return dto.ToConnectionResponse(conn), nil
}

func (s *connectionService) GetConnections(ctx context.Context, filter *types.ConnectionFilter) (*dto.ListConnectionsResponse, error) {
	s.Logger.Debugw("getting connections", "filter", filter)

	connections, err := s.ConnectionRepo.List(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to get connections", "error", err)
		return nil, err
	}

	total, err := s.ConnectionRepo.Count(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to count connections", "error", err)
		return nil, err
	}

	responses := dto.ToConnectionResponses(connections)
	return &dto.ListConnectionsResponse{
		Connections: responses,
		Total:       total,
		Limit:       filter.GetLimit(),
		Offset:      filter.GetOffset(),
	}, nil
}

func (s *connectionService) UpdateConnection(ctx context.Context, id string, req dto.UpdateConnectionRequest) (*dto.ConnectionResponse, error) {
	s.Logger.Debugw("updating connection", "connection_id", id)

	if err := req.SyncConfig.Validate(); err != nil {
		return nil, err
	}

	// Get existing connection
	conn, err := s.ConnectionRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get connection for update", "error", err, "connection_id", id)
		return nil, err
	}

	// Update simple fields if provided
	if req.Name != "" {
		conn.Name = req.Name
	}

	// Update metadata if provided
	if req.Metadata != nil {
		conn.Metadata = req.Metadata
	}

	if req.SyncConfig != nil {
		conn.SyncConfig = req.SyncConfig
	}

	// Update encrypted_secret_data if provided (e.g., webhook_verifier_token)
	// Only process if there's actual provider-specific data (not just an empty wrapper struct)
	if req.EncryptedSecretData != nil && req.EncryptedSecretData.QuickBooks != nil {
		// Encrypt and merge the new secret data with existing data
		encryptedMetadata, err := s.encryptMetadata(*req.EncryptedSecretData, conn.ProviderType)
		if err != nil {
			s.Logger.Errorw("failed to encrypt connection metadata during update", "error", err, "connection_id", id)
			return nil, err
		}

		// Merge with existing encrypted_secret_data for QuickBooks
		// This ensures we don't overwrite existing tokens (access_token, refresh_token, etc.)
		if conn.ProviderType == types.SecretProviderQuickBooks {
			existingData := conn.EncryptedSecretData
			if existingData.QuickBooks == nil {
				existingData.QuickBooks = &types.QuickBooksConnectionMetadata{}
			}
			if encryptedMetadata.QuickBooks != nil && encryptedMetadata.QuickBooks.WebhookVerifierToken != "" {
				// Only update webhook_verifier_token, don't overwrite access_token, refresh_token, etc.
				existingData.QuickBooks.WebhookVerifierToken = encryptedMetadata.QuickBooks.WebhookVerifierToken
			}
			conn.EncryptedSecretData = existingData
		}
	}

	conn.UpdatedAt = time.Now()
	conn.UpdatedBy = types.GetUserID(ctx)

	// Update the connection
	if err := s.ConnectionRepo.Update(ctx, conn); err != nil {
		s.Logger.Errorw("failed to update connection", "error", err, "connection_id", id)
		return nil, err
	}

	s.Logger.Infow("connection updated successfully", "connection_id", conn.ID)
	return dto.ToConnectionResponse(conn), nil
}

func (s *connectionService) DeleteConnection(ctx context.Context, id string) error {
	s.Logger.Debugw("deleting connection", "connection_id", id)

	// Get existing connection
	conn, err := s.ConnectionRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get connection for deletion", "error", err, "connection_id", id)
		return err
	}

	conn.UpdatedAt = time.Now()
	conn.UpdatedBy = types.GetUserID(ctx)

	// Delete the connection
	if err := s.ConnectionRepo.Delete(ctx, conn); err != nil {
		s.Logger.Errorw("failed to delete connection", "error", err, "connection_id", id)
		return err
	}

	s.Logger.Infow("connection deleted successfully", "connection_id", conn.ID)
	return nil
}
