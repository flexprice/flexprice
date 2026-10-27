package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// BillingEntity holds the schema definition for a legal entity that bills customers.
type BillingEntity struct {
	ent.Schema
}

const (
	BillingEntityDefaultUniqueConstraint = "idx_billing_entity_tenant_env_default"
	BillingEntityPrefixUniqueConstraint  = "idx_billing_entity_tenant_env_prefix"
)

func (BillingEntity) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (BillingEntity) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	jsonb := map[string]string{"postgres": "jsonb"}

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),

		field.String("name").
			SchemaType(varchar("255")).
			NotEmpty(),

		field.String("legal_name").
			SchemaType(varchar("255")).
			NotEmpty(),

		field.Bool("is_default").
			Default(false).
			Immutable(),

		field.String("invoice_number_prefix").
			SchemaType(varchar("20")).
			NotEmpty(),

		field.JSON("seller_details", types.SellerDetails{}).
			SchemaType(jsonb).
			Default(types.SellerDetails{}),

		field.JSON("tax_identifiers", []types.TaxIdentifier{}).
			SchemaType(jsonb).
			Default([]types.TaxIdentifier{}),

		field.JSON("metadata", map[string]string{}).
			SchemaType(jsonb).
			Optional(),
	}
}

func (BillingEntity) Edges() []ent.Edge {
	return nil
}

func (BillingEntity) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id").
			Unique().
			StorageKey(BillingEntityDefaultUniqueConstraint).
			Annotations(entsql.IndexWhere("(is_default AND ((status)::text = 'published'::text))")),
		index.Fields("tenant_id", "environment_id", "invoice_number_prefix").
			Unique().
			StorageKey(BillingEntityPrefixUniqueConstraint).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
	}
}
