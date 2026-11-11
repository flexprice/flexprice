package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type JournalEntryResponse struct {
	ID                 string                 `json:"id"`
	EventType          types.JournalEventType `json:"event_type"`
	SourceEntityType   types.RevRecEntityType `json:"source_entity_type"`
	SourceEntityID     string                 `json:"source_entity_id"`
	AccountingPeriodID string                 `json:"accounting_period_id"`
	BusinessDate       time.Time              `json:"business_date"`
	CustomerID         string                 `json:"customer_id"`
	ContractID         string                 `json:"contract_id,omitempty"`
	Currency           string                 `json:"currency"`
	ReversalOf         string                 `json:"reversal_of,omitempty"`
	Lines              []*JournalLineResponse `json:"lines"`
	CreatedAt          time.Time              `json:"created_at"`
}

type JournalLineResponse struct {
	ID        string                 `json:"id"`
	Account   types.LedgerAccountKey `json:"account"`
	Direction types.JournalDirection `json:"direction"`
	Amount    decimal.Decimal        `json:"amount" swaggertype:"string"`
	POBID     string                 `json:"pob_id,omitempty"`
}

func NewJournalEntryResponse(e *journal.Entry) *JournalEntryResponse {
	if e == nil {
		return nil
	}
	return &JournalEntryResponse{
		ID:                 e.ID(),
		EventType:          e.EventType(),
		SourceEntityType:   e.SourceEntityType(),
		SourceEntityID:     e.SourceEntityID(),
		AccountingPeriodID: e.AccountingPeriodID(),
		BusinessDate:       e.BusinessDate(),
		CustomerID:         e.CustomerID(),
		ContractID:         e.ContractID(),
		Currency:           e.Currency(),
		ReversalOf:         e.ReversalOf(),
		Lines: lo.Map(e.Lines(), func(l *journal.Line, _ int) *JournalLineResponse {
			return &JournalLineResponse{ID: l.ID(), Account: l.Account(), Direction: l.Direction(), Amount: l.Amount(), POBID: l.POBID()}
		}),
		CreatedAt: e.CreatedAt(),
	}
}

type ListJournalEntriesResponse = types.ListResponse[*JournalEntryResponse] // @name ListJournalEntriesResponse
