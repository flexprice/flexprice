package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// ContractAssociation links a contract to the entities it covers.
type ContractAssociation struct {
	ent.Schema
}

const (
	ContractAssociationUniqueConstraint = "idx_contract_association_unique"
)

// Mixin of the ContractAssociation.
func (ContractAssociation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the ContractAssociation.
func (ContractAssociation) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),

		field.String("contract_id").
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

// Indexes of the ContractAssociation.
func (ContractAssociation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id").
			Unique().
			StorageKey(ContractAssociationUniqueConstraint).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
		index.Fields("tenant_id", "environment_id", "contract_id"),
	}
}
