package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

const (
	Idx_entity_type_entity_id_tenant_id_environment_id = "idx_entity_type_entity_id_tenant_id_environment_id"
	Idx_tax_rate_id_tenant_id_environment_id           = "idx_tax_rate_id_tenant_id_environment_id"
	Idx_entity_lookup_active                           = "idx_entity_lookup_active"
	Unique_entity_tax_mapping                          = "unique_entity_tax_mapping"
	Idx_auto_apply_lookup                              = "idx_auto_apply_lookup"
	Idx_currency_entity_lookup                         = "idx_currency_entity_lookup"
)

// TaxConfig holds the schema definition for the TaxConfig entity.
type TaxConfig struct {
	ent.Schema
}

// Mixin of the TaxConfig.
func (TaxConfig) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the TaxConfig.
func (TaxConfig) Fields() []ent.Field {
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
			Comment("Reference to the TaxRate entity"),

		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable().
			Comment("Type of entity this tax rate applies to"),

		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Comment("ID of the entity this tax rate applies to"),

		field.Int("priority").
			Default(100).
			SchemaType(map[string]string{
				"postgres": "integer",
			}).
			Comment("Priority for tax resolution (lower number = higher priority)"),

		field.Bool("auto_apply").
			Default(false).
			Comment("Whether this tax should be automatically applied"),

		field.String("currency").
			Optional().
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			Immutable().
			NotEmpty().
			Comment("Currency"),

		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Edges of the TaxConfig.
func (TaxConfig) Edges() []ent.Edge {
	return []ent.Edge{}
}

// Indexes of the TaxConfig.
func (TaxConfig) Indexes() []ent.Index {
	return []ent.Index{
		// Primary lookup: find tax assignments for entity
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "status").
			StorageKey(Idx_entity_lookup_active),

		// Tax rate reverse lookup: find entities using specific tax rate
		index.Fields("tenant_id", "environment_id", "tax_rate_id").
			StorageKey(Idx_tax_rate_id_tenant_id_environment_id),

		// Auto-apply lookup for bulk operations
		index.Fields("tenant_id", "environment_id", "auto_apply", "entity_type").
			StorageKey(Idx_auto_apply_lookup),

		// Unique constraint: prevent duplicate assignments per entity
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id", "tax_rate_id").
			StorageKey(Unique_entity_tax_mapping).
			Unique(),

		// Currency-based lookup

		index.Fields("tenant_id", "environment_id", "currency", "entity_type").
			StorageKey(Idx_currency_entity_lookup),
	}
}
