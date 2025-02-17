package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// PaymentAttempt holds the schema definition for the PaymentAttempt entity.
type PaymentAttempt struct {
	ent.Schema
}

// Mixin of the PaymentAttempt.
func (PaymentAttempt) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the PaymentAttempt.
func (PaymentAttempt) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("payment_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("payment_status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),
		field.Int("attempt_number").
			SchemaType(map[string]string{
				"postgres": "integer",
			}).
			Default(1).
			Positive(),
		field.String("gateway_attempt_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Nillable(),
		field.String("error_message").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the PaymentAttempt.
func (PaymentAttempt) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("payment", Payment.Type).
			Ref("attempts").
			Field("payment_id").
			Unique().
			Required(),
	}
}

// Indexes of the PaymentAttempt.
func (PaymentAttempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("payment_id", "attempt_number").
			Unique().
			StorageKey("idx_payment_attempt_number_unique"),
		index.Fields("payment_id", "status").
			StorageKey("idx_payment_attempt_status"),
		index.Fields("gateway_attempt_id").
			StorageKey("idx_gateway_attempt").
			Annotations(entsql.IndexWhere("gateway_attempt_id IS NOT NULL")),
	}
}
