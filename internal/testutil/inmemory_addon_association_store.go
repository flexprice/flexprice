package testutil

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryAddonAssociationStore implements addonassociation.Repository
type InMemoryAddonAssociationStore struct {
	*InMemoryStore[*addonassociation.AddonAssociation]
}

// NewInMemoryAddonAssociationStore creates a new in-memory addon association store
func NewInMemoryAddonAssociationStore() *InMemoryAddonAssociationStore {
	return &InMemoryAddonAssociationStore{
		InMemoryStore: NewInMemoryStore[*addonassociation.AddonAssociation](),
	}
}

// Helper to copy addon association
func copyAddonAssociation(aa *addonassociation.AddonAssociation) *addonassociation.AddonAssociation {
	if aa == nil {
		return nil
	}

	// Deep copy of addon association
	copied := &addonassociation.AddonAssociation{
		ID:                 aa.ID,
		EnvironmentID:      aa.EnvironmentID,
		EntityID:           aa.EntityID,
		EntityType:         aa.EntityType,
		AddonID:            aa.AddonID,
		StartDate:          aa.StartDate,
		EndDate:            aa.EndDate,
		AddonStatus:        aa.AddonStatus,
		CancellationReason: aa.CancellationReason,
		CancelledAt:        aa.CancelledAt,
		Metadata:           lo.Assign(map[string]interface{}{}, aa.Metadata),
		BaseModel: types.BaseModel{
			TenantID:  aa.TenantID,
			Status:    aa.Status,
			CreatedAt: aa.CreatedAt,
			UpdatedAt: aa.UpdatedAt,
			CreatedBy: aa.CreatedBy,
			UpdatedBy: aa.UpdatedBy,
		},
	}

	return copied
}

