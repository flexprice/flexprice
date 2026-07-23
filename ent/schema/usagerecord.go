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

// UsageRecord holds the schema for the usage_records table. Each row is one usage snapshot for a
// subscription over a reporting window, produced by the marketplace snapshot cron and consumed by
// the marketplace reporting cron. A row is provider-agnostic at creation: it does not pin any one
// connection, so the same subscription's usage can be reported to every marketplace it's mapped to
// (AWS and GCP simultaneously, for example) without a second snapshot row. Per-destination outcomes
// are tracked in syncs, keyed by connection_id; synced is true only once every connection currently
// relevant to this record has a syncs entry (design doc FLE-981 §6).
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

		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Default(decimal.Zero),

		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty(),

		field.Time("period_start"),

		field.Time("period_end"),

		// Synced is the single retry signal: false means "still needs reporting to at least one
		// relevant connection," and the reporting cron picks it up again on its next run. It is set
		// true only once every connection currently relevant to this record has a syncs entry.
		field.Bool("synced").
			Default(false),

		// Syncs records one entry per connection this record has been successfully reported to,
		// keyed by connection_id. The reporting cron builds this map in memory and writes it back
		// whole (records are reported sequentially, so there are no concurrent writers to a row).
		field.JSON("syncs", map[string]types.UsageRecordSyncEntry{}).
			Optional().
			Default(map[string]types.UsageRecordSyncEntry{}).
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
	}
}

// Indexes of the UsageRecord.
func (UsageRecord) Indexes() []ent.Index {
	return []ent.Index{
		// The reporting cron's hot query: this tenant's unsynced rows.
		index.Fields("tenant_id", "environment_id", "synced"),
		// The snapshot cron's idempotency guarantee.
		index.Fields("tenant_id", "environment_id", "subscription_id", "period_start", "period_end").
			Unique().
			Annotations(entsql.IndexWhere("status = 'published'")),
	}
}
