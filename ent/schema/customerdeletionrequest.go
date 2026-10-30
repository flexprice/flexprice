package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
)

// CustomerDeletionRequest holds the schema definition for the CustomerDeletionRequest entity.
type CustomerDeletionRequest struct {
	ent.Schema
}

// Mixin of the CustomerDeletionRequest.
func (CustomerDeletionRequest) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the CustomerDeletionRequest.
func (CustomerDeletionRequest) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable().
			Comment("Internal customer ID; stable across later redaction"),
		// mutating this would release a retired key
		field.String("external_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			NotEmpty().
			Immutable().
			Comment("Permanent, never cleared; tenant-facing key and never-reuse registry value"),
		field.String("state").
			SchemaType(map[string]string{
				"postgres": "varchar(30)",
			}).
			NotEmpty().
			Comment("Request lifecycle: suppressed, redacting, completed, failed. Unrelated to the status column"),
		field.Time("requested_at").
			Comment("SLA clock start"),
		field.Time("suppressed_at").
			Optional().
			Nillable().
			Comment("Stage (a) proof: reads suppressed"),
		field.Time("redacted_at").
			Optional().
			Nillable().
			Comment("Stage (b) proof: data redacted"),
		field.Time("cooldown_until").
			Optional().
			Nillable().
			Comment("Dispute or chargeback hold before irreversible work"),
		field.Time("irreversible_started_at").
			Optional().
			Nillable().
			Comment("Cancellation is forbidden from this point on"),
		field.String("requested_via").
			SchemaType(map[string]string{
				"postgres": "varchar(20)",
			}).
			NotEmpty().
			Comment("Channel the request arrived through: api or written"),
		field.JSON("retention_policy", map[string]interface{}{}).
			Optional().
			Comment("Retention policy as applied, snapshotted at request time; not a config pointer"),
		field.Text("failure_reason").
			Optional(),
	}
}

// Indexes of the CustomerDeletionRequest.
func (CustomerDeletionRequest) Indexes() []ent.Index {
	return []ent.Index{
		// Idempotency backstop: one open request per customer
		index.Fields("tenant_id", "environment_id", "customer_id").
			Unique(),
		// No partial predicate: retirement is permanent
		index.Fields("tenant_id", "environment_id", "external_id").
			Unique(),
		// Finding work to do
		index.Fields("tenant_id", "environment_id", "state"),
	}
}
