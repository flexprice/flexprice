package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// ActivityLog mirrors the partitioned activity_logs table for typed reads.
// The table is created by a versioned migration; ent must not manage it.
type ActivityLog struct {
	ent.Schema
}

func (ActivityLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Skip()}
}

func (ActivityLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("tenant_id").Immutable(),
		field.String("environment_id").Immutable(),
		field.String("category").Default("business"),
		field.String("entity_type"),
		field.String("entity_id"),
		field.String("entity_label").Default(""),
		field.String("action"),
		field.String("actor_type"),
		field.String("actor_id"),
		field.String("actor_label").Default(""),
		field.String("actor_user_id").Optional().Nillable(),
		field.String("source"),
		field.String("customer_id").Optional().Nillable(),
		field.String("subscription_id").Optional().Nillable(),
		field.String("request_id").Optional().Nillable(),
		field.String("outcome").Default("success"),
		field.String("error_code").Optional().Nillable(),
		field.JSON("changes", map[string]any{}).Optional(),
		field.JSON("snapshot", map[string]any{}).Optional(),
		field.JSON("metadata", map[string]any{}).Optional(),
		field.Time("occurred_at").Default(time.Now),
	}
}
