package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// PerformanceObligationAssociation links a performance obligation to the entities it covers.
type PerformanceObligationAssociation struct {
	ent.Schema
}

const (
	PerformanceObligationAssociationUniqueConstraint = "idx_pob_association_unique"
)

// Mixin of the PerformanceObligationAssociation.
func (PerformanceObligationAssociation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the PerformanceObligationAssociation.
func (PerformanceObligationAssociation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("pob_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),

		field.String("entity_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			NotEmpty(),
	}
}

// Indexes of the PerformanceObligationAssociation.
func (PerformanceObligationAssociation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id").
			Unique().
			StorageKey(PerformanceObligationAssociationUniqueConstraint).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
		// default name exceeds the 63-character limit
		index.Fields("tenant_id", "environment_id", "pob_id").
			StorageKey("idx_pob_association_pob_id"),
	}
}
