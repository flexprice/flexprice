package billingterm

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// Instalment is one scheduled payment of an owner, billed on a subscription invoice or a separate one.
type Instalment struct {
	ID string `json:"id"`

	OwnerType     types.BillingTermOwnerType `json:"owner_type"`
	OwnerID       string                     `json:"owner_id"`
	BillingTermID *string                    `json:"billing_term_id,omitempty"`

	CustomerID     string  `json:"customer_id"`
	SubscriptionID *string `json:"subscription_id,omitempty"`
	Currency       string  `json:"currency"`

	Kind        types.InstalmentKind        `json:"kind"`
	BillTrigger types.InstalmentBillTrigger `json:"bill_trigger"`
	BillOn      time.Time                   `json:"bill_on"`
	TriggeredAt *time.Time                  `json:"triggered_at,omitempty"`

	Amount       decimal.Decimal `json:"amount" swaggertype:"string"`
	InvoiceLabel string          `json:"invoice_label"`

	ServicePeriodStart time.Time `json:"service_period_start"`
	ServicePeriodEnd   time.Time `json:"service_period_end"`

	Delivery         types.InstalmentDelivery `json:"delivery"`
	InstalmentStatus types.InstalmentStatus   `json:"instalment_status"`
	InvoiceID        *string                  `json:"invoice_id,omitempty"`

	Metadata types.Metadata `json:"metadata,omitempty" activity:"drop"`

	EnvironmentID string `json:"environment_id"`

	types.BaseModel
}

// InstalmentFromEnt converts an ent row to an Instalment; it returns nil for a nil row.
func InstalmentFromEnt(e *ent.BillingInstalment) *Instalment {
	if e == nil {
		return nil
	}

	return &Instalment{
		ID:                 e.ID,
		OwnerType:          e.OwnerType,
		OwnerID:            e.OwnerID,
		BillingTermID:      e.BillingTermID,
		CustomerID:         e.CustomerID,
		SubscriptionID:     e.SubscriptionID,
		Currency:           e.Currency,
		Kind:               e.Kind,
		BillTrigger:        e.BillTrigger,
		BillOn:             e.BillOn,
		TriggeredAt:        e.TriggeredAt,
		Amount:             e.Amount,
		InvoiceLabel:       e.InvoiceLabel,
		ServicePeriodStart: e.ServicePeriodStart,
		ServicePeriodEnd:   e.ServicePeriodEnd,
		Delivery:           e.Delivery,
		InstalmentStatus:   e.InstalmentStatus,
		InvoiceID:          e.InvoiceID,
		Metadata:           e.Metadata,
		EnvironmentID:      e.EnvironmentID,
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

// InstalmentsFromEnt converts ent rows to Instalments.
func InstalmentsFromEnt(list []*ent.BillingInstalment) []*Instalment {
	out := make([]*Instalment, 0, len(list))
	for _, e := range list {
		out = append(out, InstalmentFromEnt(e))
	}

	return out
}

type instalmentBuilder struct {
	i *Instalment
}

// NewInstalmentBuilder starts from a copy of i (or an empty instalment when i is nil).
func NewInstalmentBuilder(i *Instalment) *instalmentBuilder {
	if i == nil {
		return &instalmentBuilder{i: &Instalment{}}
	}

	cp := *i
	cp.Metadata = lo.Assign(types.Metadata{}, i.Metadata)

	return &instalmentBuilder{i: &cp}
}

// WithBillOn sets when the instalment becomes billable.
func (b *instalmentBuilder) WithBillOn(t time.Time) *instalmentBuilder {
	b.i.BillOn = t
	return b
}

// WithTriggeredAt sets when a manual instalment was triggered.
func (b *instalmentBuilder) WithTriggeredAt(t time.Time) *instalmentBuilder {
	b.i.TriggeredAt = &t
	return b
}

// Build returns the built instalment.
func (b *instalmentBuilder) Build() *Instalment {
	return b.i
}
