package journal

import (
	"github.com/flexprice/flexprice/ent"
	domainJournal "github.com/flexprice/flexprice/internal/domain/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// FromEnt maps a journal_entries row and its lines to the domain entry.
func FromEnt(e *ent.JournalEntry, lines []*ent.JournalLine) *domainJournal.Entry {
	if e == nil {
		return nil
	}
	return domainJournal.NewBuilder(nil).
		WithID(e.ID).
		WithEventType(types.JournalEventType(e.EventType)).
		WithSource(types.RevRecEntityType(e.SourceEntityType), e.SourceEntityID).
		WithIdempotencyKey(lo.FromPtr(e.IdempotencyKey)).
		WithAccountingPeriodID(e.AccountingPeriodID).
		WithBusinessDate(e.BusinessDate).
		WithCustomerID(e.CustomerID).
		WithContractID(lo.FromPtr(e.ContractID)).
		WithCurrency(e.Currency).
		WithReversalOf(lo.FromPtr(e.ReversalOf)).
		WithLines(lo.Map(lines, func(l *ent.JournalLine, _ int) *domainJournal.Line { return LineFromEnt(l) })).
		WithCreatedAt(e.CreatedAt).
		Build()
}

func LineFromEnt(l *ent.JournalLine) *domainJournal.Line {
	if l == nil {
		return nil
	}
	return domainJournal.NewLineBuilder(nil).
		WithID(l.ID).
		WithAccount(types.LedgerAccountKey(l.Account)).
		WithDirection(types.JournalDirection(l.Direction)).
		WithAmount(l.Amount).
		WithPOBID(lo.FromPtr(l.PobID)).
		Build()
}
