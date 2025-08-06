package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// RBACPolicy holds the schema definition for the RBACPolicy entity.
type RBACPolicy struct {
	ent.Schema
}

// Mixin of the RBACPolicy.
func (RBACPolicy) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

func (RBACPolicy) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("role").
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
		field.String("effect").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			Default("allow"),
	}
}

// Indexes of the RBACPolicy.
func (RBACPolicy) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "role").
			StorageKey("idx_rbac_policies_tenant_role"),
		index.Fields("resource", "action").
			StorageKey("idx_rbac_policies_resource_action"),
		index.Fields("status").
			StorageKey("idx_rbac_policies_status"),
	}
}
