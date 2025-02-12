// Code generated by ent, DO NOT EDIT.

package invoice

import (
	"time"

	"entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqlgraph"
	"github.com/shopspring/decimal"
)

const (
	// Label holds the string label denoting the invoice type in the database.
	Label = "invoice"
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
	// FieldCustomerID holds the string denoting the customer_id field in the database.
	FieldCustomerID = "customer_id"
	// FieldSubscriptionID holds the string denoting the subscription_id field in the database.
	FieldSubscriptionID = "subscription_id"
	// FieldInvoiceType holds the string denoting the invoice_type field in the database.
	FieldInvoiceType = "invoice_type"
	// FieldInvoiceStatus holds the string denoting the invoice_status field in the database.
	FieldInvoiceStatus = "invoice_status"
	// FieldPaymentStatus holds the string denoting the payment_status field in the database.
	FieldPaymentStatus = "payment_status"
	// FieldCurrency holds the string denoting the currency field in the database.
	FieldCurrency = "currency"
	// FieldAmountDue holds the string denoting the amount_due field in the database.
	FieldAmountDue = "amount_due"
	// FieldAmountPaid holds the string denoting the amount_paid field in the database.
	FieldAmountPaid = "amount_paid"
	// FieldAmountRemaining holds the string denoting the amount_remaining field in the database.
	FieldAmountRemaining = "amount_remaining"
	// FieldDescription holds the string denoting the description field in the database.
	FieldDescription = "description"
	// FieldDueDate holds the string denoting the due_date field in the database.
	FieldDueDate = "due_date"
	// FieldPaidAt holds the string denoting the paid_at field in the database.
	FieldPaidAt = "paid_at"
	// FieldVoidedAt holds the string denoting the voided_at field in the database.
	FieldVoidedAt = "voided_at"
	// FieldFinalizedAt holds the string denoting the finalized_at field in the database.
	FieldFinalizedAt = "finalized_at"
	// FieldBillingPeriod holds the string denoting the billing_period field in the database.
	FieldBillingPeriod = "billing_period"
	// FieldPeriodStart holds the string denoting the period_start field in the database.
	FieldPeriodStart = "period_start"
	// FieldPeriodEnd holds the string denoting the period_end field in the database.
	FieldPeriodEnd = "period_end"
	// FieldInvoicePdfURL holds the string denoting the invoice_pdf_url field in the database.
	FieldInvoicePdfURL = "invoice_pdf_url"
	// FieldBillingReason holds the string denoting the billing_reason field in the database.
	FieldBillingReason = "billing_reason"
	// FieldMetadata holds the string denoting the metadata field in the database.
	FieldMetadata = "metadata"
	// FieldVersion holds the string denoting the version field in the database.
	FieldVersion = "version"
	// FieldInvoiceNumber holds the string denoting the invoice_number field in the database.
	FieldInvoiceNumber = "invoice_number"
	// FieldBillingSequence holds the string denoting the billing_sequence field in the database.
	FieldBillingSequence = "billing_sequence"
	// FieldIdempotencyKey holds the string denoting the idempotency_key field in the database.
	FieldIdempotencyKey = "idempotency_key"
	// EdgeLineItems holds the string denoting the line_items edge name in mutations.
	EdgeLineItems = "line_items"
	// Table holds the table name of the invoice in the database.
	Table = "invoices"
	// LineItemsTable is the table that holds the line_items relation/edge.
	LineItemsTable = "invoice_line_items"
	// LineItemsInverseTable is the table name for the InvoiceLineItem entity.
	// It exists in this package in order to avoid circular dependency with the "invoicelineitem" package.
	LineItemsInverseTable = "invoice_line_items"
	// LineItemsColumn is the table column denoting the line_items relation/edge.
	LineItemsColumn = "invoice_id"
)

// Columns holds all SQL columns for invoice fields.
var Columns = []string{
	FieldID,
	FieldTenantID,
	FieldStatus,
	FieldCreatedAt,
	FieldUpdatedAt,
	FieldCreatedBy,
	FieldUpdatedBy,
	FieldCustomerID,
	FieldSubscriptionID,
	FieldInvoiceType,
	FieldInvoiceStatus,
	FieldPaymentStatus,
	FieldCurrency,
	FieldAmountDue,
	FieldAmountPaid,
	FieldAmountRemaining,
	FieldDescription,
	FieldDueDate,
	FieldPaidAt,
	FieldVoidedAt,
	FieldFinalizedAt,
	FieldBillingPeriod,
	FieldPeriodStart,
	FieldPeriodEnd,
	FieldInvoicePdfURL,
	FieldBillingReason,
	FieldMetadata,
	FieldVersion,
	FieldInvoiceNumber,
	FieldBillingSequence,
	FieldIdempotencyKey,
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
	// InvoiceTypeValidator is a validator for the "invoice_type" field. It is called by the builders before save.
	InvoiceTypeValidator func(string) error
	// DefaultInvoiceStatus holds the default value on creation for the "invoice_status" field.
	DefaultInvoiceStatus string
	// DefaultPaymentStatus holds the default value on creation for the "payment_status" field.
	DefaultPaymentStatus string
	// CurrencyValidator is a validator for the "currency" field. It is called by the builders before save.
	CurrencyValidator func(string) error
	// DefaultAmountDue holds the default value on creation for the "amount_due" field.
	DefaultAmountDue decimal.Decimal
	// DefaultAmountPaid holds the default value on creation for the "amount_paid" field.
	DefaultAmountPaid decimal.Decimal
	// DefaultAmountRemaining holds the default value on creation for the "amount_remaining" field.
	DefaultAmountRemaining decimal.Decimal
	// DefaultVersion holds the default value on creation for the "version" field.
	DefaultVersion int
)

