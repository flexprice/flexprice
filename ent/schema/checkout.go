package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Checkout holds the schema definition for the Checkout entity.
type Checkout struct {
	ent.Schema
}

func (Checkout) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (Checkout) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Unique().Immutable(),
		field.String("customer_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			NotEmpty(),
		field.String("entity_type").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			GoType(types.CheckoutEntityType("")),
		field.String("entity_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}),
		field.String("checkout_action").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			GoType(types.CheckoutAction("")),
		field.String("mode").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			GoType(types.CheckoutMode("")),
		field.String("checkout_status").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			GoType(types.CheckoutStatus("")).
			Default(string(types.CheckoutStatusPending)),
		field.Other("amount", decimal.Zero).
			SchemaType(map[string]string{"postgres": "numeric(20,8)"}).
			Optional().Nillable(),
		field.String("currency").Optional(),
		field.String("provider").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			GoType(types.CheckoutProvider("")).
			Default(string(types.CheckoutProviderFlexprice)),
		field.String("provider_session_id").Optional().Nillable(),
		field.Text("checkout_url").Optional().Nillable(),
		field.Text("success_url").Optional().Nillable(),
		field.Text("cancel_url").Optional().Nillable(),
		field.JSON("configuration", map[string]interface{}{}).
			Optional().
			Comment("Deferred-operation payload (JSONB). Nil in v1."),
		field.Time("expires_at"),
		field.Time("completed_at").Optional().Nillable(),
		field.Time("cancelled_at").Optional().Nillable(),
		field.Text("failure_message").Optional().Nillable(),
	}
}

func (Checkout) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id"),
		// customer lookup — list all checkouts for a customer in a tenant/env
		index.Fields("tenant_id", "environment_id", "customer_id", "checkout_status"),
		// one in-flight checkout per (tenant, env, entity, mode); also serves payment-completion lookup
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "mode").
			Unique().
			Annotations(entsql.IndexWhere("checkout_status = 'pending'")),
		// expiry sweep — cross-tenant intentional for the cleanup cron
		index.Fields("expires_at").
			Annotations(entsql.IndexWhere("checkout_status = 'pending'")),
	}
}
