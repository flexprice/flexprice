package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// ScheduledTask holds the schema definition for the ScheduledTask entity.
// This entity manages scheduled export tasks for various entity types to external destinations (S3, etc.)
type ScheduledTask struct {
	ent.Schema
}

// Mixin of the ScheduledTask.
func (ScheduledTask) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the ScheduledTask.
func (ScheduledTask) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("connection_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Comment("Reference to the connection (S3, etc.) to use for export"),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			GoType(types.ScheduledTaskEntityType("")).
			Comment("Entity type to export (feature_usage, customer, invoice, etc.)"),
		field.String("interval").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			GoType(types.ScheduledTaskInterval("")).
			Comment("Schedule interval (hourly, daily, weekly, monthly)"),
		field.Bool("enabled").
			Default(true).
			Comment("Whether this scheduled job is active"),
		field.JSON("job_config", &types.S3JobConfig{}).
			Optional().
			Comment("S3 job configuration (bucket, region, key_prefix, compression, encryption)"),
		field.String("temporal_schedule_id").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Optional().
			Comment("Temporal schedule ID for the recurring workflow"),
	}
}

// Indexes of the ScheduledTask.
func (ScheduledTask) Indexes() []ent.Index {
	return []ent.Index{
		// Index for finding jobs by tenant and environment
		index.Fields("tenant_id", "environment_id", "enabled"),
		// Index for finding jobs by connection
		index.Fields("connection_id", "enabled"),
		// Index for finding jobs by entity type and interval
		index.Fields("entity_type", "interval", "enabled"),
		// Index for finding jobs by connection, entity type, and status
		index.Fields("connection_id", "entity_type", "status"),
	}
}
