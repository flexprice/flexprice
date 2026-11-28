package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// CollectionPlaybookStepRun is one playbook step scheduled for one run. (run, step) is unique, so a step runs at
// most once for an invoice however often the scheduler retries.
type CollectionPlaybookStepRun struct {
	ent.Schema
}

// CollectionPlaybookStepRunUniqueConstraint makes each step run once per run.
const CollectionPlaybookStepRunUniqueConstraint = "idx_collection_playbook_step_run_run_step"

func (CollectionPlaybookStepRun) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (CollectionPlaybookStepRun) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	text := map[string]string{"postgres": "text"}

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),
		field.String("run_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("step_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("kind").
			GoType(types.PlaybookStepKind("")).
			SchemaType(varchar("20")),
		field.Time("due_at"),
		field.String("state").
			GoType(types.PlaybookStepRunState("")).
			SchemaType(varchar("20")).
			Default(string(types.PlaybookStepRunPending)),
		field.String("skip_reason").
			GoType(types.PlaybookStepSkipReason("")).
			SchemaType(varchar("30")).
			Optional(),
		field.String("result_ref").
			SchemaType(varchar("50")).
			Optional(),
		field.Int("attempts").
			Default(0),
		field.String("error").
			SchemaType(text).
			Optional(),
		field.Time("executed_at").
			Optional().
			Nillable(),
	}
}

func (CollectionPlaybookStepRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("run", CollectionPlaybookRun.Type).
			Ref("step_runs").
			Field("run_id").
			Unique().
			Required().
			Immutable(),
	}
}

func (CollectionPlaybookStepRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("run_id", "step_id").
			Unique().
			StorageKey(CollectionPlaybookStepRunUniqueConstraint),
		index.Fields("tenant_id", "environment_id", "state", "due_at"),
	}
}
