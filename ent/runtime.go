// Code generated by ent, DO NOT EDIT.

package ent

import (
	"time"

	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/ent/wallet"
	"github.com/flexprice/flexprice/ent/wallettransaction"
	"github.com/shopspring/decimal"
)

// The init function reads all schema descriptors with runtime code
// (default values, validators, hooks and policies) and stitches it
// to their package variables.
func init() {
	walletFields := schema.Wallet{}.Fields()
	_ = walletFields
	// walletDescTenantID is the schema descriptor for tenant_id field.
	walletDescTenantID := walletFields[1].Descriptor()
	// wallet.TenantIDValidator is a validator for the "tenant_id" field. It is called by the builders before save.
	wallet.TenantIDValidator = walletDescTenantID.Validators[0].(func(string) error)
	// walletDescCustomerID is the schema descriptor for customer_id field.
	walletDescCustomerID := walletFields[2].Descriptor()
	// wallet.CustomerIDValidator is a validator for the "customer_id" field. It is called by the builders before save.
	wallet.CustomerIDValidator = walletDescCustomerID.Validators[0].(func(string) error)
	// walletDescCurrency is the schema descriptor for currency field.
	walletDescCurrency := walletFields[3].Descriptor()
	// wallet.CurrencyValidator is a validator for the "currency" field. It is called by the builders before save.
	wallet.CurrencyValidator = walletDescCurrency.Validators[0].(func(string) error)
	// walletDescBalance is the schema descriptor for balance field.
	walletDescBalance := walletFields[6].Descriptor()
	// wallet.DefaultBalance holds the default value on creation for the balance field.
	wallet.DefaultBalance = walletDescBalance.Default.(decimal.Decimal)
	// walletDescWalletStatus is the schema descriptor for wallet_status field.
	walletDescWalletStatus := walletFields[7].Descriptor()
	// wallet.DefaultWalletStatus holds the default value on creation for the wallet_status field.
	wallet.DefaultWalletStatus = walletDescWalletStatus.Default.(string)
	// walletDescStatus is the schema descriptor for status field.
	walletDescStatus := walletFields[8].Descriptor()
	// wallet.DefaultStatus holds the default value on creation for the status field.
	wallet.DefaultStatus = walletDescStatus.Default.(string)
	// walletDescCreatedAt is the schema descriptor for created_at field.
	walletDescCreatedAt := walletFields[9].Descriptor()
	// wallet.DefaultCreatedAt holds the default value on creation for the created_at field.
	wallet.DefaultCreatedAt = walletDescCreatedAt.Default.(func() time.Time)
	// walletDescUpdatedAt is the schema descriptor for updated_at field.
	walletDescUpdatedAt := walletFields[11].Descriptor()
	// wallet.DefaultUpdatedAt holds the default value on creation for the updated_at field.
	wallet.DefaultUpdatedAt = walletDescUpdatedAt.Default.(func() time.Time)
	// wallet.UpdateDefaultUpdatedAt holds the default value on update for the updated_at field.
	wallet.UpdateDefaultUpdatedAt = walletDescUpdatedAt.UpdateDefault.(func() time.Time)
	wallettransactionFields := schema.WalletTransaction{}.Fields()
	_ = wallettransactionFields
	// wallettransactionDescTenantID is the schema descriptor for tenant_id field.
	wallettransactionDescTenantID := wallettransactionFields[1].Descriptor()
	// wallettransaction.TenantIDValidator is a validator for the "tenant_id" field. It is called by the builders before save.
	wallettransaction.TenantIDValidator = wallettransactionDescTenantID.Validators[0].(func(string) error)
	// wallettransactionDescWalletID is the schema descriptor for wallet_id field.
	wallettransactionDescWalletID := wallettransactionFields[2].Descriptor()
	// wallettransaction.WalletIDValidator is a validator for the "wallet_id" field. It is called by the builders before save.
	wallettransaction.WalletIDValidator = wallettransactionDescWalletID.Validators[0].(func(string) error)
	// wallettransactionDescType is the schema descriptor for type field.
	wallettransactionDescType := wallettransactionFields[3].Descriptor()
	// wallettransaction.DefaultType holds the default value on creation for the type field.
	wallettransaction.DefaultType = wallettransactionDescType.Default.(string)
	// wallettransaction.TypeValidator is a validator for the "type" field. It is called by the builders before save.
	wallettransaction.TypeValidator = wallettransactionDescType.Validators[0].(func(string) error)
	// wallettransactionDescBalanceBefore is the schema descriptor for balance_before field.
	wallettransactionDescBalanceBefore := wallettransactionFields[5].Descriptor()
	// wallettransaction.DefaultBalanceBefore holds the default value on creation for the balance_before field.
	wallettransaction.DefaultBalanceBefore = wallettransactionDescBalanceBefore.Default.(decimal.Decimal)
	// wallettransactionDescBalanceAfter is the schema descriptor for balance_after field.
	wallettransactionDescBalanceAfter := wallettransactionFields[6].Descriptor()
	// wallettransaction.DefaultBalanceAfter holds the default value on creation for the balance_after field.
	wallettransaction.DefaultBalanceAfter = wallettransactionDescBalanceAfter.Default.(decimal.Decimal)
	// wallettransactionDescTransactionStatus is the schema descriptor for transaction_status field.
	wallettransactionDescTransactionStatus := wallettransactionFields[11].Descriptor()
	// wallettransaction.DefaultTransactionStatus holds the default value on creation for the transaction_status field.
	wallettransaction.DefaultTransactionStatus = wallettransactionDescTransactionStatus.Default.(string)
	// wallettransactionDescStatus is the schema descriptor for status field.
	wallettransactionDescStatus := wallettransactionFields[12].Descriptor()
	// wallettransaction.DefaultStatus holds the default value on creation for the status field.
	wallettransaction.DefaultStatus = wallettransactionDescStatus.Default.(string)
	// wallettransactionDescCreatedAt is the schema descriptor for created_at field.
	wallettransactionDescCreatedAt := wallettransactionFields[13].Descriptor()
	// wallettransaction.DefaultCreatedAt holds the default value on creation for the created_at field.
	wallettransaction.DefaultCreatedAt = wallettransactionDescCreatedAt.Default.(func() time.Time)
	// wallettransactionDescUpdatedAt is the schema descriptor for updated_at field.
	wallettransactionDescUpdatedAt := wallettransactionFields[15].Descriptor()
	// wallettransaction.DefaultUpdatedAt holds the default value on creation for the updated_at field.
	wallettransaction.DefaultUpdatedAt = wallettransactionDescUpdatedAt.Default.(func() time.Time)
	// wallettransaction.UpdateDefaultUpdatedAt holds the default value on update for the updated_at field.
	wallettransaction.UpdateDefaultUpdatedAt = wallettransactionDescUpdatedAt.UpdateDefault.(func() time.Time)
}