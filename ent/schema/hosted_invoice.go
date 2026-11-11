package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Idx_hosted_invoice_token_invoice_live is exported so the repository can match
// pq.Error.Constraint when a second live token is inserted for an invoice.
const Idx_hosted_invoice_token_invoice_live = "idx_hosted_invoice_token_invoice_live"

// HostedInvoiceToken holds the schema definition for a public hosted invoice token.
type HostedInvoiceToken struct {
	ent.Schema
}

func (HostedInvoiceToken) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (HostedInvoiceToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Unique().
			Immutable(),
		field.String("invoice_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			NotEmpty().
			Immutable(),
		field.Time("expires_at").
			SchemaType(map[string]string{"postgres": "timestamptz"}),
		field.Time("revoked_at").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Optional().
			Nillable(),
	}
}

func (HostedInvoiceToken) Indexes() []ent.Index {
	return []ent.Index{
		// At most one live (unrevoked) token per invoice.
		index.Fields("tenant_id", "environment_id", "invoice_id").
			Unique().
			StorageKey(Idx_hosted_invoice_token_invoice_live).
			Annotations(entsql.IndexWhere("revoked_at IS NULL")),
	}
}
