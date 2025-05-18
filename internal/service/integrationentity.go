package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/integration"
	"github.com/flexprice/flexprice/internal/types"
)

type IntegrationEntityService interface {
	CreateIntegrationEntity(ctx context.Context, req *dto.CreateIntegrationEntityRequest) (*integration.IntegrationEntity, error)
	DeleteIntegrationEntity(ctx context.Context, id string) error
	DeleteByConnectionID(ctx context.Context, connectionID string) error
	GetIntegrationEntity(ctx context.Context, id string) (*integration.IntegrationEntity, error)
}

type integrationEntityService struct {
	ServiceParams
}

func NewIntegrationEntityService(params ServiceParams) IntegrationEntityService {
	return &integrationEntityService{ServiceParams: params}
}

func (s *integrationEntityService) CreateIntegrationEntity(ctx context.Context, req *dto.CreateIntegrationEntityRequest) (*integration.IntegrationEntity, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	integrationEntity := req.ToIntegrationEntity(ctx)

	now := time.Now().UTC()
	integrationEntity.LastSyncedAt = &now

	// add sync status to the integration entity
	integrationEntity.SyncStatus = types.SyncStatusPending
	integrationEntity.SyncHistory = []integration.SyncEvent{
		{
			Action:    types.SyncEventActionCreate,
			Status:    types.SyncStatusPending,
			Timestamp: now.Unix(),
		},
	}

	err := s.IntegrationRepo.Create(ctx, integrationEntity)
	if err != nil {
		return nil, err
	}

	return integrationEntity, nil
}

func (s *integrationEntityService) DeleteIntegrationEntity(ctx context.Context, id string) error {
	// Get the entity to verify it exists before deletion
	_, err := s.IntegrationRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	// Delete the entity
	return s.IntegrationRepo.Delete(ctx, id)
}

func (s *integrationEntityService) DeleteByConnectionID(ctx context.Context, connectionID string) error {
	err := s.IntegrationRepo.DeleteByConnectionID(ctx, connectionID)
	if err != nil {
		return err
	}

	return nil
}

func (s *integrationEntityService) GetIntegrationEntity(ctx context.Context, id string) (*integration.IntegrationEntity, error) {
	// Retrieve the entity by ID
	return s.IntegrationRepo.Get(ctx, id)
}
