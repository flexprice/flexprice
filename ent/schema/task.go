package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// Task holds the schema definition for the Task entity.
type Task struct {
	ent.Schema
}

// Mixin of the Task.
func (Task) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
	}
}

// Fields of the Task.
func (Task) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("task_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("entity_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),
		field.String("file_url").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty(),
		field.String("file_name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional().
			Nillable(),
		field.String("file_type").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty(),
		field.String("task_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default("PENDING"),
		field.Int("total_records").
			Optional().
			Nillable(),
		field.Int("processed_records").
			Default(0),
		field.Int("successful_records").
			Default(0),
		field.Int("failed_records").
			Default(0),
		field.Text("error_summary").
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]interface{}{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.Time("started_at").
			Optional().
			Nillable(),
		field.Time("completed_at").
			Optional().
			Nillable(),
		field.Time("failed_at").
			Optional().
			Nillable(),
	}
}

// Edges of the Task.
func (Task) Edges() []ent.Edge {
	return nil // No edges for now
}

// Indexes of the Task.
func (Task) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "task_type", "entity_type", "status").
			StorageKey("idx_tasks_tenant_type_status"),
		index.Fields("tenant_id", "created_by", "status").
			StorageKey("idx_tasks_tenant_user"),
		index.Fields("tenant_id", "task_status", "status").
			StorageKey("idx_tasks_tenant_task_status"),
	}
}
