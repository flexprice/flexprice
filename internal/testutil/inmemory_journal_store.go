package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/journal"
	ierr "github.com/flexprice/flexprice/internal/errors"
	journalRepo "github.com/flexprice/flexprice/internal/repository/journal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryJournalStore implements journalRepo.Repository.
type InMemoryJournalStore struct {
	*InMemoryStore[*journal.Entry]
	// keys mirrors the idempotency key unique index.
	keys *InMemoryStore[string]
}

func NewInMemoryJournalStore() *InMemoryJournalStore {
	return &InMemoryJournalStore{
		InMemoryStore: NewInMemoryStore[*journal.Entry](),
		keys:          NewInMemoryStore[string](),
	}
}

func (s *InMemoryJournalStore) CreateWithLines(ctx context.Context, e *journal.Entry) error {
	if err := journal.ValidateEntry(e.Lines()); err != nil {
		return err
	}
	if e.IdempotencyKey() != "" {
		if err := s.keys.Create(ctx, s.key(ctx, e.IdempotencyKey()), e.ID()); err != nil {
			return err
		}
	}
	return s.InMemoryStore.Create(ctx, e.ID(), journal.NewBuilder(e).Build())
}

func (s *InMemoryJournalStore) GetIDByIdempotencyKey(ctx context.Context, key string) (string, error) {
	return s.keys.Get(ctx, s.key(ctx, key))
}

func (s *InMemoryJournalStore) key(ctx context.Context, idempotencyKey string) string {
	return types.GetTenantID(ctx) + ":" + types.GetEnvironmentID(ctx) + ":" + idempotencyKey
}

func (s *InMemoryJournalStore) Get(ctx context.Context, id string) (*journal.Entry, error) {
	e, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("journal entry not found").
			WithHintf("Journal entry with ID %s was not found", id).
			Mark(ierr.ErrNotFound)
	}
	return journal.NewBuilder(e).Build(), nil
}

func (s *InMemoryJournalStore) List(ctx context.Context, filter *journalRepo.Filter) ([]*journal.Entry, error) {
	if filter == nil {
		filter = journalRepo.NewFilter()
	}
	return s.InMemoryStore.List(ctx, filter, journalFilterFn, nil)
}

func (s *InMemoryJournalStore) Count(ctx context.Context, filter *journalRepo.Filter) (int, error) {
	if filter == nil {
		filter = journalRepo.NewFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, journalFilterFn)
}

func journalFilterFn(_ context.Context, e *journal.Entry, filter interface{}) bool {
	f, ok := filter.(*journalRepo.Filter)
	if !ok {
		return true
	}
	matches := func(values []string, value string) bool { return len(values) == 0 || lo.Contains(values, value) }
	if !matches(f.JournalEntryIDs, e.ID()) || !matches(f.AccountingPeriodIDs, e.AccountingPeriodID()) ||
		!matches(f.CustomerIDs, e.CustomerID()) || !matches(f.ContractIDs, e.ContractID()) ||
		!matches(f.SourceEntityIDs, e.SourceEntityID()) {
		return false
	}
	if len(f.EventTypes) > 0 && !lo.Contains(f.EventTypes, e.EventType()) {
		return false
	}
	if f.SourceEntityType != "" && e.SourceEntityType() != f.SourceEntityType {
		return false
	}
	if (len(f.Accounts) > 0 || len(f.POBIDs) > 0) && !lo.ContainsBy(e.Lines(), func(l *journal.Line) bool {
		return (len(f.Accounts) == 0 || lo.Contains(f.Accounts, l.Account())) && matches(f.POBIDs, l.POBID())
	}) {
		return false
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && e.BusinessDate().Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && !e.BusinessDate().Before(*f.EndTime) {
			return false
		}
	}
	return true
}
