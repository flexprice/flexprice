package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
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

		// Null when an external engine calculated the tax: there is no Flexprice rate
		// to point at. Postgres does not collide null values in the unique index below,
		// so any number of external rows coexist on one entity.
		field.String("tax_rate_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Immutable().
			Comment("Reference to the TaxRate entity that was applied. Null for external engine rows"),

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

		field.String("idempotency_key").
			Optional().
			Nillable().
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Comment("Idempotency key for the tax application"),

		field.String("tax_behavior").
			GoType(types.TaxBehavior("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Comment("inclusive or exclusive, frozen at apply time"),

		field.String("provider").
			GoType(types.TaxProvider("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Comment("Engine that produced this row. Null or empty means the native engine"),

		field.String("tax_transaction_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Nillable().
			Comment("Provider transaction recording the filed tax, stamped once at commit"),

		field.String("tax_transaction_type").
			GoType(types.TaxTransactionType("")).
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Comment("What the provider transaction did. Null or empty means a filing"),

		field.JSON("external_tax_details", &types.ExternalTaxDetails{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Comment("What the external engine returned about this tax. Null for native rows"),
	}
}

// Edges of the TaxApplied.
func (TaxApplied) Edges() []ent.Edge {
	return nil
}

// Indexes of the TaxApplied.
func (TaxApplied) Indexes() []ent.Index {
	return []ent.Index{
		// Primary lookup: find tax applications for entity and tax rate.
		// Archived rows are excluded, so a recalculation can archive the current set and
		// write a new one without colliding with what earlier recalculations left behind.
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "tax_rate_id").
			Unique().
			StorageKey(Idx_entity_tax_rate_id).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),

		// Secondary lookup: find tax applications for entity and tax association
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "status").
			StorageKey(Idx_entity_tax_association_lookup),
	}
}
