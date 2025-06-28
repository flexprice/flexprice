package creditnote

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CreditNoteLineItem is the model entity for the CreditNoteLineItem schema.
type CreditNoteLineItem struct {
	ID                string          `json:"id"`
	CreditNoteID      string          `json:"credit_note_id"`
	InvoiceLineItemID string          `json:"invoice_line_item_id"`
	DisplayName       string          `json:"display_name"`
	Amount            decimal.Decimal `json:"amount"`
	Currency          string          `json:"currency"`
	Metadata          types.Metadata  `json:"metadata"`
	EnvironmentID     string          `json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an ent.CreditNoteLineItem to domain CreditNoteLineItem
func (c *CreditNoteLineItem) FromEnt(e *ent.CreditNoteLineItem) {
	c.ID = e.ID
	c.CreditNoteID = e.CreditNoteID
	c.InvoiceLineItemID = e.InvoiceLineItemID
	c.DisplayName = e.DisplayName
	c.Amount = e.Amount
	c.Currency = e.Currency
	c.Metadata = e.Metadata
	c.EnvironmentID = e.EnvironmentID
	c.BaseModel = types.BaseModel{
		Status:    types.Status(e.Status),
		CreatedBy: e.CreatedBy,
		TenantID:  e.TenantID,
		UpdatedBy: e.UpdatedBy,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}
}

func (c *CreditNoteLineItem) FromEntList(creditNoteLineItems []*ent.CreditNoteLineItem) []*CreditNoteLineItem {
	result := make([]*CreditNoteLineItem, len(creditNoteLineItems))
	for i, cnli := range creditNoteLineItems {
		result[i] = &CreditNoteLineItem{}
		result[i].FromEnt(cnli)
	}
	return result
}
