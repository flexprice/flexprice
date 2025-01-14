package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/system"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	workflowTypes "github.com/flexprice/flexprice/internal/types/workflow"
	"github.com/google/uuid"
	"go.temporal.io/sdk/client"
)

type SystemEventService interface {
	CreateSystemEvent(ctx context.Context, eventType system.SystemEventType, payload interface{}) error
	GetSystemEvents(ctx context.Context, workflowID string) ([]*system.Event, error)
	UpdateSystemEventStatus(ctx context.Context, eventID uuid.UUID, status string) error
	HandleSystemEvent(ctx context.Context, event *system.Event) error
}

type systemEventService struct {
	systemEventRepo system.Repository
	temporalClient  client.Client
	logger          *logger.Logger
}

func NewSystemEventService(
	systemEventRepo system.Repository,
	temporalClient client.Client,
	logger *logger.Logger,
) SystemEventService {
	return &systemEventService{
		systemEventRepo: systemEventRepo,
		temporalClient:  temporalClient,
		logger:          logger,
	}
}

func (s *systemEventService) CreateSystemEvent(ctx context.Context, eventType system.SystemEventType, payload interface{}) error {
	s.logger.Debug("creating system event",
		"event_type", eventType,
	)

	// Create new system event with pending status
	event := &system.Event{
		ID:        uuid.New(),
		Type:      eventType,
		Payload:   payload,
		Status:    system.EventStatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		CreatedBy: types.GetUserID(ctx),
		UpdatedBy: types.GetUserID(ctx),
	}

	// Save event to database
	if err := s.systemEventRepo.CreateEvent(ctx, event); err != nil {
		return fmt.Errorf("failed to create system event: %w", err)
	}

	// Handle different event types
	switch eventType {
	case system.SystemEventTypeSyncMeter:
		// Start temporal workflow for meter sync
		workflowOptions := client.StartWorkflowOptions{
			ID:        fmt.Sprintf("meter-sync-%s", event.ID.String()),
			TaskQueue: "meter-sync-queue",
		}

		_, err := s.temporalClient.ExecuteWorkflow(ctx, workflowOptions, "MeterSyncWorkflow", payload)
		if err != nil {
			// Update event status to failed
			if updateErr := s.systemEventRepo.UpdateEventStatus(ctx, event.ID, system.EventStatusFailed); updateErr != nil {
				s.logger.Error("failed to update event status",
					"event_id", event.ID,
					"error", updateErr,
				)
			}
			return fmt.Errorf("failed to start meter sync workflow: %w", err)
		}

		// Update event status to completed
		if err := s.systemEventRepo.UpdateEventStatus(ctx, event.ID, system.EventStatusCompleted); err != nil {
			s.logger.Error("failed to update event status",
				"event_id", event.ID,
				"error", err,
			)
		}

	case system.SystemEventTypeUpdateBillingPeriods:
		// Start temporal workflow for billing period updates
		workflowOptions := client.StartWorkflowOptions{
			ID:           fmt.Sprintf("billing-period-update-%s", event.ID.String()),
			TaskQueue:    "billing-period-queue",
			CronSchedule: "0 0 * * *", // Run daily at midnight
		}

		_, err := s.temporalClient.ExecuteWorkflow(ctx, workflowOptions, "UpdateBillingPeriodsWorkflow", payload)
		if err != nil {
			if updateErr := s.systemEventRepo.UpdateEventStatus(ctx, event.ID, system.EventStatusFailed); updateErr != nil {
				s.logger.Error("failed to update event status",
					"event_id", event.ID,
					"error", updateErr,
				)
			}
			return fmt.Errorf("failed to start billing period update workflow: %w", err)
		}

		if err := s.systemEventRepo.UpdateEventStatus(ctx, event.ID, system.EventStatusCompleted); err != nil {
			s.logger.Error("failed to update event status",
				"event_id", event.ID,
				"error", err,
			)
		}

	// Add more event type handlers here
	default:
		s.logger.Warn("unhandled system event type",
			"event_type", eventType,
			"event_id", event.ID,
		)
	}

	return nil
}

func (s *systemEventService) GetSystemEvents(ctx context.Context, workflowID string) ([]*system.Event, error) {
	s.logger.Debug("getting system events",
		"workflow_id", workflowID,
	)

	events, err := s.systemEventRepo.GetEvents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to get system events: %w", err)
	}

	return events, nil
}

func (s *systemEventService) UpdateSystemEventStatus(ctx context.Context, eventID uuid.UUID, status string) error {
	s.logger.Debug("updating system event status",
		"event_id", eventID,
		"status", status,
	)

	if err := s.systemEventRepo.UpdateEventStatus(ctx, eventID, status); err != nil {
		return fmt.Errorf("failed to update system event status: %w", err)
	}

	return nil
}

func (s *systemEventService) HandleSystemEvent(ctx context.Context, event *system.Event) error {
	if event.Type == system.SystemEventTypeUpdateBillingPeriods {
		// Prepare workflow payload
		payload := &workflowTypes.BillingPeriodUpdatePayload{
			EventID:  event.ID.String(),
			TenantID: event.TenantID,
			Metadata: event.Metadata,
		}

		// Start temporal workflow
		workflowOptions := client.StartWorkflowOptions{
			ID:           fmt.Sprintf("billing-period-update-%s", event.ID),
			TaskQueue:    "billing-period-queue",
			CronSchedule: "0 0 * * *", // Run daily at midnight
		}

		_, err := s.temporalClient.ExecuteWorkflow(ctx, workflowOptions, "UpdateBillingPeriodsWorkflow", payload)
		if err != nil {
			// Update event status to failed if workflow start fails
			if updateErr := s.systemEventRepo.UpdateEventStatus(ctx, event.ID, system.EventStatusFailed); updateErr != nil {
				s.logger.Error("failed to update event status",
					"event_id", event.ID,
					"error", updateErr,
				)
			}
			return fmt.Errorf("failed to start billing period update workflow: %w", err)
		}

		return nil
	}

	return fmt.Errorf("unknown system event type: %s", event.Type)
}
