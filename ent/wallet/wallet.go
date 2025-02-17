// Code generated by ent, DO NOT EDIT.

package wallet

import (
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/shopspring/decimal"
)

const (
	// Label holds the string label denoting the wallet type in the database.
	Label = "wallet"
	// FieldID holds the string denoting the id field in the database.
	FieldID = "id"
	// FieldTenantID holds the string denoting the tenant_id field in the database.
	FieldTenantID = "tenant_id"
	// FieldStatus holds the string denoting the status field in the database.
	FieldStatus = "status"
	// FieldCreatedAt holds the string denoting the created_at field in the database.
	FieldCreatedAt = "created_at"
	// FieldUpdatedAt holds the string denoting the updated_at field in the database.
	FieldUpdatedAt = "updated_at"
	// FieldCreatedBy holds the string denoting the created_by field in the database.
	FieldCreatedBy = "created_by"
	// FieldUpdatedBy holds the string denoting the updated_by field in the database.
	FieldUpdatedBy = "updated_by"
	// FieldName holds the string denoting the name field in the database.
	FieldName = "name"
	// FieldCustomerID holds the string denoting the customer_id field in the database.
	FieldCustomerID = "customer_id"
	// FieldCurrency holds the string denoting the currency field in the database.
	FieldCurrency = "currency"
	// FieldDescription holds the string denoting the description field in the database.
	FieldDescription = "description"
	// FieldMetadata holds the string denoting the metadata field in the database.
	FieldMetadata = "metadata"
	// FieldBalance holds the string denoting the balance field in the database.
	FieldBalance = "balance"
	// FieldWalletStatus holds the string denoting the wallet_status field in the database.
	FieldWalletStatus = "wallet_status"
	// FieldAutoTopupTrigger holds the string denoting the auto_topup_trigger field in the database.
	FieldAutoTopupTrigger = "auto_topup_trigger"
	// FieldAutoTopupMinBalance holds the string denoting the auto_topup_min_balance field in the database.
	FieldAutoTopupMinBalance = "auto_topup_min_balance"
	// FieldAutoTopupAmount holds the string denoting the auto_topup_amount field in the database.
	FieldAutoTopupAmount = "auto_topup_amount"
	// Table holds the table name of the wallet in the database.
	Table = "wallets"
)

// Columns holds all SQL columns for wallet fields.
var Columns = []string{
	FieldID,
	FieldTenantID,
	FieldStatus,
	FieldCreatedAt,
	FieldUpdatedAt,
	FieldCreatedBy,
	FieldUpdatedBy,
	FieldName,
	FieldCustomerID,
	FieldCurrency,
	FieldDescription,
	FieldMetadata,
	FieldBalance,
	FieldWalletStatus,
	FieldAutoTopupTrigger,
	FieldAutoTopupMinBalance,
	FieldAutoTopupAmount,
}

// ValidColumn reports if the column name is valid (part of the table columns).
func ValidColumn(column string) bool {
	for i := range Columns {
		if column == Columns[i] {
			return true
		}
	}
	return false
}

var (
	// TenantIDValidator is a validator for the "tenant_id" field. It is called by the builders before save.
	TenantIDValidator func(string) error
	// DefaultStatus holds the default value on creation for the "status" field.
	DefaultStatus string
	// DefaultCreatedAt holds the default value on creation for the "created_at" field.
	DefaultCreatedAt func() time.Time
	// DefaultUpdatedAt holds the default value on creation for the "updated_at" field.
	DefaultUpdatedAt func() time.Time
	// UpdateDefaultUpdatedAt holds the default value on update for the "updated_at" field.
	UpdateDefaultUpdatedAt func() time.Time
	// CustomerIDValidator is a validator for the "customer_id" field. It is called by the builders before save.
	CustomerIDValidator func(string) error
	// CurrencyValidator is a validator for the "currency" field. It is called by the builders before save.
	CurrencyValidator func(string) error
	// DefaultBalance holds the default value on creation for the "balance" field.
	DefaultBalance decimal.Decimal
	// DefaultWalletStatus holds the default value on creation for the "wallet_status" field.
	DefaultWalletStatus string
	// DefaultAutoTopupTrigger holds the default value on creation for the "auto_topup_trigger" field.
	DefaultAutoTopupTrigger string
)

// OrderOption defines the ordering options for the Wallet queries.
type OrderOption func(*sql.Selector)

// ByID orders the results by the id field.
func ByID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldID, opts...).ToFunc()
}

// ByTenantID orders the results by the tenant_id field.
func ByTenantID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldTenantID, opts...).ToFunc()
}

// ByStatus orders the results by the status field.
func ByStatus(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldStatus, opts...).ToFunc()
}

// ByCreatedAt orders the results by the created_at field.
func ByCreatedAt(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCreatedAt, opts...).ToFunc()
}

// ByUpdatedAt orders the results by the updated_at field.
func ByUpdatedAt(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldUpdatedAt, opts...).ToFunc()
}

// ByCreatedBy orders the results by the created_by field.
func ByCreatedBy(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCreatedBy, opts...).ToFunc()
}

// ByUpdatedBy orders the results by the updated_by field.
func ByUpdatedBy(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldUpdatedBy, opts...).ToFunc()
}

// ByName orders the results by the name field.
func ByName(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldName, opts...).ToFunc()
}

// ByCustomerID orders the results by the customer_id field.
func ByCustomerID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCustomerID, opts...).ToFunc()
}

// ByCurrency orders the results by the currency field.
func ByCurrency(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCurrency, opts...).ToFunc()
}

// ByDescription orders the results by the description field.
func ByDescription(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldDescription, opts...).ToFunc()
}

// ByBalance orders the results by the balance field.
func ByBalance(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldBalance, opts...).ToFunc()
}

// ByWalletStatus orders the results by the wallet_status field.
func ByWalletStatus(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldWalletStatus, opts...).ToFunc()
}

// ByAutoTopupTrigger orders the results by the auto_topup_trigger field.
func ByAutoTopupTrigger(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldAutoTopupTrigger, opts...).ToFunc()
}

// ByAutoTopupMinBalance orders the results by the auto_topup_min_balance field.
func ByAutoTopupMinBalance(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldAutoTopupMinBalance, opts...).ToFunc()
}

// ByAutoTopupAmount orders the results by the auto_topup_amount field.
func ByAutoTopupAmount(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldAutoTopupAmount, opts...).ToFunc()
}
