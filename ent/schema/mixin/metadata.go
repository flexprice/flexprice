package mixin

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
)

// MetadataMixin implements the ent.Mixin for sharing metadata field with package schemas.
type MetadataMixin struct {
	mixin.Schema
}

// Fields of the MetadataMixin.
func (MetadataMixin) Fields() []ent.Field {
	return []ent.Field{
		// Metadata as JSON field
		field.JSON("metadata", map[string]string{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}).
			Optional(),
	}
}

// Hooks of the MetadataMixin.
func (MetadataMixin) Hooks() []ent.Hook {
	return []ent.Hook{
		// Add hooks if needed
	}
}
