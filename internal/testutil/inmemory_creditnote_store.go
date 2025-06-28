package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCreditNoteStore implements the creditnote.Repository interface for testing
type InMemoryCreditNoteStore struct {
	*InMemoryStore[*creditnote.CreditNote]
	mu                  sync.RWMutex
	creditNoteLineItems map[string][]*creditnote.CreditNoteLineItem
	idempotencyKeyIndex map[string]string // idempotency_key -> credit_note_id
}

// NewInMemoryCreditNoteStore creates a new in-memory credit note store
func NewInMemoryCreditNoteStore() *InMemoryCreditNoteStore {
	return &InMemoryCreditNoteStore{
		InMemoryStore:       NewInMemoryStore[*creditnote.CreditNote](),
		creditNoteLineItems: make(map[string][]*creditnote.CreditNoteLineItem),
		idempotencyKeyIndex: make(map[string]string),
	}
}

// Helper to copy credit note
func copyCreditNote(cn *creditnote.CreditNote) *creditnote.CreditNote {
	if cn == nil {
		return nil
	}

	// Create a deep copy
	cloned := &creditnote.CreditNote{
		ID:               cn.ID,
		CreditNoteNumber: cn.CreditNoteNumber,
		InvoiceID:        cn.InvoiceID,
		CustomerID:       cn.CustomerID,
		SubscriptionID:   cn.SubscriptionID,
		CreditNoteStatus: cn.CreditNoteStatus,
		CreditNoteType:   cn.CreditNoteType,
		RefundStatus:     cn.RefundStatus,
		Reason:           cn.Reason,
		Memo:             cn.Memo,
		Currency:         cn.Currency,
		Metadata:         make(types.Metadata),
		EnvironmentID:    cn.EnvironmentID,
		TotalAmount:      cn.TotalAmount,
		IdempotencyKey:   cn.IdempotencyKey,
		BaseModel:        cn.BaseModel,
	}

	// Copy metadata
	for k, v := range cn.Metadata {
		cloned.Metadata[k] = v
	}

	return cloned
}

// Create creates a new credit note
func (s *InMemoryCreditNoteStore) Create(ctx context.Context, cn *creditnote.CreditNote) error {
	if cn == nil {
		return ierr.NewError("credit note cannot be nil").Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if cn.EnvironmentID == "" {
		cn.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Set timestamps if not set
	if cn.CreatedAt.IsZero() {
		cn.CreatedAt = time.Now().UTC()
	}
	if cn.UpdatedAt.IsZero() {
		cn.UpdatedAt = time.Now().UTC()
	}

	// Set tenant and user info from context
	if cn.TenantID == "" {
		cn.TenantID = types.GetTenantID(ctx)
	}
	if cn.CreatedBy == "" {
		cn.CreatedBy = types.GetUserID(ctx)
	}
	if cn.UpdatedBy == "" {
		cn.UpdatedBy = types.GetUserID(ctx)
	}

	// Create the credit note first
	err := s.InMemoryStore.Create(ctx, cn.ID, copyCreditNote(cn))
	if err != nil {
		return err
	}

	// Index idempotency key if provided
	if cn.IdempotencyKey != nil && *cn.IdempotencyKey != "" {
		s.mu.Lock()
		s.idempotencyKeyIndex[*cn.IdempotencyKey] = cn.ID
		s.mu.Unlock()
	}

	return nil
}

// Get retrieves a credit note by ID
func (s *InMemoryCreditNoteStore) Get(ctx context.Context, id string) (*creditnote.CreditNote, error) {
	cn, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	cloned := copyCreditNote(cn)
	// Load line items
	s.mu.RLock()
	if lineItems, exists := s.creditNoteLineItems[id]; exists {
		cloned.LineItems = s.cloneCreditNoteLineItems(lineItems)
	}
	s.mu.RUnlock()

	return cloned, nil
}

// Update updates a credit note
func (s *InMemoryCreditNoteStore) Update(ctx context.Context, cn *creditnote.CreditNote) error {
	if cn == nil {
		return ierr.NewError("credit note cannot be nil").Mark(ierr.ErrValidation)
	}

	// Update timestamp
	cn.UpdatedAt = time.Now().UTC()
	if cn.UpdatedBy == "" {
		cn.UpdatedBy = types.GetUserID(ctx)
	}

	return s.InMemoryStore.Update(ctx, cn.ID, copyCreditNote(cn))
}

// Delete marks a credit note as deleted
func (s *InMemoryCreditNoteStore) Delete(ctx context.Context, id string) error {
	// Get the credit note first to mark line items as deleted
	cn, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	userID := types.GetUserID(ctx)

	// Mark line items as deleted
	if lineItems, exists := s.creditNoteLineItems[id]; exists {
		for _, item := range lineItems {
			item.Status = types.StatusDeleted
			item.UpdatedAt = time.Now().UTC()
			item.UpdatedBy = userID
		}
	}
	s.mu.Unlock()

	// Mark credit note as deleted
	cn.Status = types.StatusDeleted
	cn.UpdatedAt = time.Now().UTC()
	cn.UpdatedBy = userID

	return s.InMemoryStore.Update(ctx, id, copyCreditNote(cn))
}

// List returns a list of credit notes based on the filter
func (s *InMemoryCreditNoteStore) List(ctx context.Context, filter *types.CreditNoteFilter) ([]*creditnote.CreditNote, error) {
	items, err := s.InMemoryStore.List(ctx, filter, creditNoteFilterFn, creditNoteSortFn)
	if err != nil {
		return nil, err
	}

	// Load line items for each credit note
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*creditnote.CreditNote, 0, len(items))
	for _, cn := range items {
		cloned := copyCreditNote(cn)
		if lineItems, exists := s.creditNoteLineItems[cn.ID]; exists {
			cloned.LineItems = s.cloneCreditNoteLineItems(lineItems)
		}
		result = append(result, cloned)
	}

	return result, nil
}

// Count returns the total count of credit notes based on the filter
func (s *InMemoryCreditNoteStore) Count(ctx context.Context, filter *types.CreditNoteFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, creditNoteFilterFn)
}

