package billingterm

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Term is one billing term of an owner: a payment rule that produces the owner's instalments.
type Term struct {
	ID string `json:"id"`

	OwnerType types.BillingTermOwnerType `json:"owner_type"`
	OwnerID   string                     `json:"owner_id"`

	Kind      types.BillingTermKind      `json:"kind"`
	ValueType types.BillingTermValueType `json:"value_type"`

	// Value is the amount of a fixed term, per instalment on a recurring one; it is nil for a remainder term.
	Value *decimal.Decimal `json:"value,omitempty" swaggertype:"string"`

	// Once terms.
	BillTrigger *types.InstalmentBillTrigger `json:"bill_trigger,omitempty"`
	BillOn      *time.Time                   `json:"bill_on,omitempty"`

	// Recurring terms.
	StartDate      *time.Time              `json:"start_date,omitempty"`
	EndDate        *time.Time              `json:"end_date,omitempty"`
	PeriodCount    *int                    `json:"period_count,omitempty"`
	InvoiceCadence *types.InvoiceCadence   `json:"invoice_cadence,omitempty"`
	Split          *types.BillingTermSplit `json:"split,omitempty"`

	// InvoiceLabel names its instalments on the invoice, after the owner's label. It may use {n} and {of}, which number
	// all of the owner's instalments together.
	InvoiceLabel *string        `json:"invoice_label,omitempty"`
	Metadata     types.Metadata `json:"metadata,omitempty" activity:"drop"`

	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// TermFromEnt converts an ent row to a Term; it returns nil for a nil row.
func TermFromEnt(e *ent.BillingTerm) *Term {
	if e == nil {
		return nil
	}

	return &Term{
		ID:             e.ID,
		OwnerType:      e.OwnerType,
		OwnerID:        e.OwnerID,
		Kind:           e.Kind,
		ValueType:      e.ValueType,
		Value:          e.Value,
		BillTrigger:    e.BillTrigger,
		BillOn:         e.BillOn,
		StartDate:      e.StartDate,
		EndDate:        e.EndDate,
		PeriodCount:    e.PeriodCount,
		InvoiceCadence: e.InvoiceCadence,
		Split:          e.Split,
		InvoiceLabel:   e.InvoiceLabel,
		Metadata:       e.Metadata,
		EnvironmentID:  e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// TermsFromEnt converts ent rows to Terms.
func TermsFromEnt(list []*ent.BillingTerm) []*Term {
	out := make([]*Term, 0, len(list))
	for _, e := range list {
		out = append(out, TermFromEnt(e))
	}

	return out
}
