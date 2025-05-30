// Code generated by ent, DO NOT EDIT.

package meter

import (
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent/schema"
)

const (
	// Label holds the string label denoting the meter type in the database.
	Label = "meter"
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
	// FieldEventName holds the string denoting the event_name field in the database.
	FieldEventName = "event_name"
	// FieldName holds the string denoting the name field in the database.
	FieldName = "name"
	// FieldAggregation holds the string denoting the aggregation field in the database.
	FieldAggregation = "aggregation"
	// FieldFilters holds the string denoting the filters field in the database.
	FieldFilters = "filters"
	// FieldResetUsage holds the string denoting the reset_usage field in the database.
	FieldResetUsage = "reset_usage"
	// Table holds the table name of the meter in the database.
	Table = "meters"
)

// Columns holds all SQL columns for meter fields.
var Columns = []string{
	FieldID,
	FieldTenantID,
	FieldStatus,
	FieldCreatedAt,
	FieldUpdatedAt,
	FieldCreatedBy,
	FieldUpdatedBy,
	FieldEnvironmentID,
	FieldEventName,
	FieldName,
	FieldAggregation,
	FieldFilters,
	FieldResetUsage,
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
	// EventNameValidator is a validator for the "event_name" field. It is called by the builders before save.
	EventNameValidator func(string) error
	// NameValidator is a validator for the "name" field. It is called by the builders before save.
	NameValidator func(string) error
	// DefaultAggregation holds the default value on creation for the "aggregation" field.
	DefaultAggregation schema.MeterAggregation
	// DefaultFilters holds the default value on creation for the "filters" field.
	DefaultFilters []schema.MeterFilter
	// DefaultResetUsage holds the default value on creation for the "reset_usage" field.
	DefaultResetUsage string
)

// OrderOption defines the ordering options for the Meter queries.
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

// ByEventName orders the results by the event_name field.
func ByEventName(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldEventName, opts...).ToFunc()
}

// ByName orders the results by the name field.
func ByName(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldName, opts...).ToFunc()
}

// ByResetUsage orders the results by the reset_usage field.
func ByResetUsage(opts ...sql.OrderTermOption) OrderOption {
	return sql.OrderByField(FieldResetUsage, opts...).ToFunc()
}
