package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	baseMixin "github.com/flexprice/flexprice/ent/schema/mixin"
	"github.com/shopspring/decimal"
)

// UsageRecord holds the schema for the usage_records table. Each row is one usage snapshot for a
// subscription over a reporting window, produced by the marketplace snapshot cron and consumed by
// the marketplace reporting cron. A row reports to exactly one marketplace — the one identified by
// connection_id — since a subscription is only ever sold through one marketplace; there is no
// fan-out to support here (design doc FLE-981 §6).
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

		// ConnectionID pins which marketplace connection this row reports through, stamped by the
		// snapshot cron. It is what the reporting cron uses to decrypt the right secret and pick the
		// right provider — the row itself carries no separate provider_type field, since the
		// connection's is authoritative and the reporting cron loads the connection anyway.
		field.String("connection_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional(),

		// Synced is the single retry signal: false means "still needs reporting," and the reporting
		// cron picks it up again on its next run. There is no separate failure state — a rejected
		// report is logged and simply left synced=false.
		field.Bool("synced").
			Default(false),

		field.Time("synced_at").
			Optional().
			Nillable(),

		// MarketplaceReportID is AWS's MeteringRecordId, or GCP's operationId (which is always this
		// row's own id, since GCP's services.report returns no per-record receipt of its own).
		field.String("marketplace_report_id").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),
	}
}

// Indexes of the UsageRecord.
func (UsageRecord) Indexes() []ent.Index {
	return []ent.Index{
		// The reporting cron's hot query: unsynced rows for one connection.
		index.Fields("tenant_id", "environment_id", "connection_id", "synced"),
	}
}
