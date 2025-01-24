package ent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/systemevent"
	"github.com/flexprice/flexprice/internal/domain/system"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/google/uuid"
)

type systemEventsRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewSystemEventRepository(client postgres.IClient, log *logger.Logger) system.Repository {
	return &systemEventsRepository{
		client: client,
		log:    log,
	}
}

func (r *systemEventsRepository) CreateEvent(ctx context.Context, event *system.Event) error {
	client := r.client.Querier(ctx)

	r.log.Debug("creating system event",
		"event_id", event.ID,
		"event_type", event.Type,
	)

	payloadMap, ok := event.Payload.(map[string]interface{})
	if !ok {
		payloadBytes, err := json.Marshal(event.Payload)
		if err != nil {
			return errors.WithOp(err, "repository.systemEvent.Create.MarshalPayload")
		}

		payloadMap = make(map[string]interface{})
		if err := json.Unmarshal(payloadBytes, &payloadMap); err != nil {
			return errors.WithOp(err, "repository.systemEvent.Create.UnmarshalPayload")
		}
	}

	_, err := client.SystemEvent.Create().
		SetID(event.ID).
		SetType(string(event.Type)).
		SetPayload(payloadMap).
		SetStatus(event.Status).
		SetCreatedAt(event.CreatedAt).
		SetUpdatedAt(event.UpdatedAt).
		SetCreatedBy(event.CreatedBy).
		SetUpdatedBy(event.UpdatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create system event: %w", err)
	}

	return nil
}

func (r *systemEventsRepository) GetEvents(ctx context.Context, workflowID string) ([]*system.Event, error) {
	client := r.client.Querier(ctx)

	r.log.Debug("getting system events",
		"workflow_id", workflowID,
	)

	events, err := client.SystemEvent.Query().
		Where(
			systemevent.WorkflowID(workflowID),
		).
		Order(ent.Desc(systemevent.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to get system events: %w", err)
	}

	result := make([]*system.Event, len(events))
	for i, event := range events {
		result[i] = toDomainEvent(event)
	}

	return result, nil
}

func (r *systemEventsRepository) UpdateEventStatus(ctx context.Context, eventID uuid.UUID, status string) error {
	client := r.client.Querier(ctx)

	r.log.Debug("updating system event status",
		"event_id", eventID,
		"status", status,
	)

	_, err := client.SystemEvent.UpdateOneID(eventID).
		SetStatus(status).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("system event not found: %s", eventID)
		}
		return fmt.Errorf("failed to update system event status: %w", err)
	}

	return nil
}

func toDomainEvent(event *ent.SystemEvent) *system.Event {
	return &system.Event{
		ID:        event.ID,
		Type:      system.SystemEventType(event.Type),
		Payload:   event.Payload,
		Status:    event.Status,
		CreatedAt: event.CreatedAt,
		UpdatedAt: event.UpdatedAt,
		CreatedBy: event.CreatedBy,
		UpdatedBy: event.UpdatedBy,
	}
}
