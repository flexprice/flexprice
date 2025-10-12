package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

type AlertLogs struct {
	ent.Schema
}

// Mixin of the Settings.
func (AlertLogs) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.BaseMixin{},
		mixin.EnvironmentMixin{},
	}
}

func (AlertLogs) Fields() []ent.Field {
	return []ent.Field{
		// ID of the alert
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		// All fields are immutable since alerts are point-in-time records

		// Type of entity being monitored (wallet, entitlement, etc)
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		// ID of the entity being monitored
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		// Type of alert (credit_balance, ongoing_balance, etc)
		field.String("alert_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		// Status of alert (ok or in_alarm)
		field.String("alert_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),

		// JSONB field storing alert information like threshold, value_at_time, timestamp
		field.JSON("alert_info", types.AlertInfo{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Immutable(),
	}
}

// Edges of the AlertLogs.
func (AlertLogs) Edges() []ent.Edge {
	return nil
}

// Indexes of the AlertLogs.
func (AlertLogs) Indexes() []ent.Index {
	return []ent.Index{
		// Index for querying alerts by entity (most common query)
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id").
			StorageKey("idx_alertlogs_entity"),
		// Index for querying alerts by type
		index.Fields("tenant_id", "environment_id", "alert_type").
			StorageKey("idx_alertlogs_type"),
		// Index for querying alerts by creation time (for latest alerts)
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "created_at").
			StorageKey("idx_alertlogs_entity_created_at"),
	}
}
