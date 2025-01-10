package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Auth holds the schema definition for the Auth entity.
type Auth struct {
	ent.Schema
}

// Fields of the Auth.
func (Auth) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("provider").
			SchemaType(map[string]string{
				"postgres": "varchar(100)",
			}).
			NotEmpty(),
		field.String("token").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default("published").
			NotEmpty(),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the Auth.
func (Auth) Edges() []ent.Edge {
	return nil
}

// Indexes of the Auth.
func (Auth) Indexes() []ent.Index {
	return nil
}
