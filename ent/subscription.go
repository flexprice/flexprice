// Code generated by ent, DO NOT EDIT.

package ent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent/subscription"
)

// Subscription is the model entity for the Subscription schema.
type Subscription struct {
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
	// LookupKey holds the value of the "lookup_key" field.
	LookupKey string `json:"lookup_key,omitempty"`
	// CustomerID holds the value of the "customer_id" field.
	CustomerID string `json:"customer_id,omitempty"`
	// PlanID holds the value of the "plan_id" field.
	PlanID string `json:"plan_id,omitempty"`
	// SubscriptionStatus holds the value of the "subscription_status" field.
	SubscriptionStatus string `json:"subscription_status,omitempty"`
	// Currency holds the value of the "currency" field.
	Currency string `json:"currency,omitempty"`
	// BillingAnchor holds the value of the "billing_anchor" field.
	BillingAnchor time.Time `json:"billing_anchor,omitempty"`
	// StartDate holds the value of the "start_date" field.
	StartDate time.Time `json:"start_date,omitempty"`
	// EndDate holds the value of the "end_date" field.
	EndDate *time.Time `json:"end_date,omitempty"`
	// CurrentPeriodStart holds the value of the "current_period_start" field.
	CurrentPeriodStart time.Time `json:"current_period_start,omitempty"`
	// CurrentPeriodEnd holds the value of the "current_period_end" field.
	CurrentPeriodEnd time.Time `json:"current_period_end,omitempty"`
	// CancelledAt holds the value of the "cancelled_at" field.
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
	// CancelAt holds the value of the "cancel_at" field.
	CancelAt *time.Time `json:"cancel_at,omitempty"`
	// CancelAtPeriodEnd holds the value of the "cancel_at_period_end" field.
	CancelAtPeriodEnd bool `json:"cancel_at_period_end,omitempty"`
	// TrialStart holds the value of the "trial_start" field.
	TrialStart *time.Time `json:"trial_start,omitempty"`
	// TrialEnd holds the value of the "trial_end" field.
	TrialEnd *time.Time `json:"trial_end,omitempty"`
	// BillingCadence holds the value of the "billing_cadence" field.
	BillingCadence string `json:"billing_cadence,omitempty"`
	// BillingPeriod holds the value of the "billing_period" field.
	BillingPeriod string `json:"billing_period,omitempty"`
	// BillingPeriodCount holds the value of the "billing_period_count" field.
	BillingPeriodCount int `json:"billing_period_count,omitempty"`
	// Version holds the value of the "version" field.
	Version int `json:"version,omitempty"`
	// Metadata holds the value of the "metadata" field.
	Metadata map[string]string `json:"metadata,omitempty"`
	// PauseStatus holds the value of the "pause_status" field.
	PauseStatus string `json:"pause_status,omitempty"`
	// ActivePauseID holds the value of the "active_pause_id" field.
	ActivePauseID *string `json:"active_pause_id,omitempty"`
	// Edges holds the relations/edges for other nodes in the graph.
	// The values are being populated by the SubscriptionQuery when eager-loading is set.
	Edges        SubscriptionEdges `json:"edges"`
	selectValues sql.SelectValues
}

// SubscriptionEdges holds the relations/edges for other nodes in the graph.
type SubscriptionEdges struct {
	// LineItems holds the value of the line_items edge.
	LineItems []*SubscriptionLineItem `json:"line_items,omitempty"`
	// Pauses holds the value of the pauses edge.
	Pauses []*SubscriptionPause `json:"pauses,omitempty"`
	// loadedTypes holds the information for reporting if a
	// type was loaded (or requested) in eager-loading or not.
	loadedTypes [2]bool
}

// LineItemsOrErr returns the LineItems value or an error if the edge
// was not loaded in eager-loading.
func (e SubscriptionEdges) LineItemsOrErr() ([]*SubscriptionLineItem, error) {
	if e.loadedTypes[0] {
		return e.LineItems, nil
	}
	return nil, &NotLoadedError{edge: "line_items"}
}

// PausesOrErr returns the Pauses value or an error if the edge
// was not loaded in eager-loading.
func (e SubscriptionEdges) PausesOrErr() ([]*SubscriptionPause, error) {
	if e.loadedTypes[1] {
		return e.Pauses, nil
	}
	return nil, &NotLoadedError{edge: "pauses"}
}

