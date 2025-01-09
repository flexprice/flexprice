package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryInvoiceStore implements invoice.Repository for testing
type InMemoryInvoiceStore struct {
	sync.RWMutex
	invoices map[string]*invoice.Invoice
}

func NewInMemoryInvoiceStore() invoice.Repository {
	return &InMemoryInvoiceStore{
		invoices: make(map[string]*invoice.Invoice),
	}
}

// Helper to copy invoice
func copyInvoice(inv *invoice.Invoice) *invoice.Invoice {
	if inv == nil {
		return nil
	}

	// Convert to ent model and back to get a deep copy
	entInv := &ent.Invoice{
		ID:              inv.ID,
		CustomerID:      inv.CustomerID,
		SubscriptionID:  inv.SubscriptionID,
		InvoiceType:     string(inv.InvoiceType),
		InvoiceStatus:   string(inv.InvoiceStatus),
		PaymentStatus:   string(inv.PaymentStatus),
		Currency:        inv.Currency,
		AmountDue:       inv.AmountDue,
		AmountPaid:      inv.AmountPaid,
		AmountRemaining: inv.AmountRemaining,
		Description:     inv.Description,
		DueDate:         inv.DueDate,
		PaidAt:          inv.PaidAt,
		VoidedAt:        inv.VoidedAt,
		FinalizedAt:     inv.FinalizedAt,
		PeriodStart:     inv.PeriodStart,
		PeriodEnd:       inv.PeriodEnd,
		InvoicePdfURL:   inv.InvoicePDFURL,
		BillingReason:   inv.BillingReason,
		Metadata:        inv.Metadata,
		Version:         inv.Version,
		Status:          string(inv.Status),
		TenantID:        inv.TenantID,
		CreatedAt:       inv.CreatedAt,
		CreatedBy:       inv.CreatedBy,
		UpdatedAt:       inv.UpdatedAt,
		UpdatedBy:       inv.UpdatedBy,
	}

	// Copy line items
	if len(inv.LineItems) > 0 {
		entInv.Edges.LineItems = make([]*ent.InvoiceLineItem, len(inv.LineItems))
		for i, item := range inv.LineItems {
			entInv.Edges.LineItems[i] = &ent.InvoiceLineItem{
				ID:             item.ID,
				InvoiceID:      item.InvoiceID,
				CustomerID:     item.CustomerID,
				SubscriptionID: item.SubscriptionID,
				PriceID:        item.PriceID,
				MeterID:        item.MeterID,
				Amount:         item.Amount,
				Quantity:       item.Quantity,
				Currency:       item.Currency,
				PeriodStart:    item.PeriodStart,
				PeriodEnd:      item.PeriodEnd,
				Status:         string(item.Status),
				TenantID:       item.TenantID,
				CreatedAt:      item.CreatedAt,
				CreatedBy:      item.CreatedBy,
				UpdatedAt:      item.UpdatedAt,
				UpdatedBy:      item.UpdatedBy,
			}
		}
	}

	return invoice.FromEnt(entInv)
}

func (s *InMemoryInvoiceStore) Create(ctx context.Context, inv *invoice.Invoice) error {
	s.Lock()
	defer s.Unlock()

	if inv.ID == "" {
		return fmt.Errorf("invoice ID cannot be empty")
	}

	if _, exists := s.invoices[inv.ID]; exists {
		return fmt.Errorf("invoice already exists")
	}

	s.invoices[inv.ID] = copyInvoice(inv)
	return nil
}

func (s *InMemoryInvoiceStore) CreateWithLineItems(ctx context.Context, inv *invoice.Invoice) error {
	return s.Create(ctx, inv)
}

func (s *InMemoryInvoiceStore) AddLineItems(ctx context.Context, invoiceID string, items []*invoice.InvoiceLineItem) error {
	s.Lock()
	defer s.Unlock()

	inv, exists := s.invoices[invoiceID]
	if !exists {
		return fmt.Errorf("invoice not found")
	}

	// Copy and add each line item
	for _, item := range items {
		itemCopy := copyInvoice(&invoice.Invoice{LineItems: []*invoice.InvoiceLineItem{item}}).LineItems[0]
		itemCopy.InvoiceID = invoiceID
		inv.LineItems = append(inv.LineItems, itemCopy)
	}
	return nil
}

func (s *InMemoryInvoiceStore) RemoveLineItems(ctx context.Context, invoiceID string, itemIDs []string) error {
	s.Lock()
	defer s.Unlock()

	inv, exists := s.invoices[invoiceID]
	if !exists {
		return fmt.Errorf("invoice not found")
	}

	// Convert to map for O(1) lookup
	toRemove := make(map[string]bool)
	for _, id := range itemIDs {
		toRemove[id] = true
	}

	// Filter out removed items
	remaining := make([]*invoice.InvoiceLineItem, 0)
	for _, item := range inv.LineItems {
		if !toRemove[item.ID] {
			remaining = append(remaining, item)
		} else {
			// Soft delete
			item.Status = types.StatusDeleted
			item.UpdatedAt = time.Now()
			item.UpdatedBy = types.GetUserID(ctx)
			remaining = append(remaining, item)
		}
	}
	inv.LineItems = remaining
	return nil
}

