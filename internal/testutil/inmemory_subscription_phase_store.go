package testutil

import (
	"context"
	"sort"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemorySubscriptionPhaseStore implements subscription.SubscriptionPhaseRepository
type InMemorySubscriptionPhaseStore struct {
	*InMemoryStore[*subscription.SubscriptionPhase]
	phasesBySubscription map[string][]*subscription.SubscriptionPhase // map[subscriptionID][]phases
}

func NewInMemorySubscriptionPhaseStore() *InMemorySubscriptionPhaseStore {
	return &InMemorySubscriptionPhaseStore{
		InMemoryStore:        NewInMemoryStore[*subscription.SubscriptionPhase](),
		phasesBySubscription: make(map[string][]*subscription.SubscriptionPhase),
	}
}

// subscriptionPhaseFilterFn implements filtering logic for subscription phases
func subscriptionPhaseFilterFn(ctx context.Context, phase *subscription.SubscriptionPhase, filter interface{}) bool {
	if phase == nil {
		return false
	}

	f, ok := filter.(*types.SubscriptionPhaseFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if phase.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, phase.EnvironmentID) {
		return false
	}

	// Filter by subscription IDs
	if len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, phase.SubscriptionID) {
		return false
	}

	// Filter by phase IDs
	if len(f.PhaseIDs) > 0 && !lo.Contains(f.PhaseIDs, phase.ID) {
		return false
	}

	// Filter by active only
	if f.ActiveOnly {
		if !phase.IsActive(time.Now()) {
			return false
		}
	}

	// Filter by active at
	if f.ActiveAt != nil {
		if !phase.IsActive(*f.ActiveAt) {
			return false
		}
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && phase.StartDate.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil {
			if phase.EndDate != nil && phase.EndDate.After(*f.EndTime) {
				return false
			}
		}
	}

	return true
}

// subscriptionPhaseSortFn implements sorting logic for subscription phases
func subscriptionPhaseSortFn(i, j *subscription.SubscriptionPhase) bool {
	if i == nil || j == nil {
		return false
	}
	// Default sort by start_date ascending
	return i.StartDate.Before(j.StartDate)
}

func (s *InMemorySubscriptionPhaseStore) Create(ctx context.Context, phase *subscription.SubscriptionPhase) error {
	if phase == nil {
		return ierr.NewError("subscription phase cannot be nil").
			WithHint("Subscription phase data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if phase.EnvironmentID == "" {
		phase.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, phase.ID, phase)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A subscription phase with this ID already exists").
				WithReportableDetails(map[string]interface{}{
					"phase_id": phase.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create subscription phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": phase.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Add to subscription phases map
	s.phasesBySubscription[phase.SubscriptionID] = append(s.phasesBySubscription[phase.SubscriptionID], phase)
	return nil
}

func (s *InMemorySubscriptionPhaseStore) CreateBulk(ctx context.Context, phases []*subscription.SubscriptionPhase) error {
	for _, phase := range phases {
		if err := s.Create(ctx, phase); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemorySubscriptionPhaseStore) Get(ctx context.Context, id string) (*subscription.SubscriptionPhase, error) {
	phase, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Subscription phase not found").
				WithReportableDetails(map[string]interface{}{
					"phase_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return phase, nil
}

func (s *InMemorySubscriptionPhaseStore) Update(ctx context.Context, phase *subscription.SubscriptionPhase) error {
	if phase == nil {
		return ierr.NewError("subscription phase cannot be nil").
			WithHint("Subscription phase data is required").
			Mark(ierr.ErrValidation)
	}
	err := s.InMemoryStore.Update(ctx, phase.ID, phase)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription phase not found").
				WithReportableDetails(map[string]interface{}{
					"phase_id": phase.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": phase.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Update in subscription phases map
	for i, p := range s.phasesBySubscription[phase.SubscriptionID] {
		if p.ID == phase.ID {
			s.phasesBySubscription[phase.SubscriptionID][i] = phase
			break
		}
	}
	return nil
}

func (s *InMemorySubscriptionPhaseStore) Delete(ctx context.Context, id string) error {
	phase, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	err = s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription phase not found").
				WithReportableDetails(map[string]interface{}{
					"phase_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete subscription phase").
			WithReportableDetails(map[string]interface{}{
				"phase_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Remove from subscription phases map
	if phases, ok := s.phasesBySubscription[phase.SubscriptionID]; ok {
		s.phasesBySubscription[phase.SubscriptionID] = lo.Filter(phases, func(p *subscription.SubscriptionPhase, _ int) bool {
			return p.ID != id
		})
	}
	return nil
}

func (s *InMemorySubscriptionPhaseStore) List(ctx context.Context, filter *types.SubscriptionPhaseFilter) ([]*subscription.SubscriptionPhase, error) {
	phases, err := s.InMemoryStore.List(ctx, filter, subscriptionPhaseFilterFn, subscriptionPhaseSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription phases").
			Mark(ierr.ErrDatabase)
	}

	// Apply custom sorting if specified
	if filter != nil && filter.QueryFilter != nil && filter.QueryFilter.Sort != nil {
		sortField := *filter.QueryFilter.Sort
		order := "asc"
		if filter.QueryFilter.Order != nil {
			order = *filter.QueryFilter.Order
		}

		sort.Slice(phases, func(i, j int) bool {
			var iVal, jVal time.Time
			switch sortField {
			case "start_date":
				iVal = phases[i].StartDate
				jVal = phases[j].StartDate
			case "end_date":
				if phases[i].EndDate != nil {
					iVal = *phases[i].EndDate
				}
				if phases[j].EndDate != nil {
					jVal = *phases[j].EndDate
				}
			case "created_at":
				iVal = phases[i].CreatedAt
				jVal = phases[j].CreatedAt
			default:
				iVal = phases[i].StartDate
				jVal = phases[j].StartDate
			}
			if order == "desc" {
				return iVal.After(jVal)
			}
			return iVal.Before(jVal)
		})
	}

	return phases, nil
}

func (s *InMemorySubscriptionPhaseStore) Count(ctx context.Context, filter *types.SubscriptionPhaseFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, subscriptionPhaseFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count subscription phases").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

// Clear removes all data from the store
func (s *InMemorySubscriptionPhaseStore) Clear() {
	s.InMemoryStore.Clear()
	s.phasesBySubscription = make(map[string][]*subscription.SubscriptionPhase)
}
