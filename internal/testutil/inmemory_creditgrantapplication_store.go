package testutil

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/creditgrantapplication"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCreditGrantApplicationStore provides an in-memory implementation of creditgrantapplication.Repository for testing
type InMemoryCreditGrantApplicationStore struct {
	mu           sync.RWMutex
	applications map[string]*creditgrantapplication.CreditGrantApplication
}

// NewInMemoryCreditGrantApplicationStore creates a new in-memory credit grant application store
func NewInMemoryCreditGrantApplicationStore() *InMemoryCreditGrantApplicationStore {
	return &InMemoryCreditGrantApplicationStore{
		applications: make(map[string]*creditgrantapplication.CreditGrantApplication),
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already exists
	if _, exists := s.applications[cga.ID]; exists {
		return ierr.NewError("credit grant application already exists").
			WithHint("A credit grant application with this ID already exists").
			WithReportableDetails(map[string]interface{}{"id": cga.ID}).
			Mark(ierr.ErrAlreadyExists)
	}

	// Set environment ID from context if not already set
	if cga.EnvironmentID == "" {
		cga.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Store copy to avoid mutation
	stored := copyCreditGrantApplication(cga)
	s.applications[cga.ID] = stored

	return nil
}

// Get retrieves a credit grant application by ID
func (s *InMemoryCreditGrantApplicationStore) Get(ctx context.Context, id string) (*creditgrantapplication.CreditGrantApplication, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cga, exists := s.applications[id]
	if !exists {
		return nil, ierr.NewError("credit grant application not found").
			WithHint("No credit grant application found with the given ID").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrNotFound)
	}

	return copyCreditGrantApplication(cga), nil
}

// List retrieves credit grant applications based on filter
func (s *InMemoryCreditGrantApplicationStore) List(ctx context.Context, filter *types.CreditGrantApplicationFilter) ([]*creditgrantapplication.CreditGrantApplication, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*creditgrantapplication.CreditGrantApplication

	for _, cga := range s.applications {
		if s.matchesFilter(ctx, cga, filter) {
			results = append(results, copyCreditGrantApplication(cga))
		}
	}

	// Apply sorting
	if filter != nil && filter.QueryFilter != nil && filter.QueryFilter.Sort != nil {
		sort.Slice(results, func(i, j int) bool {
			switch *filter.QueryFilter.Sort {
			case "scheduled_for":
				if filter.QueryFilter.Order != nil && *filter.QueryFilter.Order == "asc" {
					return results[i].ScheduledFor.Before(results[j].ScheduledFor)
				}
				return results[i].ScheduledFor.After(results[j].ScheduledFor)
			case "created_at":
				if filter.QueryFilter.Order != nil && *filter.QueryFilter.Order == "asc" {
					return results[i].CreatedAt.Before(results[j].CreatedAt)
				}
				return results[i].CreatedAt.After(results[j].CreatedAt)
			default:
				return results[i].CreatedAt.After(results[j].CreatedAt)
			}
		})
	}

	// Apply pagination
	if filter != nil && filter.QueryFilter != nil {
		offset := filter.GetOffset()
		limit := filter.GetLimit()

		if offset > len(results) {
			return []*creditgrantapplication.CreditGrantApplication{}, nil
		}

		end := offset + limit
		if end > len(results) {
			end = len(results)
		}

		results = results[offset:end]
	}

	return results, nil
}

// Count counts credit grant applications based on filter
func (s *InMemoryCreditGrantApplicationStore) Count(ctx context.Context, filter *types.CreditGrantApplicationFilter) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, cga := range s.applications {
		if s.matchesFilter(ctx, cga, filter) {
			count++
		}
	}

	return count, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.applications[cga.ID]; !exists {
		return ierr.NewError("credit grant application not found").
			WithHint("No credit grant application found with the given ID").
			WithReportableDetails(map[string]interface{}{"id": cga.ID}).
			Mark(ierr.ErrNotFound)
	}

	// Store copy to avoid mutation
	stored := copyCreditGrantApplication(cga)
	s.applications[cga.ID] = stored

	return nil
}

