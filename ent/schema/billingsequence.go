package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BillingSequence holds the schema definition for the BillingSequence entity.
type BillingSequence struct {
	ent.Schema
}

// Fields of the BillingSequence.
func (BillingSequence) Fields() []ent.Field {
	return []ent.Field{
		field.String("tenant_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.Int("last_sequence").
			SchemaType(map[string]string{
				"postgres": "integer",
			}).
			Default(0),
		field.Time("created_at").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}).
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}).
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the BillingSequence.
func (BillingSequence) Edges() []ent.Edge {
	return nil
}

// Indexes of the BillingSequence.
func (BillingSequence) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "subscription_id").
			Unique(),
	}
}
