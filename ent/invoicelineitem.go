// Code generated by ent, DO NOT EDIT.

package ent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent/invoice"
	"github.com/flexprice/flexprice/ent/invoicelineitem"
	"github.com/shopspring/decimal"
)

// InvoiceLineItem is the model entity for the InvoiceLineItem schema.
type InvoiceLineItem struct {
	config `json:"-"`
	// ID of the ent.
	ID string `json:"id,omitempty"`
	// TenantID holds the value of the "tenant_id" field.
	TenantID string `json:"tenant_id,omitempty"`
	// Status holds the value of the "status" field.
	Status string `json:"status,omitempty"`
	// CreatedAt holds the value of the "created_at" field.
	CreatedAt time.Time `json:"created_at,omitempty"`
	// UpdatedAt holds the value of the "updated_at" field.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	// CreatedBy holds the value of the "created_by" field.
	CreatedBy string `json:"created_by,omitempty"`
	// UpdatedBy holds the value of the "updated_by" field.
	UpdatedBy string `json:"updated_by,omitempty"`
	// EnvironmentID holds the value of the "environment_id" field.
	EnvironmentID string `json:"environment_id,omitempty"`
	// InvoiceID holds the value of the "invoice_id" field.
	InvoiceID string `json:"invoice_id,omitempty"`
	// CustomerID holds the value of the "customer_id" field.
	CustomerID string `json:"customer_id,omitempty"`
	// SubscriptionID holds the value of the "subscription_id" field.
	SubscriptionID *string `json:"subscription_id,omitempty"`
	// PlanID holds the value of the "plan_id" field.
	PlanID *string `json:"plan_id,omitempty"`
	// PlanDisplayName holds the value of the "plan_display_name" field.
	PlanDisplayName *string `json:"plan_display_name,omitempty"`
	// PriceID holds the value of the "price_id" field.
	PriceID string `json:"price_id,omitempty"`
	// PriceType holds the value of the "price_type" field.
	PriceType *string `json:"price_type,omitempty"`
	// MeterID holds the value of the "meter_id" field.
	MeterID *string `json:"meter_id,omitempty"`
	// MeterDisplayName holds the value of the "meter_display_name" field.
	MeterDisplayName *string `json:"meter_display_name,omitempty"`
	// DisplayName holds the value of the "display_name" field.
	DisplayName *string `json:"display_name,omitempty"`
	// Amount holds the value of the "amount" field.
	Amount decimal.Decimal `json:"amount,omitempty"`
	// Quantity holds the value of the "quantity" field.
	Quantity decimal.Decimal `json:"quantity,omitempty"`
	// Currency holds the value of the "currency" field.
	Currency string `json:"currency,omitempty"`
	// PeriodStart holds the value of the "period_start" field.
	PeriodStart *time.Time `json:"period_start,omitempty"`
	// PeriodEnd holds the value of the "period_end" field.
	PeriodEnd *time.Time `json:"period_end,omitempty"`
	// Metadata holds the value of the "metadata" field.
	Metadata map[string]string `json:"metadata,omitempty"`
	// Edges holds the relations/edges for other nodes in the graph.
	// The values are being populated by the InvoiceLineItemQuery when eager-loading is set.
	Edges        InvoiceLineItemEdges `json:"edges"`
	selectValues sql.SelectValues
}

// InvoiceLineItemEdges holds the relations/edges for other nodes in the graph.
type InvoiceLineItemEdges struct {
	// Invoice holds the value of the invoice edge.
	Invoice *Invoice `json:"invoice,omitempty"`
	// loadedTypes holds the information for reporting if a
	// type was loaded (or requested) in eager-loading or not.
	loadedTypes [1]bool
}

