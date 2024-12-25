package mixin

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
)

// BaseMixin implements the ent.Mixin for sharing base fields with package schemas.
type BaseMixin struct {
	mixin.Schema
}

// Fields of the BaseMixin.
func (BaseMixin) Fields() []ent.Field {
	return []ent.Field{
		field.String("tenant_id").
			NotEmpty().
			Immutable(),
		field.String("status").
			Default("published"),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
		field.String("created_by").
			Optional().
			Immutable(),
		field.String("updated_by").
			Optional(),
	}
}

// Hooks of the BaseMixin.
func (BaseMixin) Hooks() []ent.Hook {
	return []ent.Hook{
		// Add hooks for updating updated_at and updated_by
	}
}
