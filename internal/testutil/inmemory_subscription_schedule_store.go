package testutil

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemorySubscriptionScheduleStore implements subscription.SubscriptionScheduleRepository
type InMemorySubscriptionScheduleStore struct {
	*InMemoryStore[*subscription.SubscriptionSchedule]
	mu                      sync.RWMutex
	schedulesBySubscription map[string][]*subscription.SubscriptionSchedule // map[subscriptionID][]schedules
}

func NewInMemorySubscriptionScheduleStore() *InMemorySubscriptionScheduleStore {
	return &InMemorySubscriptionScheduleStore{
		InMemoryStore:           NewInMemoryStore[*subscription.SubscriptionSchedule](),
		schedulesBySubscription: make(map[string][]*subscription.SubscriptionSchedule),
	}
}

// Create creates a new schedule
func (s *InMemorySubscriptionScheduleStore) Create(ctx context.Context, schedule *subscription.SubscriptionSchedule) error {
	if schedule == nil {
		return ierr.NewError("schedule is nil").Mark(ierr.ErrValidation)
	}
	if err := s.InMemoryStore.Create(ctx, schedule.ID, schedule); err != nil {
		return err
	}
	// Update index
	s.mu.Lock()
	s.schedulesBySubscription[schedule.SubscriptionID] = append(s.schedulesBySubscription[schedule.SubscriptionID], schedule)
	s.mu.Unlock()
	return nil
}

// Get retrieves a schedule by ID
func (s *InMemorySubscriptionScheduleStore) Get(ctx context.Context, id string) (*subscription.SubscriptionSchedule, error) {
	return s.InMemoryStore.Get(ctx, id)
}

// Update updates an existing schedule
func (s *InMemorySubscriptionScheduleStore) Update(ctx context.Context, schedule *subscription.SubscriptionSchedule) error {
	if schedule == nil {
		return ierr.NewError("schedule is nil").Mark(ierr.ErrValidation)
	}
	// Get old schedule to update index
	oldSchedule, err := s.Get(ctx, schedule.ID)
	if err != nil {
		return err
	}
	if err := s.InMemoryStore.Update(ctx, schedule.ID, schedule); err != nil {
		return err
	}
	// Update index if subscription ID changed
	s.mu.Lock()
	if oldSchedule.SubscriptionID != schedule.SubscriptionID {
		// Remove from old subscription's list
		schedules := s.schedulesBySubscription[oldSchedule.SubscriptionID]
		for i, sched := range schedules {
			if sched.ID == schedule.ID {
				s.schedulesBySubscription[oldSchedule.SubscriptionID] = append(schedules[:i], schedules[i+1:]...)
				break
			}
		}
		// Add to new subscription's list
		s.schedulesBySubscription[schedule.SubscriptionID] = append(s.schedulesBySubscription[schedule.SubscriptionID], schedule)
	}
	s.mu.Unlock()
	return nil
}

// Delete soft deletes a schedule
func (s *InMemorySubscriptionScheduleStore) Delete(ctx context.Context, id string) error {
	schedule, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.InMemoryStore.Delete(ctx, id); err != nil {
		return err
	}
	// Remove from index
	s.mu.Lock()
	schedules := s.schedulesBySubscription[schedule.SubscriptionID]
	for i, sched := range schedules {
		if sched.ID == id {
			s.schedulesBySubscription[schedule.SubscriptionID] = append(schedules[:i], schedules[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	return nil
}

// GetBySubscriptionID retrieves all schedules for a subscription
func (s *InMemorySubscriptionScheduleStore) GetBySubscriptionID(ctx context.Context, subscriptionID string) ([]*subscription.SubscriptionSchedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	schedules, exists := s.schedulesBySubscription[subscriptionID]
	if !exists {
		return []*subscription.SubscriptionSchedule{}, nil
	}
	// Return a copy to avoid external modification
	result := make([]*subscription.SubscriptionSchedule, len(schedules))
	copy(result, schedules)
	return result, nil
}

// GetPendingBySubscriptionAndType retrieves a pending schedule of a specific type for a subscription
func (s *InMemorySubscriptionScheduleStore) GetPendingBySubscriptionAndType(ctx context.Context, subscriptionID string, scheduleType types.SubscriptionScheduleChangeType) (*subscription.SubscriptionSchedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	schedules, exists := s.schedulesBySubscription[subscriptionID]
	if !exists {
		return nil, ierr.NewError("subscription schedule not found").Mark(ierr.ErrNotFound)
	}

	for _, schedule := range schedules {
		if schedule.ScheduleType == scheduleType &&
			schedule.Status == types.ScheduleStatusPending {
			return schedule, nil
		}
	}

	return nil, ierr.NewError("subscription schedule not found").Mark(ierr.ErrNotFound)
}

// List retrieves schedules with filters
func (s *InMemorySubscriptionScheduleStore) List(ctx context.Context, filter *types.SubscriptionScheduleFilter) ([]*subscription.SubscriptionSchedule, error) {
	s.mu.RLock()
	var allSchedules []*subscription.SubscriptionSchedule
	for _, schedules := range s.schedulesBySubscription {
		allSchedules = append(allSchedules, schedules...)
	}
	s.mu.RUnlock()

	var results []*subscription.SubscriptionSchedule
	for _, schedule := range allSchedules {
		if subscriptionScheduleFilterFn(ctx, schedule, filter) {
			results = append(results, schedule)
		}
	}

	// Apply pagination
	if filter != nil && filter.QueryFilter != nil {
		offset := 0
		if filter.QueryFilter.Offset != nil {
			offset = *filter.QueryFilter.Offset
		}
		limit := 0
		if filter.QueryFilter.Limit != nil {
			limit = *filter.QueryFilter.Limit
		}
		if limit > 0 {
			if offset >= len(results) {
				return []*subscription.SubscriptionSchedule{}, nil
			}
			end := offset + limit
			if end > len(results) {
				end = len(results)
			}
			results = results[offset:end]
		}
	}

	return results, nil
}

// Count returns the count of schedules matching the filter
func (s *InMemorySubscriptionScheduleStore) Count(ctx context.Context, filter *types.SubscriptionScheduleFilter) (int, error) {
	s.mu.RLock()
	var allSchedules []*subscription.SubscriptionSchedule
	for _, schedules := range s.schedulesBySubscription {
		allSchedules = append(allSchedules, schedules...)
	}
	s.mu.RUnlock()

	count := 0
	for _, schedule := range allSchedules {
		if subscriptionScheduleFilterFn(ctx, schedule, filter) {
			count++
		}
	}

	return count, nil
}

// subscriptionScheduleFilterFn implements filtering logic for subscription schedules
func subscriptionScheduleFilterFn(ctx context.Context, schedule *subscription.SubscriptionSchedule, filter interface{}) bool {
	if schedule == nil {
		return false
	}

	f, ok := filter.(*types.SubscriptionScheduleFilter)
	if !ok || f == nil {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if schedule.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, schedule.EnvironmentID) {
		return false
	}

	// Filter by subscription IDs
	if len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, schedule.SubscriptionID) {
		return false
	}

	// Filter by schedule types
	if len(f.ScheduleType) > 0 && !lo.Contains(f.ScheduleType, schedule.ScheduleType) {
		return false
	}

	// Filter by status
	if len(f.ScheduleStatus) > 0 && !lo.Contains(f.ScheduleStatus, schedule.Status) {
		return false
	}

	// Filter by pending only
	if f.PendingOnly && schedule.Status != types.ScheduleStatusPending {
		return false
	}

	return true
}
