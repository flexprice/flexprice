package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

type Environment struct {
	ent.Schema
}

func (Environment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

func (Environment) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Immutable(),
		field.String("name").
			NotEmpty(),
		field.String("type").
			NotEmpty(),
		field.String("slug").
			NotEmpty(),
	}
}

func (Environment) Edges() []ent.Edge {
	return nil
}