// OrderOption defines the ordering options for the Invoice queries.
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

// ByCustomerID orders the results by the customer_id field.
func ByCustomerID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCustomerID, opts...).ToFunc()
}

// BySubscriptionID orders the results by the subscription_id field.
func BySubscriptionID(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldSubscriptionID, opts...).ToFunc()
}

// ByInvoiceType orders the results by the invoice_type field.
func ByInvoiceType(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldInvoiceType, opts...).ToFunc()
}

// ByInvoiceStatus orders the results by the invoice_status field.
func ByInvoiceStatus(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldInvoiceStatus, opts...).ToFunc()
}

// ByPaymentStatus orders the results by the payment_status field.
func ByPaymentStatus(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPaymentStatus, opts...).ToFunc()
}

// ByCurrency orders the results by the currency field.
func ByCurrency(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldCurrency, opts...).ToFunc()
}

// ByAmountDue orders the results by the amount_due field.
func ByAmountDue(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldAmountDue, opts...).ToFunc()
}

// ByAmountPaid orders the results by the amount_paid field.
func ByAmountPaid(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldAmountPaid, opts...).ToFunc()
}

// ByAmountRemaining orders the results by the amount_remaining field.
func ByAmountRemaining(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldAmountRemaining, opts...).ToFunc()
}

// ByDescription orders the results by the description field.
func ByDescription(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldDescription, opts...).ToFunc()
}

// ByDueDate orders the results by the due_date field.
func ByDueDate(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldDueDate, opts...).ToFunc()
}

// ByPaidAt orders the results by the paid_at field.
func ByPaidAt(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPaidAt, opts...).ToFunc()
}

// ByVoidedAt orders the results by the voided_at field.
func ByVoidedAt(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldVoidedAt, opts...).ToFunc()
}

// ByFinalizedAt orders the results by the finalized_at field.
func ByFinalizedAt(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldFinalizedAt, opts...).ToFunc()
}

// ByBillingPeriod orders the results by the billing_period field.
func ByBillingPeriod(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldBillingPeriod, opts...).ToFunc()
}

// ByPeriodStart orders the results by the period_start field.
func ByPeriodStart(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPeriodStart, opts...).ToFunc()
}

// ByPeriodEnd orders the results by the period_end field.
func ByPeriodEnd(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldPeriodEnd, opts...).ToFunc()
}

// ByInvoicePdfURL orders the results by the invoice_pdf_url field.
func ByInvoicePdfURL(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldInvoicePdfURL, opts...).ToFunc()
}

// ByBillingReason orders the results by the billing_reason field.
func ByBillingReason(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldBillingReason, opts...).ToFunc()
}

// ByVersion orders the results by the version field.
func ByVersion(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldVersion, opts...).ToFunc()
}

// ByInvoiceNumber orders the results by the invoice_number field.
func ByInvoiceNumber(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldInvoiceNumber, opts...).ToFunc()
}

// ByBillingSequence orders the results by the billing_sequence field.
func ByBillingSequence(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldBillingSequence, opts...).ToFunc()
}

// ByIdempotencyKey orders the results by the idempotency_key field.
func ByIdempotencyKey(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldIdempotencyKey, opts...).ToFunc()
}

// ByLineItemsCount orders the results by line_items count.
func ByLineItemsCount(opts ...sql.OrderTermOption) OrderOption {
	return func(s *sql.Selector) {
		sqlgraph.OrderByNeighborsCount(s, newLineItemsStep(), opts...)
	}
}

// ByLineItems orders the results by line_items terms.
func ByLineItems(term sql.OrderTerm, terms ...sql.OrderTerm) OrderOption {
	return func(s *sql.Selector) {
		sqlgraph.OrderByNeighborTerms(s, newLineItemsStep(), append([]sql.OrderTerm{term}, terms...)...)
	}
}
func newLineItemsStep() *sqlgraph.Step {
	return sqlgraph.NewStep(
		sqlgraph.From(Table, FieldID),
		sqlgraph.To(LineItemsInverseTable, FieldID),
		sqlgraph.Edge(sqlgraph.O2M, false, LineItemsTable, LineItemsColumn),
	)
}
