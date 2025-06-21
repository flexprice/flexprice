package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Costsheet represents the core entity for tracking cost-related configurations in the billing system.
// It establishes relationships between meters (what we measure) and prices (how much we charge).
// This entity is crucial for:
// 1. Determining the cost basis for billing calculations
// 2. Mapping usage metrics to their associated prices
// 3. Supporting margin and cost analysis features
type Costsheet struct {
	ent.Schema
}

// Annotations configures the underlying database schema.
// Here we explicitly set the table name to 'costsheet' (singular form)
// to maintain consistency with our naming conventions.
func (Costsheet) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "costsheet"},
	}
}

// Mixin injects common fields into the schema.
// We include:
// 1. BaseMixin: Common fields for all entities
//   - tenant_id: Multi-tenancy support
//   - status: Record status (published, draft, etc.)
//   - created_at, updated_at: Timestamps
//   - created_by, updated_by: Audit fields
//
// 2. EnvironmentMixin: Environment segregation
//   - environment_id: Separates data by environment (prod, staging, etc.)
func (Costsheet) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},        // tenant_id, status, created_at, updated_at, created_by, updated_by
		baseMixin.EnvironmentMixin{}, // environment_id
	}
}

// Schema fields for the Costsheet entity.
func (Costsheet) Fields() []ent.Field {
	return []ent.Field{
		field.String("id"). // Primary key
					SchemaType(map[string]string{
				"postgres": "varchar(50)", // Consistent with other entity IDs
			}).
			Unique().    // Ensures uniqueness across all records
			Immutable(), // ID cannot be changed once set

		field.String("meter_id"). // References the associated meter
						SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(), // Must have a valid meter reference

		field.String("price_id"). // References the associated price
						SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(), // Must have a valid price reference
	}
}

// Edges defines the relationships between Costsheet and other entities.
// These relationships are crucial for:
// 1. Cost calculation workflows
// 2. Usage tracking
// 3. Price application
func (Costsheet) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("meter", Meter.Type). // Relationship with Meter entity
						Ref("costsheet").  // Meter can reference its costsheets
						Field("meter_id"). // Foreign key field
						Unique().          // One costsheet entry per meter
						Required(),        // Must have an associated meter

		edge.From("price", Price.Type). // Relationship with Price entity
						Ref("costsheet").  // Price can reference its costsheets
						Field("price_id"). // Foreign key field
						Unique().          // One costsheet entry per price
						Required(),        // Must have an associated price
	}
}

// Indexes improves query performance and ensures data integrity.
// We index:
// 1. tenant_id + environment_id: For fast multi-tenant, multi-environment queries
// 2. meter_id + price_id: For unique constraint and fast lookups
func (Costsheet) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id"), // Supports multi-tenant, multi-environment queries
		index.Fields("meter_id", "price_id"). // Ensures unique meter-price combinations
							Unique().                                               // No duplicate meter-price pairs allowed
							Annotations(entsql.IndexWhere("status = 'published'")), // Only enforce uniqueness for published records
	}
}