// scanValues returns the types for scanning values from sql.Rows.
func (*Subscription) scanValues(columns []string) ([]any, error) {
	values := make([]any, len(columns))
	for i := range columns {
		switch columns[i] {
		case subscription.FieldMetadata:
			values[i] = new([]byte)
		case subscription.FieldCancelAtPeriodEnd:
			values[i] = new(sql.NullBool)
		case subscription.FieldBillingPeriodCount, subscription.FieldVersion:
			values[i] = new(sql.NullInt64)
		case subscription.FieldID, subscription.FieldTenantID, subscription.FieldStatus, subscription.FieldCreatedBy, subscription.FieldUpdatedBy, subscription.FieldEnvironmentID, subscription.FieldLookupKey, subscription.FieldCustomerID, subscription.FieldPlanID, subscription.FieldSubscriptionStatus, subscription.FieldCurrency, subscription.FieldBillingCadence, subscription.FieldBillingPeriod, subscription.FieldPauseStatus, subscription.FieldActivePauseID:
			values[i] = new(sql.NullString)
		case subscription.FieldCreatedAt, subscription.FieldUpdatedAt, subscription.FieldBillingAnchor, subscription.FieldStartDate, subscription.FieldEndDate, subscription.FieldCurrentPeriodStart, subscription.FieldCurrentPeriodEnd, subscription.FieldCancelledAt, subscription.FieldCancelAt, subscription.FieldTrialStart, subscription.FieldTrialEnd:
			values[i] = new(sql.NullTime)
		default:
			values[i] = new(sql.UnknownType)
		}
	}
	return values, nil
}