// Delete deletes a credit grant application
func (s *InMemoryCreditGrantApplicationStore) Delete(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.applications[cga.ID]; !exists {
		return ierr.NewError("credit grant application not found").
			WithHint("No credit grant application found with the given ID").
			WithReportableDetails(map[string]interface{}{"id": cga.ID}).
			Mark(ierr.ErrNotFound)
	}

	delete(s.applications, cga.ID)
	return nil
}

// ExistsForPeriod checks if a credit grant application exists for a specific period
func (s *InMemoryCreditGrantApplicationStore) ExistsForPeriod(ctx context.Context, grantID, subscriptionID string, periodStart, periodEnd time.Time) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, cga := range s.applications {
		if cga.CreditGrantID == grantID &&
			cga.SubscriptionID == subscriptionID &&
			cga.PeriodStart != nil && cga.PeriodEnd != nil &&
			cga.PeriodStart.Equal(periodStart) &&
			cga.PeriodEnd.Equal(periodEnd) &&
			cga.ApplicationStatus != types.ApplicationStatusCancelled &&
			cga.ApplicationStatus != types.ApplicationStatusFailed {
			return true, nil
		}
	}

	return false, nil
}

// FindAllScheduledApplications finds all applications scheduled for processing
func (s *InMemoryCreditGrantApplicationStore) FindAllScheduledApplications(ctx context.Context) ([]*creditgrantapplication.CreditGrantApplication, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*creditgrantapplication.CreditGrantApplication
	now := time.Now().UTC()

	for _, cga := range s.applications {
		if (cga.ApplicationStatus == types.ApplicationStatusPending || cga.ApplicationStatus == types.ApplicationStatusFailed) &&
			cga.ScheduledFor.Before(now) {
			results = append(results, copyCreditGrantApplication(cga))
		}
	}

	return results, nil
}

// FindByIdempotencyKey finds a credit grant application by idempotency key
func (s *InMemoryCreditGrantApplicationStore) FindByIdempotencyKey(ctx context.Context, idempotencyKey string) (*creditgrantapplication.CreditGrantApplication, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, cga := range s.applications {
		if cga.IdempotencyKey == idempotencyKey {
			return copyCreditGrantApplication(cga), nil
		}
	}

	return nil, ierr.NewError("credit grant application not found").
		WithHint("No credit grant application found with the given idempotency key").
		WithReportableDetails(map[string]interface{}{"idempotency_key": idempotencyKey}).
		Mark(ierr.ErrNotFound)
}

// matchesFilter checks if a credit grant application matches the given filter
func (s *InMemoryCreditGrantApplicationStore) matchesFilter(ctx context.Context, cga *creditgrantapplication.CreditGrantApplication, filter *types.CreditGrantApplicationFilter) bool {
	if filter == nil {
		return true
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && cga.TenantID != tenantID {
		return false
	}

	// Apply environment filter
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" && cga.EnvironmentID != environmentID {
		return false
	}

	// Check status filter
	if filter.QueryFilter != nil && filter.QueryFilter.Status != nil {
		if cga.Status != *filter.QueryFilter.Status {
			return false
		}
	}

	// Check application IDs filter
	if len(filter.ApplicationIDs) > 0 {
		if !lo.Contains(filter.ApplicationIDs, cga.ID) {
			return false
		}
	}

	// Check credit grant IDs filter
	if len(filter.CreditGrantIDs) > 0 {
		if !lo.Contains(filter.CreditGrantIDs, cga.CreditGrantID) {
			return false
		}
	}

	// Check subscription IDs filter
	if len(filter.SubscriptionIDs) > 0 {
		if !lo.Contains(filter.SubscriptionIDs, cga.SubscriptionID) {
			return false
		}
	}

	// Check scheduled for filter
	if filter.ScheduledFor != nil {
		if !cga.ScheduledFor.Equal(*filter.ScheduledFor) {
			return false
		}
	}

	// Check applied at filter
	if filter.AppliedAt != nil {
		if cga.AppliedAt == nil || !cga.AppliedAt.Equal(*filter.AppliedAt) {
			return false
		}
	}

	// Check application status filter
	if len(filter.ApplicationStatuses) > 0 {
		if !lo.Contains(filter.ApplicationStatuses, cga.ApplicationStatus) {
			return false
		}
	}

	return true
}

// Clear removes all credit grant applications from the store
func (s *InMemoryCreditGrantApplicationStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applications = make(map[string]*creditgrantapplication.CreditGrantApplication)
}
