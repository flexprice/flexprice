package testutil

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/flexprice/flexprice/internal/domain/refund"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryRefundStore implements refund.Repository
type InMemoryRefundStore struct {
	*InMemoryStore[*refund.Refund]
}

// NewInMemoryRefundStore creates a new in-memory refund repository
func NewInMemoryRefundStore() *InMemoryRefundStore {
	return &InMemoryRefundStore{
		InMemoryStore: NewInMemoryStore[*refund.Refund](),
	}
}

// Clear resets all stored data
func (m *InMemoryRefundStore) Clear() {
	m.InMemoryStore.Clear()
}

// Create stores a new refund
func (m *InMemoryRefundStore) Create(ctx context.Context, r *refund.Refund) error {
	if r == nil {
		return ierr.NewError("refund cannot be nil").
			WithHint("Refund cannot be nil").
			Mark(ierr.ErrValidation)
	}

	if r.ID == "" {
		return ierr.NewError("refund ID cannot be empty").
			WithHint("Refund ID cannot be empty").
			Mark(ierr.ErrValidation)
	}

	if r.EnvironmentID == "" {
		r.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return m.InMemoryStore.Create(ctx, r.ID, r)
}

// Get retrieves a refund by ID
func (m *InMemoryRefundStore) Get(ctx context.Context, id string) (*refund.Refund, error) {
	return m.InMemoryStore.Get(ctx, id)
}

// Update updates an existing refund
func (m *InMemoryRefundStore) Update(ctx context.Context, r *refund.Refund) error {
	if r == nil {
		return ierr.NewError("refund cannot be nil").
			WithHint("Refund cannot be nil").
			Mark(ierr.ErrValidation)
	}

	r.UpdatedAt = time.Now().UTC()

	return m.InMemoryStore.Update(ctx, r.ID, r)
}

// Delete removes a refund
func (m *InMemoryRefundStore) Delete(ctx context.Context, id string) error {
	return m.InMemoryStore.Delete(ctx, id)
}

// GetByIdempotencyKey retrieves a refund by idempotency key
func (m *InMemoryRefundStore) GetByIdempotencyKey(ctx context.Context, key string) (*refund.Refund, error) {
	refunds, err := m.List(ctx, &types.RefundFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, err
	}

	for _, r := range refunds {
		if r.IdempotencyKey == key {
			return r, nil
		}
	}

	return nil, ierr.NewError("refund not found").
		WithHint(fmt.Sprintf("Refund not found for idempotency key: %s", key)).
		Mark(ierr.ErrNotFound)
}

// refundFilterFn implements filter matching logic for refunds
func refundFilterFn(ctx context.Context, r *refund.Refund, filter interface{}) bool {
	if r == nil {
		return false
	}

	f, ok := filter.(*types.RefundFilter)
	if !ok {
		return true
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if r.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, r.EnvironmentID) {
		return false
	}

	if f.PaymentIDs != nil && !slices.Contains(f.PaymentIDs, r.PaymentID) {
		return false
	}

	if f.RefundStatuses != nil && !slices.Contains(f.RefundStatuses, r.RefundStatus) {
		return false
	}

	if f.Gateway != nil && r.PaymentGateway != *f.Gateway {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && r.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && r.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// refundSortFn implements sorting logic for refunds (newest first)
func refundSortFn(i, j *refund.Refund) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

// List returns a list of refunds based on the filter
func (m *InMemoryRefundStore) List(ctx context.Context, filter *types.RefundFilter) ([]*refund.Refund, error) {
	return m.InMemoryStore.List(ctx, filter, refundFilterFn, refundSortFn)
}

// Count returns the number of refunds matching the filter
func (m *InMemoryRefundStore) Count(ctx context.Context, filter *types.RefundFilter) (int, error) {
	return m.InMemoryStore.Count(ctx, filter, refundFilterFn)
}