// InvoiceOrErr returns the Invoice value or an error if the edge
// was not loaded in eager-loading, or loaded but was not found.
func (e InvoiceLineItemEdges) InvoiceOrErr() (*Invoice, error) {
	if e.Invoice != nil {
		return e.Invoice, nil
	} else if e.loadedTypes[0] {
		return nil, &NotFoundError{label: invoice.Label}
	}
	return nil, &NotLoadedError{edge: "invoice"}
}

// scanValues returns the types for scanning values from sql.Rows.
func (*InvoiceLineItem) scanValues(columns []string) ([]any, error) {
	values := make([]any, len(columns))
	for i := range columns {
		switch columns[i] {
		case invoicelineitem.FieldMetadata:
			values[i] = new([]byte)
		case invoicelineitem.FieldAmount, invoicelineitem.FieldQuantity:
			values[i] = new(decimal.Decimal)
		case invoicelineitem.FieldID, invoicelineitem.FieldTenantID, invoicelineitem.FieldStatus, invoicelineitem.FieldCreatedBy, invoicelineitem.FieldUpdatedBy, invoicelineitem.FieldEnvironmentID, invoicelineitem.FieldInvoiceID, invoicelineitem.FieldCustomerID, invoicelineitem.FieldSubscriptionID, invoicelineitem.FieldPlanID, invoicelineitem.FieldPlanDisplayName, invoicelineitem.FieldPriceID, invoicelineitem.FieldPriceType, invoicelineitem.FieldMeterID, invoicelineitem.FieldMeterDisplayName, invoicelineitem.FieldDisplayName, invoicelineitem.FieldCurrency:
			values[i] = new(sql.NullString)
		case invoicelineitem.FieldCreatedAt, invoicelineitem.FieldUpdatedAt, invoicelineitem.FieldPeriodStart, invoicelineitem.FieldPeriodEnd:
			values[i] = new(sql.NullTime)
		default:
			values[i] = new(sql.UnknownType)
		}
	}
	return values, nil
}

