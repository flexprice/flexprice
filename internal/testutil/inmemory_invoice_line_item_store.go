package testutil

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryInvoiceLineItemStore implements invoice.LineItemRepository for testing.
type InMemoryInvoiceLineItemStore struct {
	mu   sync.RWMutex
	data map[string]*invoice.InvoiceLineItem
}

func NewInMemoryInvoiceLineItemStore() *InMemoryInvoiceLineItemStore {
	return &InMemoryInvoiceLineItemStore{data: make(map[string]*invoice.InvoiceLineItem)}
}

func (s *InMemoryInvoiceLineItemStore) Create(ctx context.Context, item *invoice.InvoiceLineItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[item.ID]; exists {
		return ierr.NewError("invoice line item already exists").Mark(ierr.ErrAlreadyExists)
	}
	cp := *item
	s.data[item.ID] = &cp
	return nil
}

func (s *InMemoryInvoiceLineItemStore) CreateBulk(ctx context.Context, items []*invoice.InvoiceLineItem) error {
	for _, item := range items {
		if err := s.Create(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryInvoiceLineItemStore) Get(ctx context.Context, id string) (*invoice.InvoiceLineItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.data[id]
	if !ok {
		return nil, ierr.NewError("invoice line item not found").Mark(ierr.ErrNotFound)
	}
	cp := *item
	return &cp, nil
}

func (s *InMemoryInvoiceLineItemStore) Update(ctx context.Context, item *invoice.InvoiceLineItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.data[item.ID]; !exists {
		return ierr.NewError("invoice line item not found").Mark(ierr.ErrNotFound)
	}
	cp := *item
	s.data[item.ID] = &cp
	return nil
}

func (s *InMemoryInvoiceLineItemStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, exists := s.data[id]
	if !exists {
		return ierr.NewError("invoice line item not found").Mark(ierr.ErrNotFound)
	}
	cp := *item
	cp.Status = types.StatusDeleted
	s.data[id] = &cp
	return nil
}

func (s *InMemoryInvoiceLineItemStore) ListByInvoiceID(ctx context.Context, invoiceID string) ([]*invoice.InvoiceLineItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*invoice.InvoiceLineItem
	for _, item := range s.data {
		if item.InvoiceID == invoiceID && item.Status == types.StatusPublished {
			cp := *item
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (s *InMemoryInvoiceLineItemStore) List(ctx context.Context, filter *types.InvoiceLineItemFilter) ([]*invoice.InvoiceLineItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*invoice.InvoiceLineItem
	for _, item := range s.data {
		if filter != nil && len(filter.InvoiceIDs) > 0 {
			found := false
			for _, id := range filter.InvoiceIDs {
				if item.InvoiceID == id {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if filter != nil && len(filter.SubscriptionIDs) > 0 {
			// nil SubscriptionID never matches a subscription ID filter
			if item.SubscriptionID == nil {
				continue
			}
			found := false
			for _, id := range filter.SubscriptionIDs {
				if *item.SubscriptionID == id {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *InMemoryInvoiceLineItemStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]*invoice.InvoiceLineItem)
}
