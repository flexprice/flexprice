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
	// statusSnapshots records the status as last written, so a compare-and-set
	// is not defeated by callers mutating the payment pointer this store handed
	// them. See UpdateWithExpectedStatus.
	statusSnapshots map[string]types.PaymentStatus
}

// NewInMemoryPaymentStore creates a new in-memory payment repository
func NewInMemoryPaymentStore() *InMemoryPaymentStore {
	return &InMemoryPaymentStore{
		InMemoryStore:   NewInMemoryStore[*payment.Payment](),
		attempts:        make(map[string]*payment.PaymentAttempt),
		createdInOrder:  make([]*payment.Payment, 0),
		statusSnapshots: make(map[string]types.PaymentStatus),
	}
}

// Clear resets all stored data
func (m *InMemoryPaymentStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InMemoryStore.Clear()
	m.attempts = make(map[string]*payment.PaymentAttempt)
	m.createdInOrder = make([]*payment.Payment, 0)
	m.statusSnapshots = make(map[string]types.PaymentStatus)
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

	// Same for tenant. This must be persisted onto p, not just resolved into a
	// local: the duplicate check below compares against stored payments, so a row
	// kept with an empty TenantID would never match a later retry carrying the
	// context tenant, silently defeating the uniqueness check.
	if p.TenantID == "" {
		p.TenantID = types.GetTenantID(ctx)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Mirror the (tenant_id, environment_id, idempotency_key) unique index from
	// ent/schema/payment.go. Without this, the store keys only on p.ID and any
	// test of the idempotent-create path would pass vacuously — the duplicate
	// insert would silently succeed instead of surfacing ErrAlreadyExists.
	if p.IdempotencyKey != "" {
		for _, existing := range m.createdInOrder {
			if existing.IdempotencyKey == p.IdempotencyKey &&
				existing.TenantID == p.TenantID &&
				existing.EnvironmentID == p.EnvironmentID {
				return ierr.WithError(payment.ErrIdempotencyKeyConflict).
					WithHint("A payment with this idempotency key already exists").
					WithReportableDetails(map[string]interface{}{
						"idempotency_key": p.IdempotencyKey,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
		}
	}

	err := m.InMemoryStore.Create(ctx, p.ID, p)
	if err != nil {
		return err
	}

	m.createdInOrder = append(m.createdInOrder, p)
	m.statusSnapshots[p.ID] = p.PaymentStatus
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

	m.mu.Lock()
	m.statusSnapshots[p.ID] = p.PaymentStatus
	m.mu.Unlock()

	return m.InMemoryStore.Update(ctx, p.ID, p)
}

// UpdateWithExpectedStatus updates the payment only while its stored status
// still matches expectedStatus, mirroring the compare-and-set the real
// repository performs in a single statement.
//
// The comparison uses a stored snapshot rather than the live entry: this store
// hands out pointers to its own values, so a caller that mutates the payment it
// read would otherwise also mutate what we compare against, and the check would
// always appear to match. The real repository re-reads from the database and
// does not have that aliasing.
func (m *InMemoryPaymentStore) UpdateWithExpectedStatus(ctx context.Context, p *payment.Payment, expectedStatus types.PaymentStatus) error {
	if p == nil {
		return ierr.NewError("payment cannot be nil").
			WithHint("Payment cannot be nil").
			Mark(ierr.ErrValidation)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	storedStatus, ok := m.statusSnapshots[p.ID]
	if !ok {
		existing, err := m.InMemoryStore.Get(ctx, p.ID)
		if err != nil {
			return err
		}
		storedStatus = existing.PaymentStatus
	}
	if storedStatus != expectedStatus {
		return ierr.NewError("payment status changed during update").
			WithHint("The payment was modified concurrently. Retry with the current payment state.").
			WithReportableDetails(map[string]interface{}{
				"payment_id":      p.ID,
				"expected_status": expectedStatus,
			}).
			Mark(ierr.ErrVersionConflict)
	}

	p.UpdatedAt = time.Now().UTC()
	m.statusSnapshots[p.ID] = p.PaymentStatus
	return m.InMemoryStore.Update(ctx, p.ID, p)
}

// Delete removes a payment
// DeleteWithExpectedStatus deletes only while the stored status still matches
// expectedStatus, mirroring the predicate the real repository applies.
func (m *InMemoryPaymentStore) DeleteWithExpectedStatus(ctx context.Context, id string, expectedStatus types.PaymentStatus) error {
	m.mu.Lock()
	storedStatus, ok := m.statusSnapshots[id]
	m.mu.Unlock()

	if ok && storedStatus != expectedStatus {
		return ierr.NewError("payment status changed during delete").
			WithHint("The payment was modified concurrently. Re-read it before deleting.").
			WithReportableDetails(map[string]interface{}{
				"payment_id":      id,
				"expected_status": expectedStatus,
			}).
			Mark(ierr.ErrVersionConflict)
	}

	return m.Delete(ctx, id)
}

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

// ListScopedByDestinationStatusGateway returns matching payments across all tenants
// and environments, mirroring the cross-tenant repository query used by crons.
func (m *InMemoryPaymentStore) ListScopedByDestinationStatusGateway(ctx context.Context, destinationType types.PaymentDestinationType, status types.PaymentStatus, gateway types.PaymentGatewayType) ([]payment.ScopedPayment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []payment.ScopedPayment
	for _, p := range m.createdInOrder {
		if p.DestinationType != destinationType {
			continue
		}
		if p.PaymentStatus != status {
			continue
		}
		if p.PaymentGateway == nil || types.PaymentGatewayType(*p.PaymentGateway) != gateway {
			continue
		}
		if p.GatewayPaymentID == nil || *p.GatewayPaymentID == "" {
			continue
		}
		if p.Status == types.StatusDeleted {
			continue
		}
		result = append(result, payment.ScopedPayment{
			PaymentID:        p.ID,
			TenantID:         p.TenantID,
			EnvironmentID:    p.EnvironmentID,
			GatewayPaymentID: *p.GatewayPaymentID,
		})
	}
	return result, nil
}

// GetPaymentsForDestination returns payments for a specific destination
func (m *InMemoryPaymentStore) GetPaymentsForDestination(ctx context.Context, destinationType types.PaymentDestinationType, destinationID string) ([]*payment.Payment, error) {
	filter := &types.PaymentFilter{
		DestinationType: lo.ToPtr(string(destinationType)),
		DestinationID:   &destinationID,
		QueryFilter:     types.NewNoLimitQueryFilter(),
	}

	return m.List(ctx, filter)
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
