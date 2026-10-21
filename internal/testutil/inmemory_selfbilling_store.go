package testutil

import (
	"context"
	"sync"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
)

// InMemorySelfBillingStore fakes both self-billing repositories with pre-aggregated hourly rows.
type InMemorySelfBillingStore struct {
	mu       sync.Mutex
	events   []*domainsb.TenantHourEvents
	invoices []*domainsb.TenantHourInvoices
}

func NewInMemorySelfBillingStore() *InMemorySelfBillingStore {
	return &InMemorySelfBillingStore{}
}

func (s *InMemorySelfBillingStore) AddEvents(row *domainsb.TenantHourEvents) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, row)
}

func (s *InMemorySelfBillingStore) AddInvoices(row *domainsb.TenantHourInvoices) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.invoices = append(s.invoices, row)
}

func (s *InMemorySelfBillingStore) CountEventsByTenantHour(_ context.Context, from, to time.Time) ([]*domainsb.TenantHourEvents, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*domainsb.TenantHourEvents
	for _, row := range s.events {
		if !row.Hour.Before(from) && row.Hour.Before(to) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *InMemorySelfBillingStore) SumInvoicesByTenantHour(_ context.Context, from, to time.Time) ([]*domainsb.TenantHourInvoices, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*domainsb.TenantHourInvoices
	for _, row := range s.invoices {
		if !row.Hour.Before(from) && row.Hour.Before(to) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *InMemorySelfBillingStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = nil
	s.invoices = nil
}
