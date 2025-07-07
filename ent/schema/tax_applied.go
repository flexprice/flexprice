package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

const (
	Idx_entity_tax_rate_id            = "idx_entity_tax_rate_id"
	Idx_entity_tax_association_lookup = "idx_entity_tax_association_lookup"
)

// TaxApplied holds the schema definition for the TaxApplied entity.
type TaxApplied struct {
	ent.Schema
}

// Mixin of the TaxApplied.
func (TaxApplied) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the TaxApplied.
func (TaxApplied) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("tax_rate_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable().
			Comment("Reference to the TaxRate entity that was applied"),

		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable().
			Comment("Type of entity this tax was applied to (invoice, subscription, etc.)"),

		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable().
			Comment("ID of the entity this tax was applied to"),

		field.String("tax_association_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Comment("Reference to the TaxAssociation that triggered this application"),

		// Enhanced tax calculation fields
		field.Other("taxable_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(15,6)",
			}).
			Comment("Base amount on which tax was calculated"),

		field.Other("tax_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(15,6)",
			}).
			Comment("Calculated tax amount"),

		// Currency and localization
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(3)",
			}).
			NotEmpty().
			Immutable().
			Comment("Currency code (ISO 4217)"),

		field.Time("applied_at").
			Default(time.Now).
			Immutable().
			Comment("When the tax was applied"),

		// Enhanced metadata with validation
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Comment("Additional metadata for tax calculation details"),
	}
}

// Edges of the TaxApplied.
func (TaxApplied) Edges() []ent.Edge {
	return nil
}

// Indexes of the TaxApplied.
func (TaxApplied) Indexes() []ent.Index {
	return []ent.Index{
		// Primary lookup: find tax applications for entity and tax rate
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "tax_rate_id").
			Unique().
			StorageKey(Idx_entity_tax_rate_id),

		// Secondary lookup: find tax applications for entity and tax association
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id").
			StorageKey(Idx_entity_tax_association_lookup),
	}
}