// assignValues assigns the values that were returned from sql.Rows (after scanning)
// to the Subscription fields.
func (s *Subscription) assignValues(columns []string, values []any) error {
	if m, n := len(values), len(columns); m < n {
		return fmt.Errorf("mismatch number of scan values: %d != %d", m, n)
	}
	for i := range columns {
		switch columns[i] {
		case subscription.FieldID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field id", values[i])
			} else if value.Valid {
				s.ID = value.String
			}
		case subscription.FieldTenantID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field tenant_id", values[i])
			} else if value.Valid {
				s.TenantID = value.String
			}
		case subscription.FieldStatus:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field status", values[i])
			} else if value.Valid {
				s.Status = value.String
			}
		case subscription.FieldCreatedAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field created_at", values[i])
			} else if value.Valid {
				s.CreatedAt = value.Time
			}
		case subscription.FieldUpdatedAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field updated_at", values[i])
			} else if value.Valid {
				s.UpdatedAt = value.Time
			}
		case subscription.FieldCreatedBy:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field created_by", values[i])
			} else if value.Valid {
				s.CreatedBy = value.String
			}
		case subscription.FieldUpdatedBy:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field updated_by", values[i])
			} else if value.Valid {
				s.UpdatedBy = value.String
			}
		case subscription.FieldEnvironmentID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field environment_id", values[i])
			} else if value.Valid {
				s.EnvironmentID = value.String
			}
		case subscription.FieldLookupKey:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field lookup_key", values[i])
			} else if value.Valid {
				s.LookupKey = value.String
			}
		case subscription.FieldCustomerID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field customer_id", values[i])
			} else if value.Valid {
				s.CustomerID = value.String
			}
		case subscription.FieldPlanID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field plan_id", values[i])
			} else if value.Valid {
				s.PlanID = value.String
			}
		case subscription.FieldSubscriptionStatus:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field subscription_status", values[i])
			} else if value.Valid {
				s.SubscriptionStatus = value.String
			}
		case subscription.FieldCurrency:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field currency", values[i])
			} else if value.Valid {
				s.Currency = value.String
			}
		case subscription.FieldBillingAnchor:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field billing_anchor", values[i])
			} else if value.Valid {
				s.BillingAnchor = value.Time
			}
		case subscription.FieldStartDate:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field start_date", values[i])
			} else if value.Valid {
				s.StartDate = value.Time
			}
		case subscription.FieldEndDate:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field end_date", values[i])
			} else if value.Valid {
				s.EndDate = new(time.Time)
				*s.EndDate = value.Time
			}
		case subscription.FieldCurrentPeriodStart:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field current_period_start", values[i])
			} else if value.Valid {
				s.CurrentPeriodStart = value.Time
			}
		case subscription.FieldCurrentPeriodEnd:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field current_period_end", values[i])
			} else if value.Valid {
				s.CurrentPeriodEnd = value.Time
			}
		case subscription.FieldCancelledAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field cancelled_at", values[i])
			} else if value.Valid {
				s.CancelledAt = new(time.Time)
				*s.CancelledAt = value.Time
			}
		case subscription.FieldCancelAt:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field cancel_at", values[i])
			} else if value.Valid {
				s.CancelAt = new(time.Time)
				*s.CancelAt = value.Time
			}
		case subscription.FieldCancelAtPeriodEnd:
			if value, ok := values[i].(*sql.NullBool); !ok {
				return fmt.Errorf("unexpected type %T for field cancel_at_period_end", values[i])
			} else if value.Valid {
				s.CancelAtPeriodEnd = value.Bool
			}
		case subscription.FieldTrialStart:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field trial_start", values[i])
			} else if value.Valid {
				s.TrialStart = new(time.Time)
				*s.TrialStart = value.Time
			}
		case subscription.FieldTrialEnd:
			if value, ok := values[i].(*sql.NullTime); !ok {
				return fmt.Errorf("unexpected type %T for field trial_end", values[i])
			} else if value.Valid {
				s.TrialEnd = new(time.Time)
				*s.TrialEnd = value.Time
			}
		case subscription.FieldBillingCadence:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field billing_cadence", values[i])
			} else if value.Valid {
				s.BillingCadence = value.String
			}
		case subscription.FieldBillingPeriod:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field billing_period", values[i])
			} else if value.Valid {
				s.BillingPeriod = value.String
			}
		case subscription.FieldBillingPeriodCount:
			if value, ok := values[i].(*sql.NullInt64); !ok {
				return fmt.Errorf("unexpected type %T for field billing_period_count", values[i])
			} else if value.Valid {
				s.BillingPeriodCount = int(value.Int64)
			}
		case subscription.FieldVersion:
			if value, ok := values[i].(*sql.NullInt64); !ok {
				return fmt.Errorf("unexpected type %T for field version", values[i])
			} else if value.Valid {
				s.Version = int(value.Int64)
			}
		case subscription.FieldMetadata:
			if value, ok := values[i].(*[]byte); !ok {
				return fmt.Errorf("unexpected type %T for field metadata", values[i])
			} else if value != nil && len(*value) > 0 {
				if err := json.Unmarshal(*value, &s.Metadata); err != nil {
					return fmt.Errorf("unmarshal field metadata: %w", err)
				}
			}
		case subscription.FieldPauseStatus:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field pause_status", values[i])
			} else if value.Valid {
				s.PauseStatus = value.String
			}
		case subscription.FieldActivePauseID:
			if value, ok := values[i].(*sql.NullString); !ok {
				return fmt.Errorf("unexpected type %T for field active_pause_id", values[i])
			} else if value.Valid {
				s.ActivePauseID = new(string)
				*s.ActivePauseID = value.String
			}
		default:
			s.selectValues.Set(columns[i], values[i])
		}
	}
	return nil
}

// Value returns the ent.Value that was dynamically selected and assigned to the Subscription.
// This includes values selected through modifiers, order, etc.
func (s *Subscription) Value(name string) (ent.Value, error) {
	return s.selectValues.Get(name)
}

// QueryLineItems queries the "line_items" edge of the Subscription entity.
func (s *Subscription) QueryLineItems() *SubscriptionLineItemQuery {
	return NewSubscriptionClient(s.config).QueryLineItems(s)
}

// QueryPauses queries the "pauses" edge of the Subscription entity.
func (s *Subscription) QueryPauses() *SubscriptionPauseQuery {
	return NewSubscriptionClient(s.config).QueryPauses(s)
}

// Update returns a builder for updating this Subscription.
// Note that you need to call Subscription.Unwrap() before calling this method if this Subscription
// was returned from a transaction, and the transaction was committed or rolled back.
func (s *Subscription) Update() *SubscriptionUpdateOne {
	return NewSubscriptionClient(s.config).UpdateOne(s)
}

// Unwrap unwraps the Subscription entity that was returned from a transaction after it was closed,
// so that all future queries will be executed through the driver which created the transaction.
func (s *Subscription) Unwrap() *Subscription {
	_tx, ok := s.config.driver.(*txDriver)
	if !ok {
		panic("ent: Subscription is not a transactional entity")
	}
	s.config.driver = _tx.drv
	return s
}

