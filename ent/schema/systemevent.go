package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/google/uuid"
)

// SystemEvent holds the schema definition for the SystemEvent entity.
type SystemEvent struct {
	ent.Schema
}

// Fields of the SystemEvent.
func (SystemEvent) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).
			Default(uuid.New).
			Unique(),
		field.String("tenant_id").
			NotEmpty(),
		field.String("type").
			NotEmpty(),
		field.JSON("payload", map[string]interface{}{}).
			Optional(),
		field.String("status").
			NotEmpty(),
		field.Time("created_at"),
		field.Time("updated_at"),
		field.String("created_by").
			NotEmpty(),
		field.String("updated_by").
			NotEmpty(),
		field.String("workflow_id").
			Optional(),
	}
}

func (SystemEvent) Indexes() []ent.Index {
	return []ent.Index{
		// Common query patterns from repository layer
		index.Fields("workflow_id"),
		index.Fields("status"),
		index.Fields("type"),
		// For billing period updates
		index.Fields("tenant_id"),
	}
}
