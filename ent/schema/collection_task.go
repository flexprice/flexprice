package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// CollectionTask is a to-do a playbook step created for a person, about one invoice.
type CollectionTask struct {
	ent.Schema
}

func (CollectionTask) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (CollectionTask) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),
		field.String("invoice_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("customer_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("run_id").
			SchemaType(varchar("50")).
			Optional().
			Immutable(),
		field.String("step_id").
			SchemaType(varchar("50")).
			Optional().
			Immutable(),
		field.String("assignee_user_id").
			SchemaType(varchar("50")).
			Optional(),
		field.String("assignee_email").
			SchemaType(varchar("255")).
			Optional(),
		field.String("note").
			SchemaType(map[string]string{"postgres": "text"}),
		field.String("state").
			GoType(types.CollectionTaskState("")).
			SchemaType(varchar("20")).
			Default(string(types.CollectionTaskOpen)),
		field.Time("done_at").
			Optional().
			Nillable(),
		field.String("done_by").
			SchemaType(varchar("50")).
			Optional(),
	}
}

func (CollectionTask) Edges() []ent.Edge {
	return nil
}

func (CollectionTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "invoice_id"),
		index.Fields("tenant_id", "environment_id", "state", "created_at"),
		// A step creates at most one task per run, however often it is retried.
		index.Fields("run_id", "step_id").
			Unique().
			StorageKey("idx_collection_task_run_step").
			Annotations(entsql.IndexWhere("(run_id IS NOT NULL)")),
	}
}
