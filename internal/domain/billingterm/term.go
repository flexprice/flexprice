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

	// Amount is nil for a remainder term; Percentage keeps what the tenant entered, for display.
	Amount     *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
	Percentage *decimal.Decimal `json:"percentage,omitempty" swaggertype:"string"`

	// Once terms.
	BillTrigger *types.InstalmentBillTrigger `json:"bill_trigger,omitempty"`
	BillOn      *time.Time                   `json:"bill_on,omitempty"`
	CoversFrom  *time.Time                   `json:"covers_from,omitempty"`
	CoversTo    *time.Time                   `json:"covers_to,omitempty"`

	// Recurring terms.
	StartsOn       *time.Time              `json:"starts_on,omitempty"`
	EndsOn         *time.Time              `json:"ends_on,omitempty"`
	PeriodCount    *int                    `json:"period_count,omitempty"`
	InvoiceCadence *types.InvoiceCadence   `json:"invoice_cadence,omitempty"`
	Split          *types.BillingTermSplit `json:"split,omitempty"`

	Description *string        `json:"description,omitempty"`
	Metadata    types.Metadata `json:"metadata,omitempty" activity:"drop"`

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
		Amount:         e.Amount,
		Percentage:     e.Percentage,
		BillTrigger:    e.BillTrigger,
		BillOn:         e.BillOn,
		CoversFrom:     e.CoversFrom,
		CoversTo:       e.CoversTo,
		StartsOn:       e.StartsOn,
		EndsOn:         e.EndsOn,
		PeriodCount:    e.PeriodCount,
		InvoiceCadence: e.InvoiceCadence,
		Split:          e.Split,
		Description:    e.Description,
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
