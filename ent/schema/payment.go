package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// Payment holds the schema definition for the Payment entity.
type Payment struct {
	ent.Schema
}

// Mixin of the Payment.
func (Payment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the Payment.
func (Payment) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("idempotency_key").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("destination_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("destination_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("payment_method_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("payment_method_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional(),
		field.String("payment_gateway").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable(),
		field.String("gateway_payment_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Nillable(),
		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty().
			Immutable(),
		field.String("payment_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.Bool("track_attempts").
			Default(false),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.Time("succeeded_at").
			Optional().
			Nillable(),
		field.Time("failed_at").
			Optional().
			Nillable(),
		field.Time("refunded_at").
			Optional().
			Nillable(),
		field.String("error_message").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			Optional().
			Nillable(),
	}
}

// Edges of the Payment.
func (Payment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("attempts", PaymentAttempt.Type),
	}
}

// Indexes of the Payment.
func (Payment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "destination_type", "destination_id", "payment_status", "status").
			StorageKey("idx_tenant_destination_status"),
		index.Fields("tenant_id", "payment_method_type", "payment_method_id", "payment_status", "status").
			StorageKey("idx_tenant_payment_method_status"),
		index.Fields("tenant_id", "payment_gateway", "gateway_payment_id").
			StorageKey("idx_tenant_gateway_payment").
			Annotations(entsql.IndexWhere("payment_gateway IS NOT NULL AND gateway_payment_id IS NOT NULL")),
	}
}
