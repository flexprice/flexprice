package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// CollectionPlaybook is a tenant's rule for chasing unpaid invoices: which invoices it picks up, the timed steps it
// runs for each and the events that end it. Steps are stored on the playbook; their runs are in
// collection_playbook_runs and collection_playbook_step_runs.
type CollectionPlaybook struct {
	ent.Schema
}

func (CollectionPlaybook) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (CollectionPlaybook) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	jsonb := map[string]string{"postgres": "jsonb"}

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),
		field.String("name").
			SchemaType(varchar("255")).
			NotEmpty(),
		field.String("description").
			SchemaType(map[string]string{"postgres": "text"}).
			Optional(),
		field.String("state").
			GoType(types.PlaybookState("")).
			SchemaType(varchar("20")).
			Default(string(types.PlaybookStateDraft)),
		field.Int("priority"),
		field.String("trigger").
			GoType(types.PlaybookTrigger("")).
			SchemaType(varchar("20")).
			Default(string(types.PlaybookTriggerOverdue)),
		field.JSON("audience", types.PlaybookAudience{}).
			SchemaType(jsonb),
		field.JSON("steps", []types.PlaybookStep{}).
			SchemaType(jsonb),
		field.JSON("stops", []types.PlaybookStopCondition{}).
			SchemaType(jsonb),
		field.Int("min_gap_days").
			Default(3),
		field.Bool("skip_weekends").
			Default(true),
		field.String("template_id").
			SchemaType(varchar("50")).
			Optional(),
		field.Int("version").
			Default(1),
		field.Time("activated_at").
			Optional().
			Nillable(),
	}
}

func (CollectionPlaybook) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("runs", CollectionPlaybookRun.Type),
	}
}

func (CollectionPlaybook) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "state", "priority").
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
	}
}
