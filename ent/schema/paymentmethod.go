package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// PaymentMethod holds the schema definition for the PaymentMethod entity.
type PaymentMethod struct {
	ent.Schema
}

func (PaymentMethod) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (PaymentMethod) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			GoType(types.PaymentMethodType("")).
			NotEmpty(),
		field.String("gateway").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			GoType(types.PaymentGatewayType("")).
			NotEmpty(),
		field.String("gateway_method_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("payment_method_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			GoType(types.PaymentMethodStatus("")).
			Default(string(types.PaymentMethodStatusActive)),
		field.Bool("is_default").
			Default(false),
		field.JSON("method_details", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

func (PaymentMethod) Edges() []ent.Edge {
	return nil
}

func (PaymentMethod) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "customer_id", "status").
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
	}
}
