package testutil

import (
	"context"
	"sync"
	"time"

	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
)

// InMemorySelfBillingStore fakes both self-billing read repositories.
type InMemorySelfBillingStore struct {
	mu       sync.Mutex
	counts   []*domainsb.TenantEnvHourCount
	invoices []*domainsb.FinalizedInvoice
}

func NewInMemorySelfBillingStore() *InMemorySelfBillingStore { return &InMemorySelfBillingStore{} }

func (s *InMemorySelfBillingStore) AddCount(c *domainsb.TenantEnvHourCount) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts = append(s.counts, c)
}

func (s *InMemorySelfBillingStore) AddInvoice(i *domainsb.FinalizedInvoice) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoices = append(s.invoices, i)
}

func (s *InMemorySelfBillingStore) CountByTenantHour(_ context.Context, from, to time.Time) ([]*domainsb.TenantEnvHourCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*domainsb.TenantEnvHourCount
	for _, c := range s.counts {
		if !c.Hour.Before(from) && c.Hour.Before(to) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *InMemorySelfBillingStore) ListFinalized(_ context.Context, from, to time.Time) ([]*domainsb.FinalizedInvoice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*domainsb.FinalizedInvoice
	for _, i := range s.invoices {
		if !i.FinalizedAt.Before(from) && i.FinalizedAt.Before(to) {
			out = append(out, i)
		}
	}
	return out, nil
}
