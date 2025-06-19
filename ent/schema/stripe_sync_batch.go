package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// StripeSyncBatch holds the schema definition for the StripeSyncBatch entity.
type StripeSyncBatch struct {
	ent.Schema
}

// Mixin of the StripeSyncBatch.
func (StripeSyncBatch) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the StripeSyncBatch.
func (StripeSyncBatch) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable().
			Default("customer"),
		field.String("meter_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("event_type").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			NotEmpty().
			Immutable(),
		field.Float("aggregated_quantity").
			Default(0.0),
		field.Int("event_count").
			Default(0),
		field.String("stripe_event_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),
		field.String("sync_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default("pending"),
		field.Int("retry_count").
			Default(0),
		field.Text("error_message").
			Optional(),
		field.Time("window_start").
			Immutable(),
		field.Time("window_end").
			Immutable(),
		field.Time("synced_at").
			Optional().
			Nillable(),
	}
}

// Edges of the StripeSyncBatch.
func (StripeSyncBatch) Edges() []ent.Edge {
	return nil
}

// Indexes of the StripeSyncBatch.
func (StripeSyncBatch) Indexes() []ent.Index {
	return []ent.Index{
		// Primary lookup pattern for sync operations
		index.Fields("tenant_id", "environment_id", "entity_id", "entity_type", "meter_id", "window_start", "window_end").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
		// Status-based queries for sync management
		index.Fields("tenant_id", "environment_id", "sync_status", "status"),
		// Retry management queries
		index.Fields("tenant_id", "environment_id", "sync_status", "retry_count", "status"),
		// Time-based queries for cleanup and monitoring
		index.Fields("tenant_id", "environment_id", "window_start"),
		index.Fields("tenant_id", "environment_id", "synced_at"),
		// Stripe event ID lookup for idempotency
		index.Fields("tenant_id", "environment_id", "stripe_event_id").
			Annotations(entsql.IndexWhere("stripe_event_id IS NOT NULL AND status = 'published'")),
	}
}
