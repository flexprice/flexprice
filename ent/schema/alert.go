package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// Alert holds the schema definition for the Alert entity.
type Alert struct {
	ent.Schema
}

// Mixin of the Alert.
func (Alert) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Alert.
func (Alert) Fields() []ent.Field {
	return []ent.Field{
		// ID of the alert
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		// Type of entity being monitored (wallet, entitlement, etc)
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),

		// ID of the entity being monitored
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),

		// Metric being monitored (credit_balance, ongoing_balance, etc)
		field.String("alert_metric").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),

		// Current state of the alert (ok, in_alarm)
		field.String("alert_state").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Default(string(types.AlertStateOk)),

		// Whether the alert is enabled
		field.Bool("alert_enabled").
			Default(true),

		// JSONB field storing alert information like threshold, value_at_time
		field.JSON("alert_data", map[string]interface{}{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Optional(),
	}
}

// Edges of the Alert.
func (Alert) Edges() []ent.Edge {
	return nil
}

// Indexes of the Alert.
func (Alert) Indexes() []ent.Index {
	return []ent.Index{
		// Index for querying alerts by entity and metric
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "alert_metric").
			StorageKey("idx_alerts_tenant_env_entity_metric"),
		// Index for querying alerts by state
		index.Fields("tenant_id", "environment_id", "alert_state").
			StorageKey("idx_alerts_tenant_environment_state"),
		// Index for querying enabled/disabled alerts
		index.Fields("tenant_id", "environment_id", "alert_enabled").
			StorageKey("idx_alerts_tenant_environment_enabled"),
		// Index for querying alerts by entity, metric and state
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "alert_metric", "alert_state").
			StorageKey("idx_alerts_tenant_env_entity_metric_state"),
	}
}
