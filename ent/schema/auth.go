package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Auth holds the schema definition for the Auth entity.
type Auth struct {
	ent.Schema
}

func (Auth) Fields() []ent.Field {
	return []ent.Field{
		field.String("user_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable(),
		field.String("provider").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty(),
		field.String("token").
			SchemaType(map[string]string{
				"postgres": "text",
			}).
			NotEmpty(),
		field.String("status").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			Default("published"),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the Auth.
func (Auth) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id").
			Unique().
			StorageKey("idx_auth_user_id_unique").
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
		index.Fields("provider").
			StorageKey("idx_auth_provider"),
		index.Fields("status").
			StorageKey("idx_auth_status"),
		index.Fields("created_at").
			StorageKey("idx_auth_created_at"),
	}
}
