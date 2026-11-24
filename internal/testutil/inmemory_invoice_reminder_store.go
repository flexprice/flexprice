package testutil

import (
	"context"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryInvoiceReminderStore implements collections.InvoiceReminderRepository over the in-memory invoice store
// and counts the writes it receives.
type InMemoryInvoiceReminderStore struct {
	invoices  *InMemoryInvoiceStore
	mu        sync.Mutex
	reminders map[string]*collections.InvoiceReminder
	writes    int
}

func NewInMemoryInvoiceReminderStore(invoices *InMemoryInvoiceStore) *InMemoryInvoiceReminderStore {
	return &InMemoryInvoiceReminderStore{invoices: invoices, reminders: map[string]*collections.InvoiceReminder{}}
}

// Writes is the number of Create, CreateMany and Update calls so far.
func (s *InMemoryInvoiceReminderStore) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// All returns the live reminders of the environment in ctx.
func (s *InMemoryInvoiceReminderStore) All(ctx context.Context) []*collections.InvoiceReminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*collections.InvoiceReminder
	for _, r := range s.reminders {
		if s.inScope(ctx, r) {
			out = append(out, cloneReminder(r))
		}
	}
	return out
}

func (s *InMemoryInvoiceReminderStore) inScope(ctx context.Context, r *collections.InvoiceReminder) bool {
	return r.BaseModel.Status == types.StatusPublished && r.TenantID == types.GetTenantID(ctx) && r.EnvironmentID == types.GetEnvironmentID(ctx)
}

func (s *InMemoryInvoiceReminderStore) liveFor(ctx context.Context, invoiceID string) *collections.InvoiceReminder {
	for _, r := range s.reminders {
		if s.inScope(ctx, r) && r.InvoiceID == invoiceID {
			return r
		}
	}
	return nil
}

func (s *InMemoryInvoiceReminderStore) GetInvoicesWithReminders(ctx context.Context, asOf time.Time, invoiceIDs []string) ([]collections.InvoiceWithReminder, error) {
	if len(invoiceIDs) == 0 {
		return nil, nil
	}
	queryFilter := types.NewNoLimitQueryFilter()
	queryFilter.Status = lo.ToPtr(types.StatusPublished)
	invoices, err := s.invoices.List(ctx, &types.InvoiceFilter{
		QueryFilter: queryFilter, InvoiceIDs: invoiceIDs, InvoiceStatus: []types.InvoiceStatus{types.InvoiceStatusFinalized},
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var rows []collections.InvoiceWithReminder
	for _, inv := range invoices {
		if inv.PaymentStatus == types.PaymentStatusSucceeded || !inv.AmountRemaining.IsPositive() || !types.IsOverdue(inv.DueDate, asOf) {
			continue
		}
		row := collections.NewInvoiceWithReminderBuilder(&collections.InvoiceWithReminder{InvoiceID: inv.ID}).
			WithInvoice(lo.FromPtr(inv.InvoiceNumber), inv.CustomerID, strings.ToLower(inv.Currency)).
			WithAmounts(inv.AmountDue, inv.AmountRemaining).
			WithDates(inv.DueDate, inv.PaidAt)
		if r := s.liveFor(ctx, inv.ID); r != nil {
			row = row.WithReminder(cloneReminder(r))
		}
		rows = append(rows, *row.Build())
	}
	return rows, nil
}

func (s *InMemoryInvoiceReminderStore) Create(ctx context.Context, r *collections.InvoiceReminder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.liveFor(ctx, r.InvoiceID) != nil {
		return ierr.NewError("reminder already exists").Mark(ierr.ErrAlreadyExists)
	}
	r.TenantID, r.EnvironmentID, r.BaseModel.Status = types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.StatusPublished
	s.reminders[r.ID] = cloneReminder(r)
	return nil
}

func (s *InMemoryInvoiceReminderStore) CreateMany(ctx context.Context, reminders []*collections.InvoiceReminder) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	var created []string
	for _, r := range reminders {
		if s.liveFor(ctx, r.InvoiceID) != nil {
			continue
		}
		r.TenantID, r.EnvironmentID, r.BaseModel.Status = types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.StatusPublished
		s.reminders[r.ID] = cloneReminder(r)
		created = append(created, r.InvoiceID)
	}
	return created, nil
}

func (s *InMemoryInvoiceReminderStore) Update(ctx context.Context, r *collections.InvoiceReminder, previous *collections.InvoiceReminder) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	cur, ok := s.reminders[r.ID]
	if !ok || !s.inScope(ctx, cur) || cur.ReminderStatus != previous.ReminderStatus || !cur.UpdatedAt.Equal(previous.UpdatedAt) {
		return ierr.NewError("reminder changed").Mark(ierr.ErrVersionConflict)
	}
	cp := cloneReminder(r)
	cp.TenantID, cp.EnvironmentID, cp.BaseModel.Status, cp.UpdatedAt = cur.TenantID, cur.EnvironmentID, cur.BaseModel.Status, clock.Now()
	s.reminders[r.ID] = cp
	*r = *cloneReminder(cp)
	return nil
}

// cloneReminder copies a reminder with its own ApprovedAt and Metadata, so a stored row never shares memory with a caller.
func cloneReminder(r *collections.InvoiceReminder) *collections.InvoiceReminder {
	cp := *r
	if r.ApprovedAt != nil {
		cp.ApprovedAt = lo.ToPtr(*r.ApprovedAt)
	}
	if r.Metadata != nil {
		cp.Metadata = maps.Clone(r.Metadata)
	}
	return &cp
}
