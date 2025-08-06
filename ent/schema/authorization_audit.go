package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// AuthorizationAudit holds the schema definition for the AuthorizationAudit entity.
type AuthorizationAudit struct {
	ent.Schema
}

// Mixin of the AuthorizationAudit.
func (AuthorizationAudit) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

func (AuthorizationAudit) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("user_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("resource").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("action").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.Bool("allowed").
			Default(false),
		field.String("reason").
			Optional().
			SchemaType(map[string]string{
				"postgres": "text",
			}),
		field.String("ip_address").
			Optional().
			SchemaType(map[string]string{
				"postgres": "varchar(45)",
			}),
		field.String("user_agent").
			Optional().
			SchemaType(map[string]string{
				"postgres": "text",
			}),
	}
}

// Indexes of the AuthorizationAudit.
func (AuthorizationAudit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "tenant_id").
			StorageKey("idx_authorization_audit_user_tenant"),
		index.Fields("resource", "action").
			StorageKey("idx_authorization_audit_resource_action"),
		index.Fields("created_at").
			StorageKey("idx_authorization_audit_created_at"),
		index.Fields("allowed").
			StorageKey("idx_authorization_audit_allowed"),
	}
}
