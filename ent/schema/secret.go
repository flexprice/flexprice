package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/flexprice/flexprice/ent/schema/mixin"
)

// Secret holds the schema definition for the Secret entity.
type Secret struct {
	ent.Schema
}

// Mixin of the Secret.
func (Secret) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.BaseMixin{},
		mixin.EnvironmentMixin{},
	}
}

// Fields of the Secret.
func (Secret) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("name").
			NotEmpty().
			Comment("Display name for the secret"),
		field.String("type").
			NotEmpty().
			Comment("Type of secret: private_key, publishable_key, integration"),
		field.String("provider").
			NotEmpty().
			Comment("Provider: flexprice, stripe, etc."),
		field.String("value").
			Optional().
			Comment("For API keys: hashed full key, For integrations: empty"),
		field.String("display_id").
			Optional().
			Comment("First 8 characters of the API key or integration ID for display purposes"),
		field.Strings("permissions").
			Optional().
			Default([]string{}).
			Comment("List of permissions granted to this secret"),
		field.Time("expires_at").
			Optional().
			Nillable().
			Comment("Expiration time for the secret"),
		field.Time("last_used_at").
			Optional().
			Nillable().
			Comment("Last time this secret was used"),
		field.JSON("provider_data", map[string]string{}).
			Optional().
			Comment("Provider-specific encrypted data (for integrations)"),
		// RBAC Fields
		field.Strings("roles").
			Optional().
			Default([]string{}).
			Comment("Roles copied from user at API key creation time"),
		field.String("user_type").
			Optional().
			Default("user").
			Comment("User type copied from user at API key creation time"),
	}
}

// Edges of the Secret.
func (Secret) Edges() []ent.Edge {
	return []ent.Edge{}
}

// Indexes of the Secret.
func (Secret) Indexes() []ent.Index {
	return []ent.Index{
		// Primary query patterns
		index.Fields("type", "value", "status"), // API keys are queried by type and status
		index.Fields("tenant_id", "environment_id", "type", "status"),
		index.Fields("tenant_id", "environment_id", "provider", "status"),
	}
}
