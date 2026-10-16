package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// TenantMembership is a user's membership in a tenant (roles are per-tenant).
type TenantMembership struct {
	ent.Schema
}

func (TenantMembership) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

func (TenantMembership) Fields() []ent.Field {
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
			NotEmpty().
			Immutable(),
		field.Strings("roles").
			Optional().
			Default([]string{}),
	}
}

func (TenantMembership) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "tenant_id").
			Unique().
			StorageKey("idx_tenant_membership_user_tenant").
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
		index.Fields("tenant_id", "status").
			StorageKey("idx_tenant_membership_tenant_status"),
		index.Fields("user_id", "status").
			StorageKey("idx_tenant_membership_user_status"),
	}
}