// String implements the fmt.Stringer.
func (s *Subscription) String() string {
	var builder strings.Builder
	builder.WriteString("Subscription(")
	builder.WriteString(fmt.Sprintf("id=%v, ", s.ID))
	builder.WriteString("tenant_id=")
	builder.WriteString(s.TenantID)
	builder.WriteString(", ")
	builder.WriteString("status=")
	builder.WriteString(s.Status)
	builder.WriteString(", ")
	builder.WriteString("created_at=")
	builder.WriteString(s.CreatedAt.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("updated_at=")
	builder.WriteString(s.UpdatedAt.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("created_by=")
	builder.WriteString(s.CreatedBy)
	builder.WriteString(", ")
	builder.WriteString("updated_by=")
	builder.WriteString(s.UpdatedBy)
	builder.WriteString(", ")
	builder.WriteString("environment_id=")
	builder.WriteString(s.EnvironmentID)
	builder.WriteString(", ")
	builder.WriteString("lookup_key=")
	builder.WriteString(s.LookupKey)
	builder.WriteString(", ")
	builder.WriteString("customer_id=")
	builder.WriteString(s.CustomerID)
	builder.WriteString(", ")
	builder.WriteString("plan_id=")
	builder.WriteString(s.PlanID)
	builder.WriteString(", ")
	builder.WriteString("subscription_status=")
	builder.WriteString(s.SubscriptionStatus)
	builder.WriteString(", ")
	builder.WriteString("currency=")
	builder.WriteString(s.Currency)
	builder.WriteString(", ")
	builder.WriteString("billing_anchor=")
	builder.WriteString(s.BillingAnchor.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("start_date=")
	builder.WriteString(s.StartDate.Format(time.ANSIC))
	builder.WriteString(", ")
	if v := s.EndDate; v != nil {
		builder.WriteString("end_date=")
		builder.WriteString(v.Format(time.ANSIC))
	}
	builder.WriteString(", ")
	builder.WriteString("current_period_start=")
	builder.WriteString(s.CurrentPeriodStart.Format(time.ANSIC))
	builder.WriteString(", ")
	builder.WriteString("current_period_end=")
	builder.WriteString(s.CurrentPeriodEnd.Format(time.ANSIC))
	builder.WriteString(", ")
	if v := s.CancelledAt; v != nil {
		builder.WriteString("cancelled_at=")
		builder.WriteString(v.Format(time.ANSIC))
	}
	builder.WriteString(", ")
	if v := s.CancelAt; v != nil {
		builder.WriteString("cancel_at=")
		builder.WriteString(v.Format(time.ANSIC))
	}
	builder.WriteString(", ")
	builder.WriteString("cancel_at_period_end=")
	builder.WriteString(fmt.Sprintf("%v", s.CancelAtPeriodEnd))
	builder.WriteString(", ")
	if v := s.TrialStart; v != nil {
		builder.WriteString("trial_start=")
		builder.WriteString(v.Format(time.ANSIC))
	}
	builder.WriteString(", ")
	if v := s.TrialEnd; v != nil {
		builder.WriteString("trial_end=")
		builder.WriteString(v.Format(time.ANSIC))
	}
	builder.WriteString(", ")
	builder.WriteString("billing_cadence=")
	builder.WriteString(s.BillingCadence)
	builder.WriteString(", ")
	builder.WriteString("billing_period=")
	builder.WriteString(s.BillingPeriod)
	builder.WriteString(", ")
	builder.WriteString("billing_period_count=")
	builder.WriteString(fmt.Sprintf("%v", s.BillingPeriodCount))
	builder.WriteString(", ")
	builder.WriteString("version=")
	builder.WriteString(fmt.Sprintf("%v", s.Version))
	builder.WriteString(", ")
	builder.WriteString("metadata=")
	builder.WriteString(fmt.Sprintf("%v", s.Metadata))
	builder.WriteString(", ")
	builder.WriteString("pause_status=")
	builder.WriteString(s.PauseStatus)
	builder.WriteString(", ")
	if v := s.ActivePauseID; v != nil {
		builder.WriteString("active_pause_id=")
		builder.WriteString(*v)
	}
	builder.WriteByte(')')
	return builder.String()
}

// Subscriptions is a parsable slice of Subscription.
type Subscriptions []*Subscription