// assignValues assigns the values that were returned from sql.Rows (after scanning)
// to the InvoiceLineItem fields.
func (ili *InvoiceLineItem) assignValues(columns []string, values []any) error {
	if m, n := len(values), len(columns); m < n {
		return fmt.Errorf("mismatch number of scan values: %d != %d", m, n)
	}
	for i := range columns {
		switch columns[i] {
		case invoicelineitem.FieldID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field id", values[i])
			} else if value.Valid {
				ili.ID = value.String
			}
		case invoicelineitem.FieldTenantID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field tenant_id", values[i])
			} else if value.Valid {
				ili.TenantID = value.String
			}
		case invoicelineitem.FieldStatus:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field status", values[i])
			} else if value.Valid {
				ili.Status = value.String
			}
		case invoicelineitem.FieldCreatedAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field created_at", values[i])
			} else if value.Valid {
				ili.CreatedAt = value.Time
			}
		case invoicelineitem.FieldUpdatedAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field updated_at", values[i])
			} else if value.Valid {
				ili.UpdatedAt = value.Time
			}
		case invoicelineitem.FieldCreatedBy:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field created_by", values[i])
			} else if value.Valid {
				ili.CreatedBy = value.String
			}
		case invoicelineitem.FieldUpdatedBy:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field updated_by", values[i])
			} else if value.Valid {
				ili.UpdatedBy = value.String
			}
		case invoicelineitem.FieldEnvironmentID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field environment_id", values[i])
			} else if value.Valid {
				ili.EnvironmentID = value.String
			}
		case invoicelineitem.FieldInvoiceID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field invoice_id", values[i])
			} else if value.Valid {
				ili.InvoiceID = value.String
			}
		case invoicelineitem.FieldCustomerID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field customer_id", values[i])
			} else if value.Valid {
				ili.CustomerID = value.String
			}
		case invoicelineitem.FieldSubscriptionID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field subscription_id", values[i])
			} else if value.Valid {
				ili.SubscriptionID = new(string)
				*ili.SubscriptionID = value.String
			}
		case invoicelineitem.FieldPlanID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field plan_id", values[i])
			} else if value.Valid {
				ili.PlanID = new(string)
				*ili.PlanID = value.String
			}
		case invoicelineitem.FieldPlanDisplayName:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field plan_display_name", values[i])
			} else if value.Valid {
				ili.PlanDisplayName = new(string)
				*ili.PlanDisplayName = value.String
			}
		case invoicelineitem.FieldPriceID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field price_id", values[i])
			} else if value.Valid {
				ili.PriceID = value.String
			}
		case invoicelineitem.FieldPriceType:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field price_type", values[i])
			} else if value.Valid {
				ili.PriceType = new(string)
				*ili.PriceType = value.String
			}
		case invoicelineitem.FieldMeterID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field meter_id", values[i])
			} else if value.Valid {
				ili.MeterID = new(string)
				*ili.MeterID = value.String
			}
		case invoicelineitem.FieldMeterDisplayName:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field meter_display_name", values[i])
			} else if value.Valid {
				ili.MeterDisplayName = new(string)
				*ili.MeterDisplayName = value.String
			}
		case invoicelineitem.FieldDisplayName:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field display_name", values[i])
			} else if value.Valid {
				ili.DisplayName = new(string)
				*ili.DisplayName = value.String
			}
		case invoicelineitem.FieldAmount:
			if value, ok := values[i].(*decimal.Decimal); !ok {
				return fmt.Errorf("unexpected type %T for field amount", values[i])
			} else if value != nil {
				ili.Amount = *value
			}
		case invoicelineitem.FieldQuantity:
			if value, ok := values[i].(*decimal.Decimal); !ok {
				return fmt.Errorf("unexpected type %T for field quantity", values[i])
			} else if value != nil {
				ili.Quantity = *value
			}
		case invoicelineitem.FieldCurrency:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field currency", values[i])
			} else if value.Valid {
				ili.Currency = value.String
			}
		case invoicelineitem.FieldPeriodStart:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field period_start", values[i])
			} else if value.Valid {
				ili.PeriodStart = new(time.Time)
				*ili.PeriodStart = value.Time
			}
		case invoicelineitem.FieldPeriodEnd:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field period_end", values[i])
			} else if value.Valid {
				ili.PeriodEnd = new(time.Time)
				*ili.PeriodEnd = value.Time
			}
		case invoicelineitem.FieldMetadata:
			if value, ok := values[i].(*[]byte); !ok {
				return fmt.Errorf("unexpected type %T for field metadata", values[i])
			} else if value != nil && len(*value) > 0 {
				if err := json.Unmarshal(*value, &ili.Metadata); err != nil {
					return fmt.Errorf("unmarshal field metadata: %w", err)
				}
			}
		default:
			ili.selectValues.Set(columns[i], values[i])
		}
	}
	return nil
}

// Value returns the ent.Value that was dynamically selected and assigned to the InvoiceLineItem.
// This includes values selected through modifiers, order, etc.
func (ili *InvoiceLineItem) Value(name string) (ent.Value, error) {
	return ili.selectValues.Get(name)
}

// QueryInvoice queries the "invoice" edge of the InvoiceLineItem entity.
func (ili *InvoiceLineItem) QueryInvoice() *InvoiceQuery {
	return NewInvoiceLineItemClient(ili.config).QueryInvoice(ili)
}

// Update returns a builder for updating this InvoiceLineItem.
// Note that you need to call InvoiceLineItem.Unwrap() before calling this method if this InvoiceLineItem
// was returned from a transaction, and the transaction was committed or rolled back.
func (ili *InvoiceLineItem) Update() *InvoiceLineItemUpdateOne {
	return NewInvoiceLineItemClient(ili.config).UpdateOne(ili)
}