func (s *InMemoryInvoiceStore) Get(ctx context.Context, id string) (*invoice.Invoice, error) {
	s.RLock()
	defer s.RUnlock()

	inv, exists := s.invoices[id]
	if !exists {
		return nil, invoice.ErrInvoiceNotFound
	}

	if inv.Status == types.StatusDeleted {
		return nil, invoice.ErrInvoiceNotFound
	}

	return copyInvoice(inv), nil
}

func (s *InMemoryInvoiceStore) Update(ctx context.Context, inv *invoice.Invoice) error {
	s.Lock()
	defer s.Unlock()

	existing, exists := s.invoices[inv.ID]
	if !exists {
		return invoice.ErrInvoiceNotFound
	}

	if existing.Version != inv.Version {
		return invoice.NewVersionConflictError(inv.ID, inv.Version, existing.Version)
	}

	inv.Version++
	s.invoices[inv.ID] = copyInvoice(inv)
	return nil
}

func (s *InMemoryInvoiceStore) Delete(ctx context.Context, id string) error {
	s.Lock()
	defer s.Unlock()

	inv, exists := s.invoices[id]
	if !exists {
		return invoice.ErrInvoiceNotFound
	}

	// Soft delete
	inv.Status = types.StatusDeleted
	inv.UpdatedAt = time.Now()
	inv.UpdatedBy = types.GetUserID(ctx)

	// Soft delete line items
	for _, item := range inv.LineItems {
		item.Status = types.StatusDeleted
		item.UpdatedAt = time.Now()
		item.UpdatedBy = types.GetUserID(ctx)
	}

	return nil
}

func (s *InMemoryInvoiceStore) List(ctx context.Context, filter *types.InvoiceFilter) ([]*invoice.Invoice, error) {
	s.RLock()
	defer s.RUnlock()

	result := make([]*invoice.Invoice, 0)
	for _, inv := range s.invoices {
		if s.matchesFilter(ctx, inv, filter) {
			result = append(result, copyInvoice(inv))
		}
	}

	// Sort by created_at desc
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Apply pagination
	if filter != nil && filter.Limit > 0 {
		start := filter.Offset
		if start > len(result) {
			start = len(result)
		}
		end := start + filter.Limit
		if end > len(result) {
			end = len(result)
		}
		result = result[start:end]
	}

	return result, nil
}

func (s *InMemoryInvoiceStore) Count(ctx context.Context, filter *types.InvoiceFilter) (int, error) {
	s.RLock()
	defer s.RUnlock()

	count := 0
	for _, inv := range s.invoices {
		if s.matchesFilter(ctx, inv, filter) {
			count++
		}
	}
	return count, nil
}

func (s *InMemoryInvoiceStore) GetByIdempotencyKey(ctx context.Context, key string) (*invoice.Invoice, error) {
	s.RLock()
	defer s.RUnlock()

	for _, inv := range s.invoices {
		if *inv.IdempotencyKey == key {
			return copyInvoice(inv), nil
		}
	}
	return nil, invoice.ErrInvoiceNotFound
}

func (s *InMemoryInvoiceStore) ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	s.RLock()
	defer s.RUnlock()

	for _, inv := range s.invoices {
		if inv.SubscriptionID != nil && *inv.SubscriptionID == subscriptionID && inv.PeriodStart.Before(periodEnd) && inv.PeriodEnd.After(periodStart) {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryInvoiceStore) GetNextInvoiceNumber(ctx context.Context) (string, error) {
	return "INV-YYYYMM-XXXXX", nil
}

func (s *InMemoryInvoiceStore) GetNextBillingSequence(ctx context.Context, subscriptionID string) (int, error) {
	return 1, nil
}

func (s *InMemoryInvoiceStore) matchesFilter(ctx context.Context, inv *invoice.Invoice, filter *types.InvoiceFilter) bool {
	if inv.Status == types.StatusDeleted {
		return false
	}

	if inv.TenantID != types.GetTenantID(ctx) {
		return false
	}

	if filter == nil {
		return true
	}

	if filter.CustomerID != "" && inv.CustomerID != filter.CustomerID {
		return false
	}

	if filter.SubscriptionID != "" && inv.SubscriptionID != nil && *inv.SubscriptionID != filter.SubscriptionID {
		return false
	}

	if filter.InvoiceType != "" && inv.InvoiceType != filter.InvoiceType {
		return false
	}

	if len(filter.InvoiceStatus) > 0 {
		found := false
		for _, status := range filter.InvoiceStatus {
			if inv.InvoiceStatus == status {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(filter.PaymentStatus) > 0 {
		found := false
		for _, status := range filter.PaymentStatus {
			if inv.PaymentStatus == status {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if filter.StartTime != nil && inv.CreatedAt.Before(*filter.StartTime) {
		return false
	}

	if filter.EndTime != nil && inv.CreatedAt.After(*filter.EndTime) {
		return false
	}

	return true
}

func (s *InMemoryInvoiceStore) Clear() {
	s.Lock()
	defer s.Unlock()
	s.invoices = make(map[string]*invoice.Invoice)
}
