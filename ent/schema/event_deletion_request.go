package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
)

// EventDeletionRequest is one customer's leg of a bulk event deletion. It is the
// fence invoice finalization reads, the window ingestion reads, and the record of
// what happened.
type EventDeletionRequest struct {
	ent.Schema
}

func (EventDeletionRequest) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (EventDeletionRequest) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Unique().
			Immutable(),

		// Shared by every row of one request, returned to the caller and stamped on
		// every audit row. Not the HTTP request id.
		field.String("request_id").
			SchemaType(map[string]string{"postgres": "varchar(128)"}).
			NotEmpty().
			Immutable(),

		field.String("external_customer_id").
			SchemaType(map[string]string{"postgres": "varchar(255)"}).
			NotEmpty().
			Immutable(),

		field.Time("period_start").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Immutable(),

		field.Time("period_end").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Immutable(),

		field.String("deletion_status").
			SchemaType(map[string]string{"postgres": "varchar(20)"}).
			NotEmpty().
			GoType(types.EventDeletionStatus("")),

		field.Time("started_at").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Immutable(),

		field.Time("completed_at").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Optional().
			Nillable(),

		field.Time("failed_at").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Optional().
			Nillable(),

		field.String("failure_reason").
			SchemaType(map[string]string{"postgres": "text"}).
			Optional().
			Nillable(),

		// Records that this request asked for the payload not to be stored.
		field.Bool("skip_payload").
			Default(false).
			Immutable(),
	}
}

func (EventDeletionRequest) Edges() []ent.Edge {
	return nil
}

func (EventDeletionRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "external_customer_id").
			Unique().
			StorageKey("idx_event_deletion_request_in_progress").
			Annotations(entsql.IndexWhere("(((status)::text = 'published'::text) AND ((deletion_status)::text = 'DELETING'::text))")),
	}
}
