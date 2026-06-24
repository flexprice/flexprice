package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// Idx_checkout_session_idempotency_key_active is exported so the repository can
// match pq.Error.Constraint for duplicate idempotency key errors.
var Idx_checkout_session_idempotency_key_active = "idx_checkout_session_idempotency_key_active"

// CheckoutSession holds the schema definition for the CheckoutSession entity.
type CheckoutSession struct {
	ent.Schema
}

func (CheckoutSession) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (CheckoutSession) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Unique().
			Immutable(),

		field.String("customer_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			NotEmpty().
			Immutable(),

		field.String("action").
			SchemaType(map[string]string{"postgres": "varchar(30)"}).
			GoType(types.CheckoutAction("")).
			NotEmpty().
			Immutable(),

		field.String("checkout_status").
			SchemaType(map[string]string{"postgres": "varchar(20)"}).
			GoType(types.CheckoutStatus("")).
			Default(string(types.CheckoutStatusInitiated)),

		field.String("payment_provider").
			SchemaType(map[string]string{"postgres": "varchar(20)"}).
			GoType(types.CheckoutPaymentProvider("")).
			Optional().
			Nillable(),

		field.String("checkout_invoice_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Optional().
			Nillable(),

		field.String("checkout_payment_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Optional().
			Nillable(),

		field.JSON("configuration", types.CheckoutConfiguration{}).
			SchemaType(map[string]string{"postgres": "jsonb"}),

		field.JSON("result", &types.CheckoutResult{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional(),

		field.JSON("provider_result", &types.CheckoutProviderResult{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional(),

		field.String("idempotency_key").
			SchemaType(map[string]string{"postgres": "varchar(255)"}).
			Optional().
			Nillable(),

		field.Text("success_url").
			Optional().
			Nillable(),

		field.Text("failure_url").
			Optional().
			Nillable(),

		field.Text("cancel_url").
			Optional().
			Nillable(),

		field.Time("expires_at").
			Optional().
			Nillable(),

		field.Time("completed_at").
			Optional().
			Nillable(),

		field.Time("cancelled_at").
			Optional().
			Nillable(),

		field.Text("failure_reason").
			Optional().
			Nillable(),
			
		field.JSON("metadata", map[string]string{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional(),
	}
}

func (CheckoutSession) Indexes() []ent.Index {
	return []ent.Index{
		// Unique only while active (initiated|pending). After terminal state, key is reusable.
		index.Fields("tenant_id", "environment_id", "idempotency_key").
			Unique().
			StorageKey(Idx_checkout_session_idempotency_key_active).
			Annotations(entsql.IndexWhere(
				"idempotency_key IS NOT NULL AND checkout_status IN ('initiated', 'pending')")),
		// Customer history lookup
		index.Fields("tenant_id", "environment_id", "customer_id").
			StorageKey("idx_checkout_session_customer"),
		// Expiry sweep (Temporal timer is primary; this is a backstop)
		index.Fields("expires_at").
			StorageKey("idx_checkout_session_expiry").
			Annotations(entsql.IndexWhere("checkout_status IN ('initiated', 'pending')")),
	}
}
