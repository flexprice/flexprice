package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/flexprice/flexprice/ent/schema/mixin"
)

type SystemEvent struct {
	ent.Schema
}

func (SystemEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.BaseMixin{},
		mixin.EnvironmentMixin{},
	}
}

func (SystemEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("event_name").
			SchemaType(map[string]string{
				"postgres": "varchar(128)",
			}).
			Optional().
			Default(""),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(64)",
			}).
			Optional().
			Default(""),
		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Default(""),
		field.String("webhook_message_id").
			SchemaType(map[string]string{
				"postgres": "varchar(128)",
			}).
			Optional().
			Nillable(),
		field.Time("published_at").
			Optional().
			Nillable(),
		field.JSON("payload", map[string]interface{}{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Optional(),
	}
}

func (SystemEvent) Edges() []ent.Edge {
	return nil
}

func (SystemEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id").
			StorageKey("idx_system_events_tenant_env"),
	}
}
