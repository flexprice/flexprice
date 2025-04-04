package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/payment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryPaymentStore implements payment.Repository
type InMemoryPaymentStore struct {
	*InMemoryStore[*payment.Payment]
	mu             sync.RWMutex
	attempts       map[string]*payment.PaymentAttempt
	createdInOrder []*payment.Payment
}

// NewInMemoryPaymentStore creates a new in-memory payment repository
func NewInMemoryPaymentStore() *InMemoryPaymentStore {
	return &InMemoryPaymentStore{
		InMemoryStore:  NewInMemoryStore[*payment.Payment](),
		attempts:       make(map[string]*payment.PaymentAttempt),
		createdInOrder: make([]*payment.Payment, 0),
	}
}

// Clear resets all stored data
func (m *InMemoryPaymentStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InMemoryStore.Clear()
	m.attempts = make(map[string]*payment.PaymentAttempt)
	m.createdInOrder = make([]*payment.Payment, 0)
}

// Create stores a new payment
func (m *InMemoryPaymentStore) Create(ctx context.Context, p *payment.Payment) error {
	if p == nil {
		return ierr.NewError("payment cannot be nil").
			WithHint("Payment cannot be nil").
			Mark(ierr.ErrValidation)
	}

	if p.ID == "" {
		return ierr.NewError("payment ID cannot be empty").
			WithHint("Payment ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	err := m.InMemoryStore.Create(ctx, p.ID, p)
	if err != nil {
		return err
	}

	m.createdInOrder = append(m.createdInOrder, p)
	return nil
}

// Get retrieves a payment by ID
func (m *InMemoryPaymentStore) Get(ctx context.Context, id string) (*payment.Payment, error) {
	return m.InMemoryStore.Get(ctx, id)
}

// Update updates an existing payment
func (m *InMemoryPaymentStore) Update(ctx context.Context, p *payment.Payment) error {
	if p == nil {
		return ierr.NewError("payment cannot be nil").
			WithHint("Payment cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Update timestamp
	p.UpdatedAt = time.Now().UTC()

	return m.InMemoryStore.Update(ctx, p.ID, p)
}

// Delete removes a payment
func (m *InMemoryPaymentStore) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// First check if the payment exists
	_, err := m.InMemoryStore.Get(ctx, id)
	if err != nil {
		return err
	}

	// Remove from InMemoryStore
	err = m.InMemoryStore.Delete(ctx, id)
	if err != nil {
		return err
	}

	// Remove from createdInOrder
	for i, payment := range m.createdInOrder {
		if payment.ID == id {
			m.createdInOrder = append(m.createdInOrder[:i], m.createdInOrder[i+1:]...)
			break
		}
	}

	return nil
}

// GetByIdempotencyKey retrieves a payment by idempotency key
func (m *InMemoryPaymentStore) GetByIdempotencyKey(ctx context.Context, key string) (*payment.Payment, error) {
	payments, err := m.List(ctx, &types.PaymentFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, err
	}

	for _, p := range payments {
		if p.IdempotencyKey == key {
			return p, nil
		}
	}

	return nil, ierr.NewError("payment not found").
		WithHint(fmt.Sprintf("Payment not found for idempotency key: %s", key)).
		Mark(ierr.ErrNotFound)
}

// CreateAttempt creates a new payment attempt
func (m *InMemoryPaymentStore) CreateAttempt(ctx context.Context, attempt *payment.PaymentAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if attempt.ID == "" {
		return ierr.NewError("attempt ID cannot be empty").
			WithHint("Attempt ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	// Check if payment exists
	_, err := m.InMemoryStore.Get(ctx, attempt.PaymentID)
	if err != nil {
		return err
	}

	// Set environment ID from context if not already set
	if attempt.EnvironmentID == "" {
		attempt.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	m.attempts[attempt.ID] = attempt
	return nil
}

// UpdateAttempt updates an existing payment attempt
func (m *InMemoryPaymentStore) UpdateAttempt(ctx context.Context, attempt *payment.PaymentAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.attempts[attempt.ID]
	if !exists {
		return ierr.NewError("attempt not found").
			WithHint(fmt.Sprintf("Attempt not found: %s", attempt.ID)).
			Mark(ierr.ErrNotFound)
	}

	// Check tenant ID if present in context
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if existing.TenantID != tenantID {
			return ierr.NewError("tenant ID mismatch").
				WithHint("Tenant ID mismatch").
				Mark(ierr.ErrDatabase)
		}
	}

	// Update timestamp
	attempt.UpdatedAt = time.Now().UTC()

	m.attempts[attempt.ID] = attempt
	return nil
}

// GetAttempt retrieves a payment attempt by ID
func (m *InMemoryPaymentStore) GetAttempt(ctx context.Context, id string) (*payment.PaymentAttempt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	attempt, exists := m.attempts[id]
	if !exists {
		return nil, ierr.NewError("attempt not found").
			WithHint(fmt.Sprintf("Attempt not found: %s", id)).
			Mark(ierr.ErrNotFound)
	}

	// Check tenant ID if present in context
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if attempt.TenantID != tenantID {
			return nil, ierr.NewError("tenant ID mismatch").
				WithHint("Tenant ID mismatch").
				Mark(ierr.ErrNotFound)
		}
	}

	return attempt, nil
}

// ListAttempts returns a list of payment attempts for a payment
func (m *InMemoryPaymentStore) ListAttempts(ctx context.Context, paymentID string) ([]*payment.PaymentAttempt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*payment.PaymentAttempt

	for _, a := range m.attempts {
		if a.PaymentID == paymentID {
			// Check tenant ID if present in context
			if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
				if a.TenantID != tenantID {
					continue
				}
			}

			// Apply environment filter
			if !CheckEnvironmentFilter(ctx, a.EnvironmentID) {
				continue
			}

			result = append(result, a)
		}
	}

	if len(result) == 0 {
		return nil, ierr.NewError("no attempts found").
			WithHint(fmt.Sprintf("No attempts found for payment: %s", paymentID)).
			Mark(ierr.ErrNotFound)
	}

	// Sort by attempt number (ascending)
	sort.Slice(result, func(i, j int) bool {
		return result[i].AttemptNumber < result[j].AttemptNumber
	})

	return result, nil
}

// GetLatestAttempt retrieves the latest attempt for a payment
func (m *InMemoryPaymentStore) GetLatestAttempt(ctx context.Context, paymentID string) (*payment.PaymentAttempt, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var latest *payment.PaymentAttempt

	for _, a := range m.attempts {
		if a.PaymentID == paymentID {
			// Check tenant ID if present in context
			if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
				if a.TenantID != tenantID {
					continue
				}
			}

			// Apply environment filter
			if !CheckEnvironmentFilter(ctx, a.EnvironmentID) {
				continue
			}

			if latest == nil || a.AttemptNumber > latest.AttemptNumber {
				latest = a
			}
		}
	}

	if latest == nil {
		return nil, ierr.NewError("no attempts found").
			WithHint(fmt.Sprintf("No attempts found for payment: %s", paymentID)).
			Mark(ierr.ErrNotFound)
	}

	return latest, nil
}

// paymentFilterFn implements filtering logic for payments
func paymentFilterFn(ctx context.Context, p *payment.Payment, filter interface{}) bool {
	if p == nil {
		return false
	}

	f, ok := filter.(*types.PaymentFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if p.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, p.EnvironmentID) {
		return false
	}

	// Apply specific filter conditions
	if f.DestinationID != nil && p.DestinationID != *f.DestinationID {
		return false
	}

	if f.DestinationType != nil && string(p.DestinationType) != *f.DestinationType {
		return false
	}

	if f.PaymentStatus != nil && string(p.PaymentStatus) != *f.PaymentStatus {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && p.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && p.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// paymentSortFn implements sorting logic for payments
func paymentSortFn(i, j *payment.Payment) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

// List returns a list of payments based on the filter
func (m *InMemoryPaymentStore) List(ctx context.Context, filter *types.PaymentFilter) ([]*payment.Payment, error) {
	return m.InMemoryStore.List(ctx, filter, paymentFilterFn, paymentSortFn)
}

// Count returns the number of payments matching the filter
func (m *InMemoryPaymentStore) Count(ctx context.Context, filter *types.PaymentFilter) (int, error) {
	return m.InMemoryStore.Count(ctx, filter, paymentFilterFn)
}

// GetPaymentsForDestination returns payments for a specific destination
func (m *InMemoryPaymentStore) GetPaymentsForDestination(ctx context.Context, destinationType types.PaymentDestinationType, destinationID string) ([]*payment.Payment, error) {
	filter := &types.PaymentFilter{
		DestinationType: lo.ToPtr(string(destinationType)),
		DestinationID:   &destinationID,
		QueryFilter:     types.NewNoLimitQueryFilter(),
	}

	payments, err := m.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	// If not invoice destination type, return as is
	if destinationType != types.PaymentDestinationTypeInvoice {
		return payments, nil
	}

	// For invoice payments, fetch the invoice to get its number
	invoiceFilter := &types.InvoiceFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		InvoiceIDs:  []string{destinationID},
	}

	// Get the invoice repository from the payment store
	invoiceRepo := NewInMemoryInvoiceStore()
	invoices, err := invoiceRepo.List(ctx, invoiceFilter)
	if err != nil {
		return nil, err
	}

	// If invoice found, set its number in the payment response
	if len(invoices) > 0 {
		invoice := invoices[0]
		for _, p := range payments {
			if p.DestinationType == types.PaymentDestinationTypeInvoice && p.DestinationID == invoice.ID {
				p.InvoiceNumber = invoice.InvoiceNumber
			}
		}
	}

	return payments, nil
}

// GetCreatedPayments returns all payments that were created, in order of creation
// This is a helper method for testing
func (m *InMemoryPaymentStore) GetCreatedPayments() []*payment.Payment {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*payment.Payment, len(m.createdInOrder))
	copy(result, m.createdInOrder)
	return result
}
