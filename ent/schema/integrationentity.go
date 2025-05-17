// Package schema defines the ent schema for all entities.
package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// IntegrationEntity schema defines the connection between FlexPrice entities and external systems.
type IntegrationEntity struct {
	ent.Schema
}

// Mixin defines the mixins for the IntegrationEntity schema.
func (IntegrationEntity) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields defines the fields for the IntegrationEntity schema.
func (IntegrationEntity) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			GoType(types.EntityType("")).
			Comment("Type of entity being connected (e.g., customer, payment)"),
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Comment("ID of the FlexPrice entity"),
		field.String("provider_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			GoType(types.SecretProvider("")).
			Comment("Type of external provider (e.g., stripe, razorpay)"),
		field.String("provider_id").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Optional().
			Comment("ID of the entity in the external system"),
		field.String("sync_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			GoType(types.SyncStatus("")).
			Default(string(types.SyncStatusPending)).
			Comment("Synchronization status (pending, synced, failed)"),
		field.Time("last_synced_at").
			Optional().
			Comment("Timestamp of the last successful sync"),
		field.String("last_error_msg").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			Optional().
			Comment("Message from the last sync error"),
		field.JSON("sync_history", []SyncEvent{}).
			Default([]SyncEvent{}).
			Comment("History of sync events"),
		field.JSON("metadata", map[string]string{}).
			Default(map[string]string{}).
			Comment("Additional metadata for the connection"),
	}
}

// Indexes defines the indexes for the IntegrationEntity schema.
func (IntegrationEntity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "entity_type", "entity_id", "provider_type").
			Unique().
			Annotations(entsql.IndexWhere("status != 'deleted'")),
		index.Fields("tenant_id", "provider_type", "provider_id").
			Annotations(entsql.IndexWhere("status != 'deleted'")),
		index.Fields("tenant_id", "sync_status"),
	}
}

// SyncEvent represents a single synchronization event.
type SyncEvent struct {
	Action    string           `json:"action"`              // "create", "update", "delete"
	Status    types.SyncStatus `json:"status"`              // "success", "failed"
	Timestamp int64            `json:"timestamp"`           // Unix timestamp
	ErrorMsg  *string          `json:"error_msg,omitempty"` // Error message if status is "failed"
}
