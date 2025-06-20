package creditnote

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditNoteLineItem is the model entity for the CreditNoteLineItem schema.
type CreditNoteLineItem struct {
	ID                string            `json:"id"`
	TenantID          string            `json:"tenant_id"`
	EnvironmentID     string            `json:"environment_id"`
	CreditNoteID      string            `json:"credit_note_id"`
	InvoiceLineItemID string            `json:"invoice_line_item_id"`
	DisplayName       string            `json:"display_name"`
	Amount            decimal.Decimal   `json:"amount"`
	Quantity          decimal.Decimal   `json:"quantity"`
	Currency          string            `json:"currency"`
	Metadata          map[string]string `json:"metadata"`
	types.BaseModel
}

// FromEnt converts an ent.CreditNoteLineItem to domain CreditNoteLineItem
func (c *CreditNoteLineItem) FromEnt(e *ent.CreditNoteLineItem) *CreditNoteLineItem {
	return &CreditNoteLineItem{
		ID:                e.ID,
		TenantID:          e.TenantID,
		EnvironmentID:     e.EnvironmentID,
		CreditNoteID:      e.CreditNoteID,
		InvoiceLineItemID: e.InvoiceLineItemID,
		DisplayName:       e.DisplayName,
		Amount:            e.Amount,
		Quantity:          e.Quantity,
		Currency:          e.Currency,
		Metadata:          e.Metadata,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// FromEntList converts a list of ent credit notes to domain credit notes
func (c *CreditNoteLineItem) FromEntList(creditNoteLineItems []*ent.CreditNoteLineItem) []*CreditNoteLineItem {
	result := make([]*CreditNoteLineItem, len(creditNoteLineItems))
	for i, cnli := range creditNoteLineItems {
		result[i] = c.FromEnt(cnli)
	}
	return result
}
