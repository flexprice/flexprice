package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// IncomingWebhookEvent holds the schema for inbound webhook request audit logs.
type IncomingWebhookEvent struct {
	ent.Schema
}

func (IncomingWebhookEvent) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (IncomingWebhookEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Unique().
			Immutable(),
		field.String("provider").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			NotEmpty().
			Immutable(),
		field.String("method").
			SchemaType(map[string]string{"postgres": "varchar(10)"}).
			NotEmpty().
			Immutable(),
		field.String("path").
			SchemaType(map[string]string{"postgres": "text"}).
			NotEmpty().
			Immutable(),
		field.String("request_id").
			SchemaType(map[string]string{"postgres": "varchar(100)"}).
			Optional().
			Immutable(),
		field.JSON("headers", map[string][]string{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional().
			Immutable(),
		field.Text("body").
			Optional().
			Immutable(),
	}
}

func (IncomingWebhookEvent) Edges() []ent.Edge {
	return nil
}

func (IncomingWebhookEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "provider", "created_at").
			StorageKey("idx_incoming_webhook_events_tenant_env_provider_created"),
		index.Fields("tenant_id", "environment_id", "created_at").
			StorageKey("idx_incoming_webhook_events_tenant_env_created"),
		index.Fields("request_id").
			StorageKey("idx_incoming_webhook_events_request_id"),
	}
}
