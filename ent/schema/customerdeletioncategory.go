package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// CustomerDeletionCategory holds the schema definition for the CustomerDeletionCategory entity.
type CustomerDeletionCategory struct {
	ent.Schema
}

// Mixin of the CustomerDeletionCategory.
func (CustomerDeletionCategory) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CustomerDeletionCategory.
func (CustomerDeletionCategory) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("request_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Comment("Parent customer deletion request ID"),
		field.String("category").
			SchemaType(map[string]string{
				"postgres": "varchar(60)",
			}).
			NotEmpty().
			Comment("Touchpoint: customer_pg, events_ch, meter_usage_ch, invoice_pdfs, etc"),
		field.String("disposition").
			SchemaType(map[string]string{
				"postgres": "varchar(30)",
			}).
			NotEmpty().
			Comment("Outcome for this touchpoint: pending, purged, redacted, retained"),
		field.Text("legal_basis").
			Optional().
			Comment("Required when disposition is retained, otherwise null"),
		field.Time("verified_at").
			Optional().
			Nillable(),
		field.JSON("verification_result", map[string]interface{}{}).
			Optional().
			Comment("Actual store counts observed at verification time"),
	}
}

// Indexes of the CustomerDeletionCategory.
func (CustomerDeletionCategory) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "category").
			Unique(),
		// Backs the release gate
		index.Fields("tenant_id", "environment_id", "disposition"),
	}
}
