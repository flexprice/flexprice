package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// InvoiceSequence holds the schema definition for the InvoiceSequence entity.
type InvoiceSequence struct {
	ent.Schema
}

// Fields of the InvoiceSequence.
func (InvoiceSequence) Fields() []ent.Field {
	return []ent.Field{
		field.String("tenant_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("environment_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional(),
		field.String("year_month").
			SchemaType(map[string]string{
				"postgres": "varchar(6)",
			}).
			NotEmpty(),
		field.String("prefix").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Optional().
			Nillable(),
		field.Int64("last_value").
			SchemaType(map[string]string{
				"postgres": "bigint",
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

// Edges of the InvoiceSequence.
func (InvoiceSequence) Edges() []ent.Edge {
	return nil
}

// Indexes of the InvoiceSequence.
func (InvoiceSequence) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "prefix", "year_month").
			Unique().
			StorageKey("idx_invoice_sequences_tenant_env_prefix_month"),
	}
}
