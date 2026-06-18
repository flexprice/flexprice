package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WebhookRequest holds the schema for inbound webhook request audit logs.
type WebhookRequest struct {
	ent.Schema
}

func (WebhookRequest) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Unique().
			Immutable(),
		field.String("tenant_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			NotEmpty().
			Immutable(),
		field.String("environment_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			NotEmpty().
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
		field.Time("created_at").
			Immutable().
			Default(time.Now),
	}
}

func (WebhookRequest) Edges() []ent.Edge {
	return nil
}

func (WebhookRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id").
			StorageKey("idx_webhook_requests_tenant_env"),
		index.Fields("provider").
			StorageKey("idx_webhook_requests_provider"),
		index.Fields("request_id").
			StorageKey("idx_webhook_requests_request_id"),
		index.Fields("created_at").
			StorageKey("idx_webhook_requests_created_at"),
	}
}
