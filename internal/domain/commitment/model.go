package commitment

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// Commitment is a subscription-level spend or quantity commitment.
type Commitment struct {
	ID                  string                            `json:"id"`
	SubscriptionID      string                            `json:"subscription_id"`
	CustomerID          string                            `json:"customer_id"`
	Name                string                            `json:"name"`
	InvoiceLabel        string                            `json:"invoice_label"`
	InvoiceDescription  *string                           `json:"invoice_description,omitempty"`
	Currency            string                            `json:"currency"`
	ScopeEntityType     types.CommitmentScopeType         `json:"scope_entity_type"`
	ScopeEntityID       string                            `json:"scope_entity_id"`
	Type                types.CommitmentType              `json:"type"`
	Value               decimal.Decimal                   `json:"value" swaggertype:"string"`
	CommittedPriceID    *string                           `json:"committed_price_id,omitempty"`
	OveragePriceID      *string                           `json:"overage_price_id,omitempty"`
	Measurement         types.CommitmentMeasurement       `json:"measurement"`
	ActiveWindows       types.ActiveWindows               `json:"active_windows,omitempty"`
	Timezone            string                            `json:"timezone"`
	Priority            int                               `json:"priority"`
	Binding             bool                              `json:"binding"`
	CollectionMethod    *types.CommitmentCollectionMethod `json:"collection_method,omitempty"`
	TotalAmount         *decimal.Decimal                  `json:"total_amount,omitempty" swaggertype:"string"`
	InstalmentInvoicing *string                           `json:"instalment_invoicing,omitempty"`
	StartDate           time.Time                         `json:"start_date"`
	EndDate             time.Time                         `json:"end_date"`
	OnEarlyTermination  *types.CommitmentEarlyTermination `json:"on_early_termination,omitempty"`
	CommitmentStatus    types.CommitmentStatus            `json:"commitment_status"`
	TerminatedAt        *time.Time                        `json:"terminated_at,omitempty"`
	IdempotencyKey      *string                           `json:"idempotency_key,omitempty"`
	Metadata            types.Metadata                    `json:"metadata,omitempty"`
	EnvironmentID       string                            `json:"environment_id"`
	types.BaseModel
}

// IsActiveAt reports whether t falls inside the term and the commitment has not ended early.
func (c *Commitment) IsActiveAt(t time.Time) bool {
	if c == nil || t.Before(c.StartDate) || !t.Before(c.EndDate) {
		return false
	}
	return c.TerminatedAt == nil || t.Before(*c.TerminatedAt)
}

// Location returns the commitment timezone, falling back to UTC.
func (c *Commitment) Location() *time.Location {
	if c == nil {
		return time.UTC
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func FromEnt(e *ent.Commitment) *Commitment {
	if e == nil {
		return nil
	}
	return &Commitment{
		ID: e.ID, SubscriptionID: e.SubscriptionID, CustomerID: e.CustomerID, Name: e.Name,
		InvoiceLabel: e.InvoiceLabel, InvoiceDescription: e.InvoiceDescription, Currency: e.Currency,
		ScopeEntityType: e.ScopeEntityType, ScopeEntityID: e.ScopeEntityID, Type: e.Type, Value: e.Value,
		CommittedPriceID: e.CommittedPriceID, OveragePriceID: e.OveragePriceID, Measurement: e.Measurement,
		ActiveWindows: e.ActiveWindows, Timezone: e.Timezone, Priority: e.Priority, Binding: e.Binding,
		CollectionMethod: e.CollectionMethod, TotalAmount: e.TotalAmount, InstalmentInvoicing: e.InstalmentInvoicing,
		StartDate: e.StartDate, EndDate: e.EndDate, OnEarlyTermination: e.OnEarlyTermination,
		CommitmentStatus: e.CommitmentStatus, TerminatedAt: e.TerminatedAt, IdempotencyKey: e.IdempotencyKey,
		Metadata: e.Metadata, EnvironmentID: e.EnvironmentID,
		BaseModel: types.BaseModel{TenantID: e.TenantID, Status: types.Status(e.Status), CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt, CreatedBy: e.CreatedBy, UpdatedBy: e.UpdatedBy},
	}
}

func FromEntList(list []*ent.Commitment) []*Commitment {
	out := make([]*Commitment, 0, len(list))
	for _, e := range list {
		out = append(out, FromEnt(e))
	}
	return out
}
