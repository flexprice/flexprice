package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCreditNoteLineItemStore implements the credit note line item repository interface for testing
type InMemoryCreditNoteLineItemStore struct {
	*InMemoryStore[*creditnote.CreditNoteLineItem]
}

// NewInMemoryCreditNoteLineItemStore creates a new in-memory credit note line item store
func NewInMemoryCreditNoteLineItemStore() *InMemoryCreditNoteLineItemStore {
	return &InMemoryCreditNoteLineItemStore{
		InMemoryStore: NewInMemoryStore[*creditnote.CreditNoteLineItem](),
	}
}

// Helper to copy credit note line item
func copyCreditNoteLineItem(item *creditnote.CreditNoteLineItem) *creditnote.CreditNoteLineItem {
	if item == nil {
		return nil
	}

	cloned := &creditnote.CreditNoteLineItem{
		ID:                item.ID,
		InvoiceLineItemID: item.InvoiceLineItemID,
		DisplayName:       item.DisplayName,
		Amount:            item.Amount,
		Metadata:          make(types.Metadata),
		CreditNoteID:      item.CreditNoteID,
		Currency:          item.Currency,
		EnvironmentID:     item.EnvironmentID,
		BaseModel:         item.BaseModel,
	}

	// Copy metadata
	for k, v := range item.Metadata {
		cloned.Metadata[k] = v
	}

	return cloned
}

func (s *InMemoryCreditNoteLineItemStore) Create(ctx context.Context, item *creditnote.CreditNoteLineItem) error {
	if item == nil {
		return ierr.NewError("credit note line item cannot be nil").
			WithHint("Credit note line item cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if item.EnvironmentID == "" {
		item.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, item.ID, copyCreditNoteLineItem(item))
}

func (s *InMemoryCreditNoteLineItemStore) Get(ctx context.Context, id string) (*creditnote.CreditNoteLineItem, error) {
	item, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return copyCreditNoteLineItem(item), nil
}

func (s *InMemoryCreditNoteLineItemStore) Update(ctx context.Context, item *creditnote.CreditNoteLineItem) error {
	if item == nil {
		return ierr.NewError("credit note line item cannot be nil").
			WithHint("Credit note line item cannot be nil").
			Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Update(ctx, item.ID, copyCreditNoteLineItem(item))
}

func (s *InMemoryCreditNoteLineItemStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryCreditNoteLineItemStore) List(ctx context.Context, filter *types.CreditNoteLineItemFilter) ([]*creditnote.CreditNoteLineItem, error) {
	return s.InMemoryStore.List(ctx, filter, creditNoteLineItemFilterFn, creditNoteLineItemSortFn)
}

func (s *InMemoryCreditNoteLineItemStore) Count(ctx context.Context, filter *types.CreditNoteLineItemFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, creditNoteLineItemFilterFn)
}

func (s *InMemoryCreditNoteLineItemStore) ListByCreditNote(ctx context.Context, creditNoteID string) ([]*creditnote.CreditNoteLineItem, error) {
	filter := &types.CreditNoteLineItemFilter{
		CreditNoteIDs: []string{creditNoteID},
	}
	return s.List(ctx, filter)
}

func (s *InMemoryCreditNoteLineItemStore) ListByInvoiceLineItem(ctx context.Context, invoiceLineItemID string) ([]*creditnote.CreditNoteLineItem, error) {
	filter := &types.CreditNoteLineItemFilter{
		InvoiceLineItemIDs: []string{invoiceLineItemID},
	}
	return s.List(ctx, filter)
}

func (s *InMemoryCreditNoteLineItemStore) CreateBulk(ctx context.Context, lineItems []*creditnote.CreditNoteLineItem) error {
	for _, item := range lineItems {
		if err := s.Create(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// Filter and sort functions for credit note line items
func creditNoteLineItemFilterFn(ctx context.Context, item *creditnote.CreditNoteLineItem, filter interface{}) bool {
	if item == nil {
		return false
	}

	f, ok := filter.(*types.CreditNoteLineItemFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID := types.GetTenantID(ctx); tenantID != "" {
		if item.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, item.EnvironmentID) {
		return false
	}

	// Check if deleted
	if item.Status == types.StatusDeleted {
		return false
	}

	// Filter by credit note IDs
	if len(f.CreditNoteIDs) > 0 && !lo.Contains(f.CreditNoteIDs, item.CreditNoteID) {
		return false
	}

	// Filter by invoice line item IDs
	if len(f.InvoiceLineItemIDs) > 0 && !lo.Contains(f.InvoiceLineItemIDs, item.InvoiceLineItemID) {
		return false
	}

	return true
}

func creditNoteLineItemSortFn(i, j *creditnote.CreditNoteLineItem) bool {
	return i.CreatedAt.After(j.CreatedAt)
}
