package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Commitment holds the schema definition for a first-class subscription commitment.
type Commitment struct {
	ent.Schema
}

const (
	CommitmentIdempotencyKeyConstraint = "commitment_tenant_id_environment_id_idempotency_key"
)

func (Commitment) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

func (Commitment) Fields() []ent.Field {
	varchar := func(n string) map[string]string { return map[string]string{"postgres": "varchar(" + n + ")"} }
	numeric := map[string]string{"postgres": "numeric(20,8)"}

	return []ent.Field{
		field.String("id").
			SchemaType(varchar("50")).
			Unique().
			Immutable(),

		field.String("subscription_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("customer_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("name").
			SchemaType(varchar("255")).
			NotEmpty(),

		field.String("invoice_label").
			SchemaType(varchar("255")).
			NotEmpty(),

		field.String("invoice_description").
			SchemaType(varchar("500")).
			Optional().
			Nillable(),

		field.String("currency").
			SchemaType(varchar("10")).
			NotEmpty().
			Immutable(),

		field.String("scope_entity_type").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.CommitmentScopeType("")),

		field.String("scope_entity_id").
			SchemaType(varchar("50")).
			NotEmpty().
			Immutable(),

		field.String("type").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.CommitmentType("")),

		field.Other("value", decimal.Decimal{}).
			SchemaType(numeric).
			Immutable(),

		field.String("committed_price_id").
			SchemaType(varchar("50")).
			Optional().
			Nillable().
			Immutable(),

		field.String("overage_price_id").
			SchemaType(varchar("50")).
			Optional().
			Nillable().
			Immutable(),

		field.String("measurement").
			SchemaType(varchar("20")).
			NotEmpty().
			Immutable().
			GoType(types.CommitmentMeasurement("")),

		field.JSON("active_windows", types.ActiveWindows{}).
			Optional().
			Immutable(),

		field.String("timezone").
			SchemaType(varchar("64")).
			NotEmpty().
			Immutable().
			Validate(types.ValidateTimezone),

		field.Int("priority").
			Default(0).
			Immutable(),

		field.Bool("binding").
			Default(false).
			Immutable(),

		field.String("collection_method").
			SchemaType(varchar("20")).
			Optional().
			Nillable().
			Immutable().
			GoType(types.CommitmentCollectionMethod("")),

		field.Other("total_amount", decimal.Decimal{}).
			SchemaType(numeric).
			Optional().
			Nillable().
			Immutable(),

		field.String("instalment_invoicing").
			SchemaType(varchar("30")).
			Optional().
			Nillable().
			Immutable(),

		field.Time("start_date").
			Immutable(),

		// Null for an open-ended commitment, which runs until terminated.
		field.Time("end_date").
			Optional().
			Nillable().
			Immutable(),

		field.String("on_early_termination").
			SchemaType(varchar("20")).
			Optional().
			Nillable().
			Immutable().
			GoType(types.CommitmentEarlyTermination("")),

		field.String("commitment_status").
			SchemaType(varchar("20")).
			Default(string(types.CommitmentStatusActive)).
			GoType(types.CommitmentStatus("")),

		field.Time("terminated_at").
			Optional().
			Nillable(),

		field.String("idempotency_key").
			SchemaType(varchar("128")).
			Optional().
			Nillable().
			Immutable(),

		field.Other("metadata", types.Metadata{}).
			SchemaType(map[string]string{"postgres": "jsonb"}).
			Optional().
			Default(types.Metadata{}),
	}
}

func (Commitment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "subscription_id", "commitment_status"),
		index.Fields("tenant_id", "environment_id", "scope_entity_type", "scope_entity_id"),
		index.Fields("tenant_id", "environment_id", "customer_id"),
		// Archived commitments release their key.
		index.Fields("tenant_id", "environment_id", "idempotency_key").
			Unique().
			StorageKey(CommitmentIdempotencyKeyConstraint).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text)")),
	}
}
