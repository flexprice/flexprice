package testutil

import (
	"context"
	"sort"
	"sync"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryReminderDecisionStore implements collections.ReminderDecisionRepository and counts the writes it receives.
type InMemoryReminderDecisionStore struct {
	mu        sync.Mutex
	decisions map[string]*collections.ReminderDecision
	writes    int
}

func NewInMemoryReminderDecisionStore() *InMemoryReminderDecisionStore {
	return &InMemoryReminderDecisionStore{decisions: map[string]*collections.ReminderDecision{}}
}

// Writes is the number of Create, Update and Delete calls so far.
func (s *InMemoryReminderDecisionStore) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

func (s *InMemoryReminderDecisionStore) inScope(ctx context.Context, d *collections.ReminderDecision) bool {
	return d.BaseModel.Status == types.StatusPublished &&
		d.TenantID == types.GetTenantID(ctx) && d.EnvironmentID == types.GetEnvironmentID(ctx)
}

func copyReminderDecision(d *collections.ReminderDecision) *collections.ReminderDecision {
	cp := *d
	cp.InvoiceIDs = append([]string(nil), d.InvoiceIDs...)
	return &cp
}

func (s *InMemoryReminderDecisionStore) Create(ctx context.Context, d *collections.ReminderDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if d.EnvironmentID == "" {
		d.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	d.BaseModel.Status = types.StatusPublished
	for _, existing := range s.decisions {
		if s.inScope(ctx, existing) && existing.CustomerID == d.CustomerID && existing.Currency == d.Currency {
			return ierr.NewError("reminder decision already exists").Mark(ierr.ErrAlreadyExists)
		}
	}
	d.Version = 1
	s.decisions[d.ID] = copyReminderDecision(d)
	return nil
}

// ListByKeys returns the decisions of the named groups.
func (s *InMemoryReminderDecisionStore) ListByKeys(ctx context.Context, keys []collections.GroupKey) ([]*collections.ReminderDecision, error) {
	all, err := s.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return lo.Filter(all, func(d *collections.ReminderDecision, _ int) bool {
		return lo.Contains(keys, collections.GroupKey{CustomerID: d.CustomerID, Currency: d.Currency})
	}), nil
}

// ListAll returns every decision of the environment; tests use it to inspect what was stored.
func (s *InMemoryReminderDecisionStore) ListAll(ctx context.Context) ([]*collections.ReminderDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*collections.ReminderDecision
	for _, d := range s.decisions {
		if s.inScope(ctx, d) {
			out = append(out, copyReminderDecision(d))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *InMemoryReminderDecisionStore) current(ctx context.Context, d *collections.ReminderDecision) (*collections.ReminderDecision, error) {
	existing, ok := s.decisions[d.ID]
	if !ok || !s.inScope(ctx, existing) || existing.Version != d.Version {
		return nil, ierr.NewError("reminder decision changed").Mark(ierr.ErrVersionConflict)
	}
	return existing, nil
}

func (s *InMemoryReminderDecisionStore) Update(ctx context.Context, d *collections.ReminderDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	existing, err := s.current(ctx, d)
	if err != nil {
		return err
	}
	next := copyReminderDecision(d)
	next.TenantID, next.EnvironmentID, next.BaseModel.Status = existing.TenantID, existing.EnvironmentID, existing.BaseModel.Status
	next.Version = existing.Version + 1
	s.decisions[d.ID] = next
	*d = *copyReminderDecision(next)
	return nil
}

func (s *InMemoryReminderDecisionStore) Delete(ctx context.Context, d *collections.ReminderDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	existing, err := s.current(ctx, d)
	if err != nil {
		return err
	}
	existing.BaseModel.Status = types.StatusDeleted
	existing.Version++
	return nil
}
