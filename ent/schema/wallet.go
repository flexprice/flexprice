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

// Wallet holds the schema definition for the Wallet entity.
type Wallet struct {
	ent.Schema
}

// Mixin of the Wallet.
func (Wallet) Mixin() []ent.Mixin {
	return []ent.Mixin{
		baseMixin.BaseMixin{},
		baseMixin.EnvironmentMixin{},
	}
}

// Fields of the Wallet.
func (Wallet) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Unique().
			Immutable(),
		field.String("name").
			SchemaType(map[string]string{
				"postgres": "varchar(255)",
			}).
			Optional(),
		field.String("customer_id").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			NotEmpty().
			Immutable(),
		field.String("currency").
			SchemaType(map[string]string{
				"postgres": "varchar(10)",
			}).
			NotEmpty(),
		field.String("description").
			Optional(),
		field.JSON("metadata", map[string]string{}).
			Optional().
			SchemaType(map[string]string{
				"postgres": "jsonb",
			}),
		field.Other("balance", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}).
			Default(decimal.Zero),
		field.Other("credit_balance", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}).
			Annotations(
				entsql.Default("0"),
			),
		field.String("wallet_status").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Default(string(types.WalletStatusActive)),
		field.String("auto_topup_trigger").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Optional().
			Nillable().
			Default(string(types.AutoTopupTriggerDisabled)),
		field.Other("auto_topup_min_balance", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}).
			Optional().
			Nillable(),
		field.Other("auto_topup_amount", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(20,9)",
			}).
			Optional().
			Nillable(),
		field.String("wallet_type").
			SchemaType(map[string]string{
				"postgres": "varchar(50)",
			}).
			Immutable().
			Default(string(types.WalletTypePrePaid)),
		field.Other("conversion_rate", decimal.Decimal{}).
			SchemaType(map[string]string{
				"postgres": "numeric(10,5)",
			}).
			Immutable().
			Annotations(
				entsql.Default("1"),
			),
		field.JSON("config", types.WalletConfig{}).
			Optional(),
	}
}

// Edges of the Wallet.
func (Wallet) Edges() []ent.Edge {
	return nil
}

// Indexes of the Wallet.
func (Wallet) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "environment_id", "customer_id", "status"),
		index.Fields("tenant_id", "environment_id", "status", "wallet_status"),
	}
}
