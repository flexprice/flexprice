package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCreditGrantApplicationStore provides an in-memory implementation of creditgrantapplication.Repository for testing
type InMemoryCreditGrantApplicationStore struct {
	*InMemoryStore[*creditgrantapplication.CreditGrantApplication]
}

// NewInMemoryCreditGrantApplicationStore creates a new in-memory credit grant application store
func NewInMemoryCreditGrantApplicationStore() *InMemoryCreditGrantApplicationStore {
	return &InMemoryCreditGrantApplicationStore{
		InMemoryStore: NewInMemoryStore[*creditgrantapplication.CreditGrantApplication](),
	}
}

// Helper to copy application
func copyCreditGrantApplication(cga *creditgrantapplication.CreditGrantApplication) *creditgrantapplication.CreditGrantApplication {
	if cga == nil {
		return nil
	}

	copy := *cga
	if cga.PeriodStart != nil {
		start := *cga.PeriodStart
		copy.PeriodStart = &start
	}
	if cga.PeriodEnd != nil {
		end := *cga.PeriodEnd
		copy.PeriodEnd = &end
	}
	if cga.AppliedAt != nil {
		applied := *cga.AppliedAt
		copy.AppliedAt = &applied
	}
	if cga.FailureReason != nil {
		reason := *cga.FailureReason
		copy.FailureReason = &reason
	}

	// Deep copy metadata
	if cga.Metadata != nil {
		copy.Metadata = lo.Assign(map[string]string{}, cga.Metadata)
	}

	return &copy
}

// Create creates a new credit grant application
func (s *InMemoryCreditGrantApplicationStore) Create(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication) error {
	// Set environment ID from context if not already set
	if cga.EnvironmentID == "" {
		cga.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, cga.ID, copyCreditGrantApplication(cga))
}

// Get retrieves a credit grant application by ID
func (s *InMemoryCreditGrantApplicationStore) Get(ctx context.Context, id string) (*creditgrantapplication.CreditGrantApplication, error) {
	cga, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	return copyCreditGrantApplication(cga), nil
}

// List retrieves credit grant applications based on filter
func (s *InMemoryCreditGrantApplicationStore) List(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*creditgrantapplication.CreditGrantApplication, error) {
	items, err := s.InMemoryStore.List(ctx, filter, creditGrantApplicationFilterFn, creditGrantApplicationSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(cga *creditgrantapplication.CreditGrantApplication, _ int) *creditgrantapplication.CreditGrantApplication {
		return copyCreditGrantApplication(cga)
	}), nil
}

// Count counts credit grant applications based on filter
func (s *InMemoryCreditGrantApplicationStore) Count(ctx context.Context, filter *types.CreditGrantApplicationFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, creditGrantApplicationFilterFn)
}

// ListAll retrieves all credit grant applications based on filter without pagination
func (s *InMemoryCreditGrantApplicationStore) ListAll(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*creditgrantapplication.CreditGrantApplication, error) {
	if filter == nil {
		filter = types.NewNoLimitCreditGrantApplicationFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	return s.List(ctx, filter)
}

// Update updates an existing credit grant application
func (s *InMemoryCreditGrantApplicationStore) Update(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication) error {
	return s.InMemoryStore.Update(ctx, cga.ID, copyCreditGrantApplication(cga))
}

// Delete deletes a credit grant application
func (s *InMemoryCreditGrantApplicationStore) Delete(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication) error {
	return s.InMemoryStore.Delete(ctx, cga.ID)
}

// FindAllScheduledApplications retrieves all scheduled applications
func (s *InMemoryCreditGrantApplicationStore) FindAllScheduledApplications(ctx context.Context) ([]*creditgrantapplication.CreditGrantApplication, error) {
	filter := &types.CreditGrantApplicationFilter{
		QueryFilter:         types.NewNoLimitQueryFilter(),
		ApplicationStatuses: []types.ApplicationStatus{types.ApplicationStatusPending, types.ApplicationStatusFailed},
	}

	return s.List(ctx, filter)
}

// FindByIdempotencyKey finds a credit grant application by idempotency key
func (s *InMemoryCreditGrantApplicationStore) FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*creditgrantapplication.CreditGrantApplication, error) {
	// Create a filter function that matches by idempotency key
	filterFn := func(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication, _ interface{}) bool {
		return cga.IdempotencyKey == idempotencyKey &&
			cga.TenantID == types.GetTenantID(ctx) &&
			CheckEnvironmentFilter(ctx, cga.EnvironmentID)
	}

	// List all applications with our filter
	applications, err := s.InMemoryStore.List(ctx, nil, filterFn, nil)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list credit grant applications").
			Mark(ierr.ErrDatabase)
	}

	if len(applications) == 0 {
		return nil, ierr.NewError("credit grant application not found").
			WithHintf("Credit grant application with idempotency key %s was not found", idempotencyKey).
			WithReportableDetails(map[string]any{
				"idempotency_key": idempotencyKey,
			}).
			Mark(ierr.ErrNotFound)
	}

	return copyCreditGrantApplication(applications[0]), nil
}

// creditGrantApplicationFilterFn implements filtering logic for credit grant applications
func creditGrantApplicationFilterFn(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication, filter interface{}) bool {
	f, ok := filter.(*types.CreditGrantApplicationFilter)
	if !ok {
		return false
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && cga.TenantID != tenantID {
		return false
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, cga.EnvironmentID) {
		return false
	}

	// Check application IDs filter
	if len(f.ApplicationIDs) > 0 && !lo.Contains(f.ApplicationIDs, cga.ID) {
		return false
	}

	// Check credit grant IDs filter
	if len(f.CreditGrantIDs) > 0 && !lo.Contains(f.CreditGrantIDs, cga.CreditGrantID) {
		return false
	}

	// Check subscription IDs filter
	if len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, cga.SubscriptionID) {
		return false
	}

	// Check application statuses filter
	if len(f.ApplicationStatuses) > 0 && !lo.Contains(f.ApplicationStatuses, cga.ApplicationStatus) {
		return false
	}

	// Check scheduled for filter
	if f.ScheduledFor != nil && !cga.ScheduledFor.Equal(*f.ScheduledFor) {
		return false
	}

	// Check applied at filter
	if f.AppliedAt != nil {
		if cga.AppliedAt == nil || !cga.AppliedAt.Equal(*f.AppliedAt) {
			return false
		}
	}

	return true
}

// creditGrantApplicationSortFn implements sorting logic for credit grant applications
func creditGrantApplicationSortFn(i, j *creditgrantapplication.CreditGrantApplication) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}

// Clear removes all credit grant applications from the store
func (s *InMemoryCreditGrantApplicationStore) Clear() {
	s.InMemoryStore.Clear()
}
