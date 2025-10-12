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

// decryptMetadata decrypts the structured encrypted secret data
func (s *connectionService) decryptMetadata(encryptedSecretData types.ConnectionMetadata, providerType types.SecretProvider) (types.ConnectionMetadata, error) {
	decryptedMetadata := encryptedSecretData

	switch providerType {
	case types.SecretProviderStripe:
		if encryptedSecretData.Stripe != nil {
			decryptedPublishableKey, err := s.encryptionService.Decrypt(encryptedSecretData.Stripe.PublishableKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			decryptedSecretKey, err := s.encryptionService.Decrypt(encryptedSecretData.Stripe.SecretKey)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}
			decryptedWebhookSecret, err := s.encryptionService.Decrypt(encryptedSecretData.Stripe.WebhookSecret)
			if err != nil {
				return types.ConnectionMetadata{}, err
			}

			decryptedMetadata.Stripe = &types.StripeConnectionMetadata{
				PublishableKey: decryptedPublishableKey,
				SecretKey:      decryptedSecretKey,
				WebhookSecret:  decryptedWebhookSecret,
				AccountID:      encryptedSecretData.Stripe.AccountID, // Account ID is not sensitive
			}
		}

	default:
		// For other providers or unknown types, use generic format
		if encryptedSecretData.Generic != nil {
			decryptedData := make(map[string]interface{})
			for key, value := range encryptedSecretData.Generic.Data {
				if strValue, ok := value.(string); ok {
					decryptedValue, err := s.encryptionService.Decrypt(strValue)
					if err != nil {
						return types.ConnectionMetadata{}, err
					}
					decryptedData[key] = decryptedValue
				} else {
					decryptedData[key] = value
				}
			}
			decryptedMetadata.Generic = &types.GenericConnectionMetadata{
				Data: decryptedData,
			}
		}
	}

	return decryptedMetadata, nil
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

	// Encrypt metadata
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
