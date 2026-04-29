package ent

import (
	"context"
	"encoding/json"
	"time"

	flexent "github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/systemevent"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// SystemEventRepository persists rows into the system_events table.
type SystemEventRepository struct {
	client postgres.IClient
}

func NewSystemEventRepository(client postgres.IClient) *SystemEventRepository {
	return &SystemEventRepository{client: client}
}

// GetByID returns a system_events row only when id matches tenant and environment.
func (r *SystemEventRepository) GetByID(ctx context.Context, tenantID, environmentID, id string) (*flexent.SystemEvent, error) {
	return r.client.Reader(ctx).SystemEvent.Query().
		Where(
			systemevent.IDEQ(id),
			systemevent.TenantIDEQ(tenantID),
			systemevent.EnvironmentIDEQ(environmentID),
		).
		Only(ctx)
}

// ListStaleUndeliveredWebhooks returns system_events rows that were consumed but never
// delivered (no published_at / webhook_message_id), with created_at strictly before olderThan.
// Results are ordered by created_at ascending. Pass limit > 0 (caller caps page size).
func (r *SystemEventRepository) ListStaleUndeliveredWebhooks(ctx context.Context, olderThan time.Time, limit int) ([]*flexent.SystemEvent, error) {
	if limit <= 0 {
		return nil, nil
	}
	return r.client.Reader(ctx).SystemEvent.Query().
		Where(
			systemevent.WebhookMessageIDIsNil(),
			systemevent.PublishedAtIsNil(),
			systemevent.CreatedAtLT(olderThan),
			systemevent.EventNameNotNil(),
			systemevent.EventNameNEQ(""),
			systemevent.FailureCountLT(4),
		).
		Order(flexent.Asc(systemevent.FieldCreatedAt)).
		Limit(limit).
		All(ctx)
}

// OnConsumed creates a full system_events row when the consumer reads the Kafka message.
// All fields are populated from the event — only webhook_message_id and published_at are left
// empty until the webhook is actually delivered (see OnDelivered).
func (r *SystemEventRepository) OnConsumed(ctx context.Context, event *types.WebhookEvent) error {
	if event == nil || event.ID == "" {
		return nil
	}

	payloadMap, err := toPayloadMap(event.Payload)
	if err != nil {
		return err
	}

	client := r.client.Writer(ctx)
	now := time.Now().UTC()

	err = client.SystemEvent.Create().
		SetID(event.ID).
		SetTenantID(event.TenantID).
		SetEnvironmentID(event.EnvironmentID).
		SetEventName(string(event.EventName)).
		SetEntityType(string(event.EntityType)).
		SetEntityID(event.EntityID).
		SetPayload(payloadMap).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		SetCreatedBy(event.UserID).
		SetUpdatedBy(event.UserID).
		Exec(ctx)
	if err == nil {
		return nil
	}
	if !flexent.IsConstraintError(err) {
		return err
	}

	// Row already exists (created by another consumer process with stale code).
	// Overwrite entity_type / entity_id so the correct values win.
	updateQ := client.SystemEvent.UpdateOneID(event.ID).SetUpdatedAt(now)
	if event.EventName != "" {
		updateQ = updateQ.SetEventName(string(event.EventName))
	}
	if event.EntityType != "" {
		updateQ = updateQ.SetEntityType(string(event.EntityType))
	}
	if event.EntityID != "" {
		updateQ = updateQ.SetEntityID(event.EntityID)
	}
	if payloadMap != nil {
		updateQ = updateQ.SetPayload(payloadMap)
	}
	return updateQ.Exec(ctx)
}

// OnDelivered stamps webhook_message_id and published_at once the webhook has been sent.
// webhookMessageID is the Svix msg_… id; nil for native HTTP delivery.
func (r *SystemEventRepository) OnDelivered(ctx context.Context, eventID string, webhookMessageID *string) error {
	if eventID == "" {
		return nil
	}

	client := r.client.Writer(ctx)
	now := time.Now().UTC()

	return client.SystemEvent.UpdateOneID(eventID).
		SetUpdatedAt(now).
		SetNillableWebhookMessageID(webhookMessageID).
		SetPublishedAt(now).
		Exec(ctx)
}

// OnFailed records the reason a webhook delivery failed. It overwrites any
// previous failure_reason and never clears it — not even on later success.
func (r *SystemEventRepository) OnFailed(ctx context.Context, eventID, reason string) error {
	if eventID == "" {
		return nil
	}
	return r.client.Writer(ctx).SystemEvent.UpdateOneID(eventID).
		SetUpdatedAt(time.Now().UTC()).
		SetFailureReason(reason).
		AddFailureCount(1).
		Exec(ctx)
}

func toPayloadMap(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
