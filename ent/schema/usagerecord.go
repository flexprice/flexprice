package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// UsageRecord holds the schema for the usage_records table. Each row is one usage snapshot for a
// subscription over a reporting window. The syncs JSONB map records which marketplaces the row has
// been reported to, so the same structure supports additional marketplaces without a schema change.
type UsageRecord struct {
	ent.Schema
}

// Mixin of the UsageRecord.
func (UsageRecord) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the UsageRecord.
func (UsageRecord) Fields() []ent.Field {
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
			NotEmpty(),

		field.String("customer_external_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),

		field.String("subscription_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),

		field.String("plan_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty(),

		// Quantity is reserved for future multi-dimension/raw-unit use; unused in v1.
		field.Other("quantity", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),

		// Amount is the dollar total for [PeriodStart, PeriodEnd), from CalculateMeterUsageCharges —
		// this is what's sent to AWS as BatchMeterUsage's "Quantity" field.
		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),

		field.Time("period_start"),

		field.Time("period_end"),

		// Syncs is a map keyed by Marketplace (e.g. "aws") -> MarketplaceSyncEntry (connection_id,
		// synced_at, marketplace_report_id). Typed on the Go side in internal/domain/usagerecord;
		// stored here as a generic JSON map, matching how ent/schema/connection.go stores
		// encrypted_secret_data/metadata.
		field.JSON("syncs", map[string]interface{}{}).
			Optional(),

		field.Bool("all_providers_synced").
			Default(false),
	}
}

// Indexes of the UsageRecord.
func (UsageRecord) Indexes() []ent.Index {
	return []ent.Index{
		// Cron B's hot query: unsynced rows for a tenant/environment.
		index.Fields("tenant_id", "environment_id", "all_providers_synced"),
	}
}
