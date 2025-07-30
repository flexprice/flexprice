package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
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

// encryptMetadata encrypts the metadata map by traversing key-value pairs and only encrypting values
func (s *connectionService) encryptMetadata(metadata map[string]interface{}) (map[string]interface{}, error) {
	if metadata == nil {
		return nil, nil
	}

	encryptedMetadata := make(map[string]interface{})

	// Traverse metadata by key-value pairs
	for key, value := range metadata {
		// Serialize the value to JSON
		jsonData, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}

		// Encrypt the JSON string
		encryptedJSON, err := s.encryptionService.Encrypt(string(jsonData))
		if err != nil {
			return nil, err
		}

		// Store encrypted value with original key
		encryptedMetadata[key] = encryptedJSON
	}

	return encryptedMetadata, nil
}

// decryptMetadata decrypts the metadata map by traversing key-value pairs and only decrypting values
func (s *connectionService) decryptMetadata(metadata map[string]interface{}) (map[string]interface{}, error) {
	if metadata == nil {
		return nil, nil
	}

	decryptedMetadata := make(map[string]interface{})

	// Traverse metadata by key-value pairs
	for key, value := range metadata {
		// Check if the value is encrypted (string)
		if encryptedValue, ok := value.(string); ok {
			// Decrypt the JSON string
			decryptedJSON, err := s.encryptionService.Decrypt(encryptedValue)
			if err != nil {
				return nil, err
			}

			// Deserialize back to original type
			var decryptedValue interface{}
			if err := json.Unmarshal([]byte(decryptedJSON), &decryptedValue); err != nil {
				return nil, err
			}

			decryptedMetadata[key] = decryptedValue
		} else {
			// If value is not encrypted (for backward compatibility), keep as-is
			decryptedMetadata[key] = value
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
	if conn.Metadata != nil {
		encryptedMetadata, err := s.encryptMetadata(conn.Metadata)
		if err != nil {
			s.Logger.Errorw("failed to encrypt metadata", "error", err)
			return nil, err
		}
		conn.Metadata = encryptedMetadata
	}

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

	// Decrypt metadata
	if conn.Metadata != nil {
		decryptedMetadata, err := s.decryptMetadata(conn.Metadata)
		if err != nil {
			s.Logger.Errorw("failed to decrypt metadata", "error", err)
			return nil, err
		}
		conn.Metadata = decryptedMetadata
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

	// Decrypt metadata for all connections
	for _, conn := range connections {
		if conn.Metadata != nil {
			decryptedMetadata, err := s.decryptMetadata(conn.Metadata)
			if err != nil {
				s.Logger.Errorw("failed to decrypt metadata for connection", "error", err, "connection_id", conn.ID)
				return nil, err
			}
			conn.Metadata = decryptedMetadata
		}
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

	// Get existing connection
	conn, err := s.ConnectionRepo.Get(ctx, id)
	if err != nil {
		s.Logger.Errorw("failed to get connection for update", "error", err, "connection_id", id)
		return nil, err
	}

	// Update fields if provided
	if req.Name != "" {
		conn.Name = req.Name
	}
	if req.ProviderType != "" {
		conn.ProviderType = req.ProviderType
	}
	if req.Metadata != nil {
		encryptedMetadata, err := s.encryptMetadata(req.Metadata)
		if err != nil {
			s.Logger.Errorw("failed to encrypt metadata during update", "error", err)
			return nil, err
		}
		conn.Metadata = encryptedMetadata
	}

	conn.UpdatedAt = time.Now()
	conn.UpdatedBy = types.GetUserID(ctx)

	// Update the connection
	if err := s.ConnectionRepo.Update(ctx, conn); err != nil {
		s.Logger.Errorw("failed to update connection", "error", err, "connection_id", id)
		return nil, err
	}

	// Decrypt metadata for response
	if conn.Metadata != nil {
		decryptedMetadata, err := s.decryptMetadata(conn.Metadata)
		if err != nil {
			s.Logger.Errorw("failed to decrypt metadata after update", "error", err)
			return nil, err
		}
		conn.Metadata = decryptedMetadata
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
