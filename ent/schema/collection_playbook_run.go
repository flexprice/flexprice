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

// CollectionPlaybookRun is one invoice going through one playbook. An invoice has at most one live run.
type CollectionPlaybookRun struct {
	ent.Schema
}

// CollectionPlaybookRunLiveUniqueConstraint keeps an invoice in at most one live (active or paused) run.
const CollectionPlaybookRunLiveUniqueConstraint = "idx_collection_playbook_run_live_invoice"

func (CollectionPlaybookRun) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (CollectionPlaybookRun) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),
		field.String("playbook_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("invoice_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("customer_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),
		field.String("state").
			GoType(types.PlaybookRunState("")).
			SchemaType(varchar("20")).
			Default(string(types.PlaybookRunActive)),
		field.String("stop_reason").
			GoType(types.PlaybookStopReason("")).
			SchemaType(varchar("30")).
			Optional(),
		field.String("paused_reason").
			SchemaType(map[string]string{"postgres": "text"}).
			Optional(),
		field.Time("invoice_due_date").
			Immutable(),
		field.Time("next_step_at").
			Optional().
			Nillable(),
		field.Time("started_at").
			Immutable(),
		field.Time("ended_at").
			Optional().
			Nillable(),
	}
}

func (CollectionPlaybookRun) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("playbook", CollectionPlaybook.Type).
			Ref("runs").
			Field("playbook_id").
			Unique().
			Required().
			Immutable(),
		edge.To("step_runs", CollectionPlaybookStepRun.Type),
	}
}

func (CollectionPlaybookRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "invoice_id").
			Unique().
			StorageKey(CollectionPlaybookRunLiveUniqueConstraint).
			Annotations(entsql.IndexWhere("(((status)::text = 'published'::text) AND ((state)::text = ANY ((ARRAY['active'::character varying, 'paused'::character varying])::text[])))")),
		// Explicit names: the generated ones pass Postgres's 63-character limit.
		index.Fields("tenant_id", "environment_id", "playbook_id", "state").
			StorageKey("idx_collection_playbook_run_playbook_state"),
		index.Fields("tenant_id", "environment_id", "customer_id", "state").
			StorageKey("idx_collection_playbook_run_customer_state"),
		index.Fields("tenant_id", "environment_id", "next_step_at").
			Annotations(entsql.IndexWhere("((state)::text = 'active'::text)")),
	}
}
