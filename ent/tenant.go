// Code generated by ent, DO NOT EDIT.

package ent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/ent/tenant"
)

// Tenant is the model entity for the Tenant schema.
type Tenant struct {
	config `json:"-"`
	// ID of the ent.
	ID string `json:"id,omitempty"`
	// Name holds the value of the "name" field.
	Name string `json:"name,omitempty"`
	// Status holds the value of the "status" field.
	Status string `json:"status,omitempty"`
	// CreatedAt holds the value of the "created_at" field.
	CreatedAt time.Time `json:"created_at,omitempty"`
	// UpdatedAt holds the value of the "updated_at" field.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	// BillingDetails holds the value of the "billing_details" field.
	BillingDetails schema.TenantBillingDetails `json:"billing_details,omitempty"`
	selectValues   sql.SelectValues
}

// scanValues returns the types for scanning values from sql.Rows.
func (*Tenant) scanValues(columns []string) ([]any, error) {
	values := make([]any, len(columns))
	for i := range columns {
		switch columns[i] {
		case tenant.FieldBillingDetails:
			values[i] = new([]byte)
		case tenant.FieldID, tenant.FieldName, tenant.FieldStatus:
			values[i] = new(sql.NullString)
		case tenant.FieldCreatedAt, tenant.FieldUpdatedAt:
			values[i] = new(sql.NullTime)
		default:
			values[i] = new(sql.UnknownType)
		}
	}
	return values, nil
}

// assignValues assigns the values that were returned from sql.Rows (after scanning)
// to the Tenant fields.
func (t *Tenant) assignValues(columns []string, values []any) error {
	if m, n := len(values), len(columns); m < n {
		return fmt.Errorf("mismatch number of scan values: %d != %d", m, n)
	}
	for i := range columns {
		switch columns[i] {
		case tenant.FieldID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field id", values[i])
			} else if value.Valid {
				t.ID = value.String
			}
		case tenant.FieldName:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field name", values[i])
			} else if value.Valid {
				t.Name = value.String
			}
		case tenant.FieldStatus:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field status", values[i])
			} else if value.Valid {
				t.Status = value.String
			}
		case tenant.FieldCreatedAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field created_at", values[i])
			} else if value.Valid {
				t.CreatedAt = value.Time
			}
		case tenant.FieldUpdatedAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field updated_at", values[i])
			} else if value.Valid {
				t.UpdatedAt = value.Time
			}
		case tenant.FieldBillingDetails:
			if value, ok := values[i].(*[]byte); !ok {
				return fmt.Errorf("unexpected type %T for field billing_details", values[i])
			} else if value != nil && len(*value) > 0 {
				if err := json.Unmarshal(*value, &t.BillingDetails); err != nil {
					return fmt.Errorf("unmarshal field billing_details: %w", err)
				}
			}
		default:
			t.selectValues.Set(columns[i], values[i])
		}
	}
	return nil
}

// Value returns the ent.Value that was dynamically selected and assigned to the Tenant.
// This includes values selected through modifiers, order, etc.
func (t *Tenant) Value(name string) (ent.Value, error) {
	return t.selectValues.Get(name)
}

// Update returns a builder for updating this Tenant.
// Note that you need to call Tenant.Unwrap() before calling this method if this Tenant
// was returned from a transaction, and the transaction was committed or rolled back.
func (t *Tenant) Update() *TenantUpdateOne {
	return NewTenantClient(t.config).UpdateOne(t)
}

// Unwrap unwraps the Tenant entity that was returned from a transaction after it was closed,
// so that all future queries will be executed through the driver which created the transaction.
func (t *Tenant) Unwrap() *Tenant {
	_tx, ok := t.config.driver.(*txDriver)
	if !ok {
		panic("ent: Tenant is not a transactional entity")
	}
	t.config.driver = _tx.drv
	return t
}

// String implements the fmt.Stringer.
func (t *Tenant) String() string {
	var builder strings.Builder
	builder.WriteString("Tenant(")
	builder.WriteString(fmt.Sprintf("id=%v, ", t.ID))
	builder.WriteString("name=")
	builder.WriteString(t.Name)
	builder.WriteString(", ")
	builder.WriteString("status=")
	builder.WriteString(t.Status)
	builder.WriteString(", ")
	builder.WriteString("created_at=")
	builder.WriteString(t.CreatedAt.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("updated_at=")
	builder.WriteString(t.UpdatedAt.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("billing_details=")
	builder.WriteString(fmt.Sprintf("%v", t.BillingDetails))
	builder.WriteByte(')')
	return builder.String()
}

// Tenants is a parsable slice of Tenant.
type Tenants []*Tenant
