package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/integration"
	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// EntitySyncService handles entity synchronization with external systems
type EntitySyncService interface {
	// Sync an entity immediately (synchronous)
	SyncEntity(ctx context.Context, entityType string, entityID string, provider string) error

	// Queue an entity for synchronization (asynchronous)
	QueueEntitySync(ctx context.Context, entityType string, entityID string, provider string) error

	// Process sync queue (for background workers)
	ProcessSyncQueue(ctx context.Context) error

	// Retry failed syncs
	RetryFailedSyncs(ctx context.Context, limit int) error
}

// entitySyncService implements the EntitySyncService interface
type entitySyncService struct {
	ServiceParams
}

// NewEntitySyncService creates a new entity sync service
func NewEntitySyncService(params ServiceParams) EntitySyncService {
	return &entitySyncService{
		ServiceParams: params,
	}
}

// SyncEntity synchronizes an entity with an external system
func (s *entitySyncService) SyncEntity(ctx context.Context, entityType string, entityID string, provider string) error {
	// Get the entity
	entity, err := s.getEntityByType(ctx, entityType, entityID)
	if err != nil {
		return err
	}

	// Get the integration gateway
	gatewayService := NewGatewayService(s.ServiceParams)
	gateway, err := gatewayService.GetGateway(ctx, provider)
	if err != nil {
		return err
	}

	// Check capability support
	capability := types.IntegrationCapability(entityType)
	if !gateway.SupportsCapability(capability) {
		return ierr.NewError(fmt.Sprintf("%s does not support %s synchronization", provider, entityType)).
			WithHint(fmt.Sprintf("The %s provider does not support %s synchronization", provider, entityType)).
			Mark(ierr.ErrInvalidOperation)
	}

	// Get existing connection or create new one
	connection, err := s.ConnectionRepo.GetByEntityAndProvider(ctx, entityType, entityID, provider)
	isNew := err != nil && ierr.IsNotFound(err)
	if isNew {
		connection = &integration.IntegrationEntity{
			ID:           types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CONNECTION),
			EntityType:   entityType,
			EntityID:     entityID,
			ProviderType: provider,
			SyncStatus:   "pending",
			SyncHistory:  []integration.SyncEvent{},
			Metadata:     types.Metadata{},
			BaseModel:    types.GetDefaultBaseModel(ctx),
		}
	} else if err != nil {
		return err
	}

	// Perform sync based on entity type
	var providerID string
	syncErr := error(nil)
	action := "create"

	if !isNew && connection.ProviderID != "" {
		action = "update"
	}

	// Perform the appropriate action based on entity type
	switch entityType {
	case "customer":
		if isNew || connection.ProviderID == "" {
			providerID, syncErr = gateway.CreateCustomer(ctx, entity.(*customer.Customer))
		} else {
			syncErr = gateway.UpdateCustomer(ctx, entity.(*customer.Customer), connection.ProviderID)
			providerID = connection.ProviderID
		}
	case "payment":
		if isNew || connection.ProviderID == "" {
			providerID, syncErr = gateway.CreatePayment(ctx, entity.(*payment.Payment), nil)
		} else {
			// Currently only read-only after creation
			providerID = connection.ProviderID
		}
	default:
		return ierr.NewError(fmt.Sprintf("unsupported entity type: %s", entityType)).
			WithHint(fmt.Sprintf("Entity type %s is not supported for synchronization", entityType)).
			Mark(ierr.ErrInvalidOperation)
	}

	// Update connection record
	now := time.Now().UTC()
	timestamp := now.Unix()
	syncEvent := integration.SyncEvent{
		Action:    action,
		Status:    "success",
		Timestamp: timestamp,
		ErrorMsg:  nil,
	}

	if syncErr != nil {
		errorMsg := syncErr.Error()
		syncEvent.Status = "failed"
		syncEvent.ErrorMsg = &errorMsg
		connection.SyncStatus = "failed"
		connection.LastErrorMsg = &errorMsg
	} else {
		connection.SyncStatus = "synced"
		connection.ProviderID = providerID
		connection.LastSyncedAt = &now
	}

	connection.SyncHistory = append(connection.SyncHistory, syncEvent)

	// Save or update the connection
	if isNew {
		err = s.ConnectionRepo.Create(ctx, connection)
	} else {
		err = s.ConnectionRepo.Update(ctx, connection)
	}

	if err != nil {
		return err
	}

	// Return the sync error if any
	return syncErr
}

// QueueEntitySync queues an entity for asynchronous synchronization
func (s *entitySyncService) QueueEntitySync(ctx context.Context, entityType string, entityID string, provider string) error {
	// For now, we'll just do synchronous processing
	// In the future, this will queue a task in a background worker system
	return s.SyncEntity(ctx, entityType, entityID, provider)
}

// ProcessSyncQueue processes the queue of entities to be synchronized
func (s *entitySyncService) ProcessSyncQueue(ctx context.Context) error {
	// This will be implemented when we add asynchronous processing
	// For now, it's a no-op
	return nil
}

// RetryFailedSyncs retries synchronization for entities that failed
func (s *entitySyncService) RetryFailedSyncs(ctx context.Context, limit int) error {
	filter := &integration.IntegrationEntityFilter{
		SyncStatus: "failed",
	}

	if limit > 0 {
		filter.QueryFilter = &types.QueryFilter{
			Limit: lo.ToPtr(limit),
		}
	}

	connections, err := s.ConnectionRepo.List(ctx, filter)
	if err != nil {
		return err
	}

	for _, conn := range connections {
		s.Logger.Infow("retrying failed sync",
			"entity_type", conn.EntityType,
			"entity_id", conn.EntityID,
			"provider", conn.ProviderType)

		err := s.SyncEntity(ctx, conn.EntityType, conn.EntityID, conn.ProviderType)
		if err != nil {
			s.Logger.Errorw("retry failed",
				"error", err,
				"entity_type", conn.EntityType,
				"entity_id", conn.EntityID)
		}
	}

	return nil
}

// getEntityByType retrieves an entity by its type and ID
func (s *entitySyncService) getEntityByType(ctx context.Context, entityType string, entityID string) (interface{}, error) {
	switch entityType {
	case "customer":
		return s.CustomerRepo.Get(ctx, entityID)
	case "payment":
		return s.PaymentRepo.Get(ctx, entityID)
	default:
		return nil, ierr.NewError(fmt.Sprintf("unsupported entity type: %s", entityType)).
			WithHint(fmt.Sprintf("Entity type %s is not supported for synchronization", entityType)).
			Mark(ierr.ErrInvalidOperation)
	}
}
