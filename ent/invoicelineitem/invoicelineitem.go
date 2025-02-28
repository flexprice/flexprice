// Code generated by ent, DO NOT EDIT.

package invoicelineitem

import (
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqlgraph"
	"github.com/shopspring/decimal"
)

const (
	// Label holds the string label denoting the invoicelineitem type in the database.
	Label = "invoice_line_item"
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
	// FieldEnvironmentID holds the string denoting the environment_id field in the database.
	FieldEnvironmentID = "environment_id"
	// FieldInvoiceID holds the string denoting the invoice_id field in the database.
	FieldInvoiceID = "invoice_id"
	// FieldCustomerID holds the string denoting the customer_id field in the database.
	FieldCustomerID = "customer_id"
	// FieldSubscriptionID holds the string denoting the subscription_id field in the database.
	FieldSubscriptionID = "subscription_id"
	// FieldPlanID holds the string denoting the plan_id field in the database.
	FieldPlanID = "plan_id"
	// FieldPlanDisplayName holds the string denoting the plan_display_name field in the database.
	FieldPlanDisplayName = "plan_display_name"
	// FieldPriceID holds the string denoting the price_id field in the database.
	FieldPriceID = "price_id"
	// FieldPriceType holds the string denoting the price_type field in the database.
	FieldPriceType = "price_type"
	// FieldMeterID holds the string denoting the meter_id field in the database.
	FieldMeterID = "meter_id"
	// FieldMeterDisplayName holds the string denoting the meter_display_name field in the database.
	FieldMeterDisplayName = "meter_display_name"
	// FieldDisplayName holds the string denoting the display_name field in the database.
	FieldDisplayName = "display_name"
	// FieldAmount holds the string denoting the amount field in the database.
	FieldAmount = "amount"
	// FieldQuantity holds the string denoting the quantity field in the database.
	FieldQuantity = "quantity"
	// FieldCurrency holds the string denoting the currency field in the database.
	FieldCurrency = "currency"
	// FieldPeriodStart holds the string denoting the period_start field in the database.
	FieldPeriodStart = "period_start"
	// FieldPeriodEnd holds the string denoting the period_end field in the database.
	FieldPeriodEnd = "period_end"
	// FieldMetadata holds the string denoting the metadata field in the database.
	FieldMetadata = "metadata"
	// EdgeInvoice holds the string denoting the invoice edge name in mutations.
	EdgeInvoice = "invoice"
	// Table holds the table name of the invoicelineitem in the database.
	Table = "invoice_line_items"
	// InvoiceTable is the table that holds the invoice relation/edge.
	InvoiceTable = "invoice_line_items"
	// InvoiceInverseTable is the table name for the Invoice entity.
	// It exists in this package in order to avoid circular dependency with the "invoice" package.
	InvoiceInverseTable = "invoices"
	// InvoiceColumn is the table column denoting the invoice relation/edge.
	InvoiceColumn = "invoice_id"
)

// Columns holds all SQL columns for invoicelineitem fields.
var Columns = []string{
	FieldID,
	FieldTenantID,
	FieldStatus,
	FieldCreatedAt,
	FieldUpdatedAt,
	FieldCreatedBy,
	FieldUpdatedBy,
	FieldEnvironmentID,
	FieldInvoiceID,
	FieldCustomerID,
	FieldSubscriptionID,
	FieldPlanID,
	FieldPlanDisplayName,
	FieldPriceID,
	FieldPriceType,
	FieldMeterID,
	FieldMeterDisplayName,
	FieldDisplayName,
	FieldAmount,
	FieldQuantity,
	FieldCurrency,
	FieldPeriodStart,
	FieldPeriodEnd,
	FieldMetadata,
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
	// DefaultEnvironmentID holds the default value on creation for the "environment_id" field.
	DefaultEnvironmentID string
	// InvoiceIDValidator is a validator for the "invoice_id" field. It is called by the builders before save.
	InvoiceIDValidator func(string) error
	// CustomerIDValidator is a validator for the "customer_id" field. It is called by the builders before save.
	CustomerIDValidator func(string) error
	// PriceIDValidator is a validator for the "price_id" field. It is called by the builders before save.
	PriceIDValidator func(string) error
	// DefaultAmount holds the default value on creation for the "amount" field.
	DefaultAmount decimal.Decimal
	// DefaultQuantity holds the default value on creation for the "quantity" field.
	DefaultQuantity decimal.Decimal
	// CurrencyValidator is a validator for the "currency" field. It is called by the builders before save.
	CurrencyValidator func(string) error
)

// OrderOption defines the ordering options for the InvoiceLineItem queries.
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

// ByEnvironmentID orders the results by the environment_id field.
func ByEnvironmentID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldEnvironmentID, opts...).ToFunc()
}

// ByInvoiceID orders the results by the invoice_id field.
func ByInvoiceID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldInvoiceID, opts...).ToFunc()
}

// ByCustomerID orders the results by the customer_id field.
func ByCustomerID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCustomerID, opts...).ToFunc()
}

// BySubscriptionID orders the results by the subscription_id field.
func BySubscriptionID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldSubscriptionID, opts...).ToFunc()
}

// ByPlanID orders the results by the plan_id field.
func ByPlanID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPlanID, opts...).ToFunc()
}

// ByPlanDisplayName orders the results by the plan_display_name field.
func ByPlanDisplayName(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPlanDisplayName, opts...).ToFunc()
}

// ByPriceID orders the results by the price_id field.
func ByPriceID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPriceID, opts...).ToFunc()
}

// ByPriceType orders the results by the price_type field.
func ByPriceType(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPriceType, opts...).ToFunc()
}

// ByMeterID orders the results by the meter_id field.
func ByMeterID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldMeterID, opts...).ToFunc()
}

// ByMeterDisplayName orders the results by the meter_display_name field.
func ByMeterDisplayName(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldMeterDisplayName, opts...).ToFunc()
}

// ByDisplayName orders the results by the display_name field.
func ByDisplayName(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldDisplayName, opts...).ToFunc()
}

// ByAmount orders the results by the amount field.
func ByAmount(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldAmount, opts...).ToFunc()
}

// ByQuantity orders the results by the quantity field.
func ByQuantity(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldQuantity, opts...).ToFunc()
}

// ByCurrency orders the results by the currency field.
func ByCurrency(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCurrency, opts...).ToFunc()
}

// ByPeriodStart orders the results by the period_start field.
func ByPeriodStart(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPeriodStart, opts...).ToFunc()
}

// ByPeriodEnd orders the results by the period_end field.
func ByPeriodEnd(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPeriodEnd, opts...).ToFunc()
}

// ByInvoiceField orders the results by invoice field.
func ByInvoiceField(field string, opts ...sql.OrderTermOption) OrderOption {
	return func(s *sql.Selector) {
		sqlgraph.OrderByNeighborTerms(s, newInvoiceStep(), sql.OrderByField(field, opts...))
	}
}
func newInvoiceStep() *sqlgraph.Step {
	return sqlgraph.NewStep(
		sqlgraph.From(Table, FieldID),
		sqlgraph.To(InvoiceInverseTable, FieldID),
		sqlgraph.Edge(sqlgraph.M2O, true, InvoiceTable, InvoiceColumn),
	)
}
