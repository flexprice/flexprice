package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

const (
	ConnectionAssociationOwnerConnectionUnique = "idx_connection_assoc_owner_connection"
	ConnectionAssociationOwnerProviderUnique   = "idx_connection_assoc_owner_provider"
	ConnectionAssociationCustomerUnique        = "idx_connection_assoc_customer"
	connectionAssociationLookup                = "idx_connection_assoc_lookup"
)

// ConnectionAssociation links a billing entity (owner) or a customer (preference) to a connection.
type ConnectionAssociation struct {
	ent.Schema
}

func (ConnectionAssociation) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (ConnectionAssociation) Fields() []ent.Field {
	varchar := map[string]string{"postgres": "varchar(50)"}

	return []ent.Field{
		field.String("id").
			SchemaType(varchar).
			Unique().
			Immutable(),

		field.String("entity_type").
			SchemaType(varchar).
			NotEmpty().
			Immutable(),

		field.String("entity_id").
			SchemaType(varchar).
			NotEmpty().
			Immutable(),

		field.String("connection_id").
			SchemaType(varchar).
			NotEmpty().
			Immutable(),

		field.String("provider_type").
			SchemaType(varchar).
			NotEmpty().
			Immutable(),
	}
}

func (ConnectionAssociation) Edges() []ent.Edge {
	return nil
}

func (ConnectionAssociation) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "connection_id").
			Unique().
			StorageKey(ConnectionAssociationOwnerConnectionUnique).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text AND (entity_type)::text = 'billing_entity'::text)")),
		index.Fields("tenant_id", "environment_id", "entity_id", "provider_type").
			Unique().
			StorageKey(ConnectionAssociationOwnerProviderUnique).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text AND (entity_type)::text = 'billing_entity'::text)")),
		index.Fields("tenant_id", "environment_id", "entity_id").
			Unique().
			StorageKey(ConnectionAssociationCustomerUnique).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text AND (entity_type)::text = 'customer'::text)")),
		index.Fields("tenant_id", "environment_id", "entity_type", "entity_id").
			StorageKey(connectionAssociationLookup),
	}
}