func (s *InMemoryAddonAssociationStore) Create(ctx context.Context, aa *addonassociation.AddonAssociation) error {
	if aa == nil {
		return ierr.NewError("addon association cannot be nil").
			WithHint("Addon association cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if aa.EnvironmentID == "" {
		aa.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, aa.ID, copyAddonAssociation(aa))
}

func (s *InMemoryAddonAssociationStore) GetByID(ctx context.Context, id string) (*addonassociation.AddonAssociation, error) {
	aa, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("addon association not found").
			WithHint("Addon association not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyAddonAssociation(aa), nil
}

func (s *InMemoryAddonAssociationStore) Update(ctx context.Context, aa *addonassociation.AddonAssociation) error {
	if aa == nil {
		return ierr.NewError("addon association cannot be nil").
			WithHint("Addon association cannot be nil").
			Mark(ierr.ErrValidation)
	}

	return s.InMemoryStore.Update(ctx, aa.ID, copyAddonAssociation(aa))
}

// Delete mirrors the ent repository, which SOFT deletes by setting status = archived and
// leaving addon_status untouched (repository/ent/addonassociation.go). Removing the row here
// instead would make an archived association indistinguishable from one that never existed,
// so rollback and cleanup paths could not be asserted.
func (s *InMemoryAddonAssociationStore) Delete(ctx context.Context, id string) error {
	assoc, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return err
	}

	assoc.Status = types.StatusArchived
	return s.InMemoryStore.Update(ctx, id, assoc)
}

func (s *InMemoryAddonAssociationStore) CreateBulk(ctx context.Context, associations []*addonassociation.AddonAssociation) error {
	for _, aa := range associations {
		if err := s.Create(ctx, aa); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryAddonAssociationStore) CancelBulk(ctx context.Context, ids []string, effectiveAt time.Time, reason string) error {
	return s.mutateEach(ctx, ids, func(aa *addonassociation.AddonAssociation) {
		aa.EndDate = lo.ToPtr(effectiveAt)
		aa.CancelledAt = lo.ToPtr(effectiveAt)
		aa.AddonStatus = types.AddonStatusCancelled
		if reason != "" {
			aa.CancellationReason = reason
		}
	})
}

func (s *InMemoryAddonAssociationStore) ActivateBulk(ctx context.Context, ids []string) error {
	return s.mutateEach(ctx, ids, func(aa *addonassociation.AddonAssociation) {
		aa.AddonStatus = types.AddonStatusActive
	})
}

func (s *InMemoryAddonAssociationStore) DeleteBulk(ctx context.Context, ids []string) error {
	return s.mutateEach(ctx, ids, func(aa *addonassociation.AddonAssociation) {
		aa.Status = types.StatusArchived
	})
}

// mutateEach skips unknown ids, matching the ent repositories' predicate updates.
func (s *InMemoryAddonAssociationStore) mutateEach(
	ctx context.Context,
	ids []string,
	mutate func(*addonassociation.AddonAssociation),
) error {
	for _, id := range ids {
		aa, err := s.InMemoryStore.Get(ctx, id)
		if err != nil {
			continue
		}

		updated := copyAddonAssociation(aa)
		mutate(updated)
		if err := s.InMemoryStore.Update(ctx, id, updated); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryAddonAssociationStore) GetByIDs(ctx context.Context, ids []string) ([]*addonassociation.AddonAssociation, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	filter := types.NewNoLimitAddonAssociationFilter()
	filter.Status = nil
	filter.AssociationIDs = ids

	return s.List(ctx, filter)
}

func (s *InMemoryAddonAssociationStore) List(ctx context.Context, filter *types.AddonAssociationFilter) ([]*addonassociation.AddonAssociation, error) {
	if filter == nil {
		filter = types.NewAddonAssociationFilter()
	}

	return s.InMemoryStore.List(ctx, filter, addonAssociationFilterFn, addonAssociationSortFn)
}

func (s *InMemoryAddonAssociationStore) Count(ctx context.Context, filter *types.AddonAssociationFilter) (int, error) {
	if filter == nil {
		filter = types.NewAddonAssociationFilter()
	}

	return s.InMemoryStore.Count(ctx, filter, addonAssociationFilterFn)
}

func addonAssociationFilterFn(ctx context.Context, aa *addonassociation.AddonAssociation, filter interface{}) bool {
	f, ok := filter.(*types.AddonAssociationFilter)
	if !ok {
		return true
	}

	// Check tenant and environment
	if aa.TenantID != types.GetTenantID(ctx) {
		return false
	}

	if !CheckEnvironmentFilter(ctx, aa.EnvironmentID) {
		return false
	}

	// Apply filter conditions
	for _, condition := range f.Filters {
		if !applyAddonAssociationFilterCondition(aa, condition) {
			return false
		}
	}

	// Check specific filters
	if len(f.AssociationIDs) > 0 {
		if !lo.Contains(f.AssociationIDs, aa.ID) {
			return false
		}
	}

	if len(f.AddonIDs) > 0 {
		if !lo.Contains(f.AddonIDs, aa.AddonID) {
			return false
		}
	}

	if f.EntityType != nil {
		if aa.EntityType != *f.EntityType {
			return false
		}
	}

	if len(f.EntityIDs) > 0 {
		if !lo.Contains(f.EntityIDs, aa.EntityID) {
			return false
		}
	}

	if f.AddonStatus != nil {
		if aa.AddonStatus != types.AddonStatus(*f.AddonStatus) {
			return false
		}
	}

	if len(f.AddonStatuses) > 0 {
		if !lo.Contains(f.AddonStatuses, string(aa.AddonStatus)) {
			return false
		}
	}

	// Mirrors the ent predicate: start_date <= t AND (end_date IS NULL OR end_date > t).
	if f.ActiveAt != nil {
		if aa.StartDate == nil || aa.StartDate.After(*f.ActiveAt) {
			return false
		}
		if aa.EndDate != nil && !aa.EndDate.After(*f.ActiveAt) {
			return false
		}
	}

	if f.StartDate != nil || f.EndDate != nil {
		// Ensure association is published and active for the given time window
		if aa.Status != types.StatusPublished {
			return false
		}
		allowedStatuses := f.AddonStatuses
		if len(allowedStatuses) == 0 {
			allowedStatuses = []string{string(types.AddonStatusActive)}
		}
		if !lo.Contains(allowedStatuses, string(aa.AddonStatus)) {
			return false
		}

		start := f.StartDate
		end := f.EndDate

		// Derive bounds similar to repository logic
		if start == nil && end != nil {
			start = end
		}
		if end == nil && start != nil {
			end = start
		}

		// If still nil (shouldn't happen), allow
		if start != nil && end != nil {
			// Overlap: starts on/before end AND (no end date or end after start)
			if aa.StartDate != nil && aa.StartDate.After(*end) {
				return false
			}
			if aa.EndDate != nil && !aa.EndDate.After(*start) {
				return false
			}
		}
	}

	return true
}

func applyAddonAssociationFilterCondition(aa *addonassociation.AddonAssociation, condition *types.FilterCondition) bool {
	if condition.Field == nil {
		return true
	}

	switch *condition.Field {
	case "id":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(aa.ID), strings.ToLower(*condition.Value.String))
		}
	case "entity_id":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(aa.EntityID), strings.ToLower(*condition.Value.String))
		}
	case "entity_type":
		if condition.Value != nil && condition.Value.String != nil {
			return string(aa.EntityType) == *condition.Value.String
		}
	case "addon_id":
		if condition.Value != nil && condition.Value.String != nil {
			return strings.Contains(strings.ToLower(aa.AddonID), strings.ToLower(*condition.Value.String))
		}
	case "addon_status":
		if condition.Value != nil && condition.Value.String != nil {
			return aa.AddonStatus == types.AddonStatus(*condition.Value.String)
		}
	case "environment_id":
		if condition.Value != nil && condition.Value.String != nil {
			return aa.EnvironmentID == *condition.Value.String
		}
	default:
		return true
	}

	return true
}

func addonAssociationSortFn(i, j *addonassociation.AddonAssociation) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}
