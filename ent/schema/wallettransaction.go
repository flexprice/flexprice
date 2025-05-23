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

// WalletTransaction holds the schema definition for the WalletTransaction entity.
type WalletTransaction struct {
	ent.Schema
}

// Mixin of the WalletTransaction.
func (WalletTransaction) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the WalletTransaction.
func (WalletTransaction) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("wallet_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("type").
			Default(string(types.TransactionTypeCredit)).
			NotEmpty(),
		field.Other("amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}),
		field.Other("credit_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}).
			Annotations(
				entsql.Default("0"),
			),
		field.Other("credit_balance_before", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}).
			Annotations(
				entsql.Default("0"),
			),
		field.Other("credit_balance_after", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}).
			Annotations(
				entsql.Default("0"),
			),
		field.String("reference_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional(),
		field.String("reference_id").
			Optional(),
		field.String("description").
			Optional(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.String("transaction_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.TransactionStatusPending)),
		field.Time("expiry_date").
			SchemaType(map[string]string{
				"postgres": "timestamp",
			}).
			Immutable().
			Optional().
			Nillable(),
		field.Other("credits_available", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,8)",
			}).
			Annotations(
				entsql.Default("0"),
			),
		field.String("idempotency_key").
			Nillable().
			Immutable().
			Optional(),
		field.String("transaction_reason").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			Default(string(types.TransactionReasonFreeCredit)),
		field.Int("priority").
			Optional().
			Nillable().
			Comment("Lower number indicates higher priority. Nil values are treated as lowest priority."),
	}
}

// Edges of the WalletTransaction.
func (WalletTransaction) Edges() []ent.Edge {
	return nil
}

// Indexes of the WalletTransaction.
func (WalletTransaction) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "wallet_id"),
		index.Fields("tenant_id", "environment_id", "reference_type", "reference_id", "status"),
		index.Fields("tenant_id", "environment_id", "created_at"),
		index.Fields("tenant_id", "environment_id", "wallet_id", "type", "credits_available", "expiry_date").
			StorageKey("idx_tenant_wallet_type_credits_available_expiry_date").
			Annotations(entsql.IndexWhere("credits_available > 0 AND type = 'credit'")),
		index.Fields("tenant_id", "environment_id", "idempotency_key").
			Unique().
			Annotations(
				entsql.IndexWhere("idempotency_key IS NOT NULL AND idempotency_key <> '' AND status='published'"),
			).
			StorageKey("idx_tenant_environment_idempotency_key"),
	}
}