// AddLineItems adds line items to a credit note
func (s *InMemoryCreditNoteStore) AddLineItems(ctx context.Context, creditNoteID string, items []*creditnote.CreditNoteLineItem) error {
	// Check if credit note exists
	_, err := s.InMemoryStore.Get(ctx, creditNoteID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Add line items
	if s.creditNoteLineItems[creditNoteID] == nil {
		s.creditNoteLineItems[creditNoteID] = make([]*creditnote.CreditNoteLineItem, 0)
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	for _, item := range items {
		if item.CreatedAt.IsZero() {
			item.CreatedAt = time.Now().UTC()
		}
		if item.UpdatedAt.IsZero() {
			item.UpdatedAt = time.Now().UTC()
		}
		if item.CreatedBy == "" {
			item.CreatedBy = userID
		}
		if item.UpdatedBy == "" {
			item.UpdatedBy = userID
		}
		if item.TenantID == "" {
			item.TenantID = tenantID
		}
		if item.EnvironmentID == "" {
			item.EnvironmentID = environmentID
		}

		s.creditNoteLineItems[creditNoteID] = append(s.creditNoteLineItems[creditNoteID], s.cloneCreditNoteLineItem(item))
	}

	return nil
}

// RemoveLineItems removes line items from a credit note
func (s *InMemoryCreditNoteStore) RemoveLineItems(ctx context.Context, creditNoteID string, itemIDs []string) error {
	// Check if credit note exists
	_, err := s.InMemoryStore.Get(ctx, creditNoteID)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	userID := types.GetUserID(ctx)

	// Mark line items as deleted
	if lineItems, exists := s.creditNoteLineItems[creditNoteID]; exists {
		for _, item := range lineItems {
			if lo.Contains(itemIDs, item.ID) {
				item.Status = types.StatusDeleted
				item.UpdatedAt = time.Now().UTC()
				item.UpdatedBy = userID
			}
		}
	}

	return nil
}

// CreateWithLineItems creates a credit note with its line items
func (s *InMemoryCreditNoteStore) CreateWithLineItems(ctx context.Context, cn *creditnote.CreditNote) error {
	if cn == nil {
		return ierr.NewError("credit note cannot be nil").Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if cn.EnvironmentID == "" {
		cn.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Set timestamps if not set
	if cn.CreatedAt.IsZero() {
		cn.CreatedAt = time.Now().UTC()
	}
	if cn.UpdatedAt.IsZero() {
		cn.UpdatedAt = time.Now().UTC()
	}

	// Set tenant and user info from context
	if cn.TenantID == "" {
		cn.TenantID = types.GetTenantID(ctx)
	}
	if cn.CreatedBy == "" {
		cn.CreatedBy = types.GetUserID(ctx)
	}
	if cn.UpdatedBy == "" {
		cn.UpdatedBy = types.GetUserID(ctx)
	}

	// Create the credit note first
	err := s.InMemoryStore.Create(ctx, cn.ID, copyCreditNote(cn))
	if err != nil {
		return err
	}

	// Index idempotency key if provided
	if cn.IdempotencyKey != nil && *cn.IdempotencyKey != "" {
		s.mu.Lock()
		s.idempotencyKeyIndex[*cn.IdempotencyKey] = cn.ID
		s.mu.Unlock()
	}

	// Add line items
	if len(cn.LineItems) > 0 {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.creditNoteLineItems[cn.ID] = make([]*creditnote.CreditNoteLineItem, 0)
		for _, item := range cn.LineItems {
			if item.CreatedAt.IsZero() {
				item.CreatedAt = time.Now().UTC()
			}
			if item.UpdatedAt.IsZero() {
				item.UpdatedAt = time.Now().UTC()
			}
			if item.CreatedBy == "" {
				item.CreatedBy = types.GetUserID(ctx)
			}
			if item.UpdatedBy == "" {
				item.UpdatedBy = types.GetUserID(ctx)
			}
			if item.TenantID == "" {
				item.TenantID = cn.TenantID
			}
			if item.EnvironmentID == "" {
				item.EnvironmentID = cn.EnvironmentID
			}

			s.creditNoteLineItems[cn.ID] = append(s.creditNoteLineItems[cn.ID], s.cloneCreditNoteLineItem(item))
		}
	}

	return nil
}

// GetByIdempotencyKey retrieves a credit note by idempotency key
func (s *InMemoryCreditNoteStore) GetByIdempotencyKey(ctx context.Context, key string) (*creditnote.CreditNote, error) {
	s.mu.RLock()
	creditNoteID, exists := s.idempotencyKeyIndex[key]
	s.mu.RUnlock()

	if !exists {
		return nil, ierr.NewError("credit note not found").
			WithHintf("Credit note with idempotency key %s not found", key).
			Mark(ierr.ErrNotFound)
	}

	return s.Get(ctx, creditNoteID)
}

// Clear removes all credit notes from the store
func (s *InMemoryCreditNoteStore) Clear() {
	s.InMemoryStore.Clear()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creditNoteLineItems = make(map[string][]*creditnote.CreditNoteLineItem)
	s.idempotencyKeyIndex = make(map[string]string)
}

// Helper methods for filtering and sorting

func creditNoteFilterFn(ctx context.Context, cn *creditnote.CreditNote, filter interface{}) bool {
	if cn == nil {
		return false
	}

	f, ok := filter.(*types.CreditNoteFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID := types.GetTenantID(ctx); tenantID != "" {
		if cn.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, cn.EnvironmentID) {
		return false
	}

	// Check if deleted
	if cn.Status == types.StatusDeleted {
		return false
	}

	// Invoice ID filter
	if f.InvoiceID != "" && cn.InvoiceID != f.InvoiceID {
		return false
	}

	// Credit note type filter
	if f.CreditNoteType != "" && cn.CreditNoteType != f.CreditNoteType {
		return false
	}

	// Credit note IDs filter
	if len(f.CreditNoteIDs) > 0 && !lo.Contains(f.CreditNoteIDs, cn.ID) {
		return false
	}

	// Credit note status filter
	if len(f.CreditNoteStatus) > 0 && !lo.Contains(f.CreditNoteStatus, cn.CreditNoteStatus) {
		return false
	}

	// Time range filter
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil && cn.CreatedAt.Before(*f.TimeRangeFilter.StartTime) {
			return false
		}
		if f.TimeRangeFilter.EndTime != nil && cn.CreatedAt.After(*f.TimeRangeFilter.EndTime) {
			return false
		}
	}

	return true
}

func creditNoteSortFn(i, j *creditnote.CreditNote) bool {
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryCreditNoteStore) cloneCreditNoteLineItem(item *creditnote.CreditNoteLineItem) *creditnote.CreditNoteLineItem {
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

func (s *InMemoryCreditNoteStore) cloneCreditNoteLineItems(items []*creditnote.CreditNoteLineItem) []*creditnote.CreditNoteLineItem {
	if items == nil {
		return nil
	}

	cloned := make([]*creditnote.CreditNoteLineItem, 0, len(items))
	for _, item := range items {
		if item.Status != types.StatusDeleted {
			cloned = append(cloned, s.cloneCreditNoteLineItem(item))
		}
	}

	return cloned
}
