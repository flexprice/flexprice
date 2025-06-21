package testutil

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCreditNoteStore implements the creditnote.Repository interface for testing
type InMemoryCreditNoteStore struct {
	mu                  sync.RWMutex
	creditNotes         map[string]*creditnote.CreditNote
	creditNoteLineItems map[string][]*creditnote.CreditNoteLineItem
	idempotencyKeyIndex map[string]string // idempotency_key -> credit_note_id
}

// NewInMemoryCreditNoteStore creates a new in-memory credit note store
func NewInMemoryCreditNoteStore() *InMemoryCreditNoteStore {
	return &InMemoryCreditNoteStore{
		creditNotes:         make(map[string]*creditnote.CreditNote),
		creditNoteLineItems: make(map[string][]*creditnote.CreditNoteLineItem),
		idempotencyKeyIndex: make(map[string]string),
	}
}

// Create creates a new credit note
func (s *InMemoryCreditNoteStore) Create(ctx context.Context, cn *creditnote.CreditNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cn.ID == "" {
		return ierr.NewError("credit note ID is required").Mark(ierr.ErrValidation)
	}

	// Check if credit note already exists
	if _, exists := s.creditNotes[cn.ID]; exists {
		return ierr.NewError("credit note already exists").
			WithHintf("Credit note with ID %s already exists", cn.ID).
			Mark(ierr.ErrAlreadyExists)
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

	// Clone to avoid mutations
	cloned := s.cloneCreditNote(cn)
	s.creditNotes[cn.ID] = cloned

	return nil
}

// Get retrieves a credit note by ID
func (s *InMemoryCreditNoteStore) Get(ctx context.Context, id string) (*creditnote.CreditNote, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cn, exists := s.creditNotes[id]
	if !exists {
		return nil, ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	// Check tenant access
	if cn.TenantID != tenantID {
		return nil, ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	// Check environment access
	if environmentID != "" && cn.EnvironmentID != environmentID {
		return nil, ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	// Check if deleted
	if cn.Status == types.StatusDeleted {
		return nil, ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	cloned := s.cloneCreditNote(cn)
	// Load line items
	if lineItems, exists := s.creditNoteLineItems[id]; exists {
		cloned.LineItems = s.cloneCreditNoteLineItems(lineItems)
	}

	return cloned, nil
}

// Update updates a credit note
func (s *InMemoryCreditNoteStore) Update(ctx context.Context, cn *creditnote.CreditNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	existing, exists := s.creditNotes[cn.ID]
	if !exists {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", cn.ID).
			Mark(ierr.ErrNotFound)
	}

	// Check tenant access
	if existing.TenantID != tenantID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", cn.ID).
			Mark(ierr.ErrNotFound)
	}

	// Check environment access
	if environmentID != "" && existing.EnvironmentID != environmentID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", cn.ID).
			Mark(ierr.ErrNotFound)
	}

	// Check if deleted
	if existing.Status == types.StatusDeleted {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", cn.ID).
			Mark(ierr.ErrNotFound)
	}

	// Update fields
	updated := s.cloneCreditNote(existing)
	updated.CreditNoteStatus = cn.CreditNoteStatus
	updated.RefundStatus = cn.RefundStatus
	updated.Reason = cn.Reason
	updated.Metadata = cn.Metadata
	updated.UpdatedAt = time.Now().UTC()
	updated.UpdatedBy = userID

	s.creditNotes[cn.ID] = updated
	return nil
}

// Delete marks a credit note as deleted
func (s *InMemoryCreditNoteStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	cn, exists := s.creditNotes[id]
	if !exists {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	// Check tenant access
	if cn.TenantID != tenantID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	// Check environment access
	if environmentID != "" && cn.EnvironmentID != environmentID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", id).
			Mark(ierr.ErrNotFound)
	}

	// Mark as deleted
	cn.Status = types.StatusDeleted
	cn.UpdatedAt = time.Now().UTC()
	cn.UpdatedBy = userID

	// Mark line items as deleted
	if lineItems, exists := s.creditNoteLineItems[id]; exists {
		for _, item := range lineItems {
			item.Status = types.StatusDeleted
			item.UpdatedAt = time.Now().UTC()
			item.UpdatedBy = userID
		}
	}

	return nil
}

// List returns a list of credit notes based on the filter
func (s *InMemoryCreditNoteStore) List(ctx context.Context, filter *types.CreditNoteFilter) ([]*creditnote.CreditNote, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	var result []*creditnote.CreditNote

	for _, cn := range s.creditNotes {
		// Check tenant access
		if cn.TenantID != tenantID {
			continue
		}

		// Check environment access
		if environmentID != "" && cn.EnvironmentID != environmentID {
			continue
		}

		// Check if deleted
		if cn.Status == types.StatusDeleted {
			continue
		}

		// Apply filters
		if s.matchesFilter(cn, filter) {
			cloned := s.cloneCreditNote(cn)
			// Load line items
			if lineItems, exists := s.creditNoteLineItems[cn.ID]; exists {
				cloned.LineItems = s.cloneCreditNoteLineItems(lineItems)
			}
			result = append(result, cloned)
		}
	}

	// Sort results
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Apply pagination
	if filter != nil {
		offset := filter.GetOffset()
		limit := filter.GetLimit()

		if offset >= len(result) {
			return []*creditnote.CreditNote{}, nil
		}

		end := offset + limit
		if end > len(result) {
			end = len(result)
		}

		result = result[offset:end]
	}

	return result, nil
}

// Count returns the total count of credit notes based on the filter
func (s *InMemoryCreditNoteStore) Count(ctx context.Context, filter *types.CreditNoteFilter) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	count := 0

	for _, cn := range s.creditNotes {
		// Check tenant access
		if cn.TenantID != tenantID {
			continue
		}

		// Check environment access
		if environmentID != "" && cn.EnvironmentID != environmentID {
			continue
		}

		// Check if deleted
		if cn.Status == types.StatusDeleted {
			continue
		}

		// Apply filters
		if s.matchesFilter(cn, filter) {
			count++
		}
	}

	return count, nil
}

// AddLineItems adds line items to a credit note
func (s *InMemoryCreditNoteStore) AddLineItems(ctx context.Context, creditNoteID string, items []*creditnote.CreditNoteLineItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if credit note exists
	cn, exists := s.creditNotes[creditNoteID]
	if !exists {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", creditNoteID).
			Mark(ierr.ErrNotFound)
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Check tenant access
	if cn.TenantID != tenantID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", creditNoteID).
			Mark(ierr.ErrNotFound)
	}

	// Check environment access
	if environmentID != "" && cn.EnvironmentID != environmentID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", creditNoteID).
			Mark(ierr.ErrNotFound)
	}

	// Add line items
	if s.creditNoteLineItems[creditNoteID] == nil {
		s.creditNoteLineItems[creditNoteID] = make([]*creditnote.CreditNoteLineItem, 0)
	}

	for _, item := range items {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if credit note exists
	cn, exists := s.creditNotes[creditNoteID]
	if !exists {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", creditNoteID).
			Mark(ierr.ErrNotFound)
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	// Check tenant access
	if cn.TenantID != tenantID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", creditNoteID).
			Mark(ierr.ErrNotFound)
	}

	// Check environment access
	if environmentID != "" && cn.EnvironmentID != environmentID {
		return ierr.NewError("credit note not found").
			WithHintf("Credit note with ID %s not found", creditNoteID).
			Mark(ierr.ErrNotFound)
	}

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
	s.mu.Lock()
	defer s.mu.Unlock()

	if cn.ID == "" {
		return ierr.NewError("credit note ID is required").Mark(ierr.ErrValidation)
	}

	// Check if credit note already exists
	if _, exists := s.creditNotes[cn.ID]; exists {
		return ierr.NewError("credit note already exists").
			WithHintf("Credit note with ID %s already exists", cn.ID).
			Mark(ierr.ErrAlreadyExists)
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

	// Clone to avoid mutations
	cloned := s.cloneCreditNote(cn)
	s.creditNotes[cn.ID] = cloned

	// Add line items
	if len(cn.LineItems) > 0 {
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
	defer s.mu.RUnlock()

	creditNoteID, exists := s.idempotencyKeyIndex[key]
	if !exists {
		return nil, ierr.NewError("credit note not found").
			WithHintf("Credit note with idempotency key %s not found", key).
			Mark(ierr.ErrNotFound)
	}

	return s.Get(ctx, creditNoteID)
}

// Clear removes all credit notes from the store
func (s *InMemoryCreditNoteStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.creditNotes = make(map[string]*creditnote.CreditNote)
	s.creditNoteLineItems = make(map[string][]*creditnote.CreditNoteLineItem)
	s.idempotencyKeyIndex = make(map[string]string)
}

// Helper methods

func (s *InMemoryCreditNoteStore) matchesFilter(cn *creditnote.CreditNote, filter *types.CreditNoteFilter) bool {
	if filter == nil {
		return true
	}

	// Invoice ID filter
	if filter.InvoiceID != "" && cn.InvoiceID != filter.InvoiceID {
		return false
	}

	// Credit note type filter
	if filter.CreditNoteType != "" && cn.CreditNoteType != filter.CreditNoteType {
		return false
	}

	// Credit note IDs filter
	if len(filter.CreditNoteIDs) > 0 && !lo.Contains(filter.CreditNoteIDs, cn.ID) {
		return false
	}

	// Credit note status filter
	if len(filter.CreditNoteStatus) > 0 && !lo.Contains(filter.CreditNoteStatus, cn.CreditNoteStatus) {
		return false
	}

	// Time range filter
	if filter.TimeRangeFilter != nil {
		if filter.TimeRangeFilter.StartTime != nil && cn.CreatedAt.Before(*filter.TimeRangeFilter.StartTime) {
			return false
		}
		if filter.TimeRangeFilter.EndTime != nil && cn.CreatedAt.After(*filter.TimeRangeFilter.EndTime) {
			return false
		}
	}

	return true
}

func (s *InMemoryCreditNoteStore) cloneCreditNote(cn *creditnote.CreditNote) *creditnote.CreditNote {
	if cn == nil {
		return nil
	}

	// Create a deep copy
	cloned := &creditnote.CreditNote{
		ID:               cn.ID,
		CreditNoteNumber: cn.CreditNoteNumber,
		InvoiceID:        cn.InvoiceID,
		CreditNoteStatus: cn.CreditNoteStatus,
		CreditNoteType:   cn.CreditNoteType,
		RefundStatus:     cn.RefundStatus,
		Reason:           cn.Reason,
		Memo:             cn.Memo,
		Currency:         cn.Currency,
		Metadata:         make(types.Metadata),
		EnvironmentID:    cn.EnvironmentID,
		TotalAmount:      cn.TotalAmount,
		BaseModel:        cn.BaseModel,
	}

	// Copy metadata
	for k, v := range cn.Metadata {
		cloned.Metadata[k] = v
	}

	return cloned
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
