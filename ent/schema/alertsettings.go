package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// AlertSettings holds the schema definition for the alert_settings entity.
// One row per monitored entity: subscription spend (Part A), subscription line item
// spend (Part B), or group spend (Part C). See ERD subscription-spend-notifications.md.
type AlertSettings struct {
	ent.Schema
}

// Mixin of the AlertSettings.
func (AlertSettings) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.BaseMixin{},
		mixin.EnvironmentMixin{},
	}
}

func (AlertSettings) Fields() []ent.Field {
	return []ent.Field{
		// ID of the alert setting
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		// Denormalized from config.alert_enabled for indexed filtering
		field.Bool("enabled").
			Default(true),

		// Type of entity being monitored. Bound to the full AlertEntityType set so wallet/entitlement
		// alerts can join this table later without a migration; the CRUD narrows what's creatable
		// today. No SchemaType needed — the generated enum validator already bounds the values.
		field.Enum("entity_type").
			Values(string(types.AlertEntityTypeSubscription), string(types.AlertEntityTypeSubscriptionLineItem), string(types.AlertEntityTypeGroup)).
			Immutable(),

		// ID of the entity being monitored
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		// Parent entity type (optional) - "subscription" for line-item and group rows, NULL for
		// subscription rows. Shares entity_type's binding and wide value set for the same reason.
		field.Enum("parent_entity_type").
			Values(string(types.AlertEntityTypeSubscription)).
			Optional().
			Nillable().
			Immutable(),

		// Parent entity ID (optional) - subscription.id for line-item and group rows, NULL for subscription rows
		field.String("parent_entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable(),

		// JSONB field storing the threshold configuration (types.AlertSettings)
		field.JSON("config", types.AlertSettings{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the AlertSettings.
func (AlertSettings) Edges() []ent.Edge {
	return nil
}

// Indexes of the AlertSettings.
func (AlertSettings) Indexes() []ent.Index {
	return []ent.Index{
		// Part A lookup and general list/CRUD
		index.Fields("tenant_id", "environment_id", "status", "enabled", "entity_type", "entity_id").
			StorageKey("idx_alert_settings_entity"),
		// Part B and Part C lookups ("enabled configs whose parent is this subscription")
		index.Fields("tenant_id", "environment_id", "status", "enabled", "entity_type", "parent_entity_type", "parent_entity_id").
			StorageKey("idx_alert_settings_parent"),
	}
}