// Unwrap unwraps the InvoiceLineItem entity that was returned from a transaction after it was closed,
// so that all future queries will be executed through the driver which created the transaction.
func (ili *InvoiceLineItem) Unwrap() *InvoiceLineItem {
	_tx, ok := ili.config.driver.(*txDriver)
	if !ok {
		panic("ent: InvoiceLineItem is not a transactional entity")
	}
	ili.config.driver = _tx.drv
	return ili
}

// String implements the fmt.Stringer.
func (ili *InvoiceLineItem) String() string {
	var builder strings.Builder
	builder.WriteString("InvoiceLineItem(")
	builder.WriteString(fmt.Sprintf("id=%v, ", ili.ID))
	builder.WriteString("tenant_id=")
	builder.WriteString(ili.TenantID)
	builder.WriteString(", ")
	builder.WriteString("status=")
	builder.WriteString(ili.Status)
	builder.WriteString(", ")
	builder.WriteString("created_at=")
	builder.WriteString(ili.CreatedAt.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("updated_at=")
	builder.WriteString(ili.UpdatedAt.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("created_by=")
	builder.WriteString(ili.CreatedBy)
	builder.WriteString(", ")
	builder.WriteString("updated_by=")
	builder.WriteString(ili.UpdatedBy)
	builder.WriteString(", ")
	builder.WriteString("environment_id=")
	builder.WriteString(ili.EnvironmentID)
	builder.WriteString(", ")
	builder.WriteString("invoice_id=")
	builder.WriteString(ili.InvoiceID)
	builder.WriteString(", ")
	builder.WriteString("customer_id=")
	builder.WriteString(ili.CustomerID)
	builder.WriteString(", ")
	if v := ili.SubscriptionID; v != nil {
		builder.WriteString("subscription_id=")
		builder.WriteString(*v)
	}
	builder.WriteString(", ")
	if v := ili.PlanID; v != nil {
		builder.WriteString("plan_id=")
		builder.WriteString(*v)
	}
	builder.WriteString(", ")
	if v := ili.PlanDisplayName; v != nil {
		builder.WriteString("plan_display_name=")
		builder.WriteString(*v)
	}
	builder.WriteString(", ")
	builder.WriteString("price_id=")
	builder.WriteString(ili.PriceID)
	builder.WriteString(", ")
	if v := ili.PriceType; v != nil {
		builder.WriteString("price_type=")
		builder.WriteString(*v)
	}
	builder.WriteString(", ")
	if v := ili.MeterID; v != nil {
		builder.WriteString("meter_id=")
		builder.WriteString(*v)
	}
	builder.WriteString(", ")
	if v := ili.MeterDisplayName; v != nil {
		builder.WriteString("meter_display_name=")
		builder.WriteString(*v)
	}
	builder.WriteString(", ")
	if v := ili.DisplayName; v != nil {
		builder.WriteString("display_name=")
		builder.WriteString(*v)
	}
	builder.WriteString(", ")
	builder.WriteString("amount=")
	builder.WriteString(fmt.Sprintf("%v", ili.Amount))
	builder.WriteString(", ")
	builder.WriteString("quantity=")
	builder.WriteString(fmt.Sprintf("%v", ili.Quantity))
	builder.WriteString(", ")
	builder.WriteString("currency=")
	builder.WriteString(ili.Currency)
	builder.WriteString(", ")
	if v := ili.PeriodStart; v != nil {
		builder.WriteString("period_start=")
		builder.WriteString(v.Format(time.ANSIC))
	}
	builder.WriteString(", ")
	if v := ili.PeriodEnd; v != nil {
		builder.WriteString("period_end=")
		builder.WriteString(v.Format(time.ANSIC))
	}
	builder.WriteString(", ")
	builder.WriteString("metadata=")
	builder.WriteString(fmt.Sprintf("%v", ili.Metadata))
	builder.WriteByte(')')
	return builder.String()
}

// InvoiceLineItems is a parsable slice of InvoiceLineItem.
type InvoiceLineItems []*InvoiceLineItem
