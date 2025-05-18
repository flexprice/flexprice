package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domainConnection "github.com/flexprice/flexprice/internal/domain/connection"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// ConnectionService defines the interface for connection business logic
type ConnectionService interface {
	Create(ctx context.Context, req *dto.CreateConnectionRequest) (*domainConnection.Connection, error)
	Get(ctx context.Context, id string) (*domainConnection.Connection, error)
	GetByConnectionCode(ctx context.Context, connectionCode string) (*domainConnection.Connection, error)
	GetByProviderType(ctx context.Context, providerType types.SecretProvider) ([]*domainConnection.Connection, error)
	List(ctx context.Context, filter *types.ConnectionFilter) (*dto.ListConnectionsResponse, error)
	Update(ctx context.Context, id string, req *dto.UpdateConnectionRequest) (*domainConnection.Connection, error)
	Delete(ctx context.Context, id string) error
}

type connectionService struct {
	ServiceParams
}

// NewConnectionService creates a new connection service
func NewConnectionService(
	params ServiceParams,
) ConnectionService {
	return &connectionService{
		ServiceParams: params,
	}
}

func (s *connectionService) Create(ctx context.Context, req *dto.CreateConnectionRequest) (*domainConnection.Connection, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Create integration secret first
	integrationReq := &dto.CreateIntegrationRequest{
		Name:        req.Name,
		Provider:    req.ProviderType,
		Credentials: req.Credentials,
	}

	secretService := NewSecretService(s.SecretRepo, s.Config, s.Logger)
	secret, err := secretService.CreateIntegration(ctx, integrationReq)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create integration secret for connection").
			Mark(ierr.ErrSystem)
	}

	// Create connection entity
	connection := req.ToConnection(ctx, secret.ID)

	// Create connection in repository
	if err := s.ConnectionRepo.Create(ctx, connection); err != nil {
		// If connection creation fails, attempt to delete the created secret
		// to prevent orphaned secrets
		_ = s.SecretRepo.Delete(ctx, secret.ID)
		return nil, err
	}

	return connection, nil
}

func (s *connectionService) Get(ctx context.Context, id string) (*domainConnection.Connection, error) {
	connection, err := s.ConnectionRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func (s *connectionService) GetByConnectionCode(ctx context.Context, connectionCode string) (*domainConnection.Connection, error) {
	connection, err := s.ConnectionRepo.GetByConnectionCode(ctx, connectionCode)
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func (s *connectionService) GetByProviderType(ctx context.Context, providerType types.SecretProvider) ([]*domainConnection.Connection, error) {
	connections, err := s.ConnectionRepo.GetByProviderType(ctx, providerType)
	if err != nil {
		return nil, err
	}
	return connections, nil
}

func (s *connectionService) List(ctx context.Context, filter *types.ConnectionFilter) (*dto.ListConnectionsResponse, error) {
	if filter == nil {
		filter = &types.ConnectionFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	connections, err := s.ConnectionRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	count, err := s.ConnectionRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	pagination := types.NewPaginationResponse(count, filter.GetLimit(), filter.GetOffset())

	return &dto.ListConnectionsResponse{
		Items:      dto.ToConnectionResponseList(connections),
		Pagination: &pagination,
	}, nil
}

func (s *connectionService) Update(ctx context.Context, id string, req *dto.UpdateConnectionRequest) (*domainConnection.Connection, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Get the existing connection
	connection, err := s.ConnectionRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields
	if req.Name != "" {
		connection.Name = req.Name
	}
	if req.ConnectionCode != "" {
		connection.ConnectionCode = req.ConnectionCode
	}
	if req.Metadata != nil {
		connection.Metadata = req.Metadata
	}

	// Update the timestamps
	connection.UpdatedAt = time.Now().UTC()
	connection.UpdatedBy = types.GetUserID(ctx)

	// Update in repository
	if err := s.ConnectionRepo.Update(ctx, connection); err != nil {
		return nil, err
	}

	return connection, nil
}

func (s *connectionService) Delete(ctx context.Context, id string) error {
	// First get the connection to retrieve the secret ID
	connection, err := s.ConnectionRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete the connection
	if err := s.ConnectionRepo.Delete(ctx, id); err != nil {
		return err
	}

	// Delete the associated secret
	if connection.SecretID != "" {
		if err := s.SecretRepo.Delete(ctx, connection.SecretID); err != nil {
			s.Logger.Warnw("failed to delete secret associated with connection",
				"connection_id", id,
				"secret_id", connection.SecretID,
				"error", err)
			// We don't return an error here as the connection was successfully deleted
		}
	}

	return nil
}
