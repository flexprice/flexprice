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

const (
	// Idx_fx_rate_tenant_live enforces exactly one published tenant rate per (tenant, env, pair).
	Idx_fx_rate_tenant_live = "idx_fx_rate_tenant_live"
	// Idx_fx_rate_override serves customer/subscription override lookups.
	Idx_fx_rate_override = "idx_fx_rate_override"
)

// FXRate holds the schema definition for the FXRate entity.
type FXRate struct {
	ent.Schema
}

// Mixin of the FXRate.
func (FXRate) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},        // tenant_id, status (published default), created_*/updated_*
		baseMixin.EnvironmentMixin{}, // environment_id
	}
}

// Fields of the FXRate.
func (FXRate) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			Unique().
			Immutable(),
		field.String("scope").
			SchemaType(map[string]string{"postgres": "varchar(20)"}).
			NotEmpty().
			GoType(types.FXRateScope("")),
		field.String("scope_id").
			SchemaType(map[string]string{"postgres": "varchar(50)"}).
			NotEmpty(),
		field.String("from_currency").
			SchemaType(map[string]string{"postgres": "varchar(10)"}).
			NotEmpty(),
		field.String("to_currency").
			SchemaType(map[string]string{"postgres": "varchar(10)"}).
			NotEmpty(),
		field.Other("rate", decimal.Decimal{}).
			SchemaType(map[string]string{"postgres": "numeric(24,12)"}),
		field.String("source").
			SchemaType(map[string]string{"postgres": "varchar(20)"}).
			Default(string(types.FXRateSourceFixed)).
			NotEmpty().
			GoType(types.FXRateSource("")),
		field.Time("start_date").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Optional().
			Nillable(),
		field.Time("end_date").
			SchemaType(map[string]string{"postgres": "timestamptz"}).
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{"postgres": "jsonb"}),
	}
}

// Indexes of the FXRate.
func (FXRate) Indexes() []ent.Index {
	return []ent.Index{
		// exactly one published tenant rate per (tenant, env, pair)
		index.Fields("tenant_id", "environment_id", "scope", "scope_id", "from_currency", "to_currency").
			Unique().
			StorageKey(Idx_fx_rate_tenant_live).
			Annotations(entsql.IndexWhere("((status)::text = 'published'::text) AND ((scope)::text = 'tenant'::text)")),
		// serves customer/subscription override lookups
		index.Fields("tenant_id", "environment_id", "scope", "scope_id", "from_currency", "to_currency", "start_date").
			StorageKey(Idx_fx_rate_override),
	}
}
