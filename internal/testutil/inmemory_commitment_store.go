package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/commitment"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCommitmentStore implements commitment.Repository in memory.
type InMemoryCommitmentStore struct {
	*InMemoryStore[*commitment.Commitment]
}

func NewInMemoryCommitmentStore() *InMemoryCommitmentStore {
	return &InMemoryCommitmentStore{
		InMemoryStore: NewInMemoryStore[*commitment.Commitment](),
	}
}

// commitmentFilterFn mirrors the scoped query and applyCommitmentFilter in the ent repo.
// TODO: generic Filters and Sort conditions are not applied here.
func commitmentFilterFn(ctx context.Context, c *commitment.Commitment, filter interface{}) bool {
	if c == nil || c.Status != types.StatusPublished {
		return false
	}

	if !CheckTenantFilter(ctx, c.TenantID) || !CheckEnvironmentFilter(ctx, c.EnvironmentID) {
		return false
	}

	f, ok := filter.(*commitment.Filter)
	if !ok || f == nil {
		return true
	}

	switch {
	case len(f.IDs) > 0 && !lo.Contains(f.IDs, c.ID),
		len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, c.SubscriptionID),
		len(f.CustomerIDs) > 0 && !lo.Contains(f.CustomerIDs, c.CustomerID),
		len(f.Statuses) > 0 && !lo.Contains(f.Statuses, c.CommitmentStatus),
		f.ScopeType != nil && *f.ScopeType != c.ScopeEntityType,
		len(f.ScopeIDs) > 0 && !lo.Contains(f.ScopeIDs, c.ScopeEntityID),
		f.ActiveFrom != nil && c.EndDate != nil && !c.EndDate.After(*f.ActiveFrom),
		f.ActiveTo != nil && !c.StartDate.Before(*f.ActiveTo):
		return false
	}

	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && c.CreatedAt.Before(*f.StartTime) {
			return false
		}

		if f.EndTime != nil && c.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

func commitmentSortFn(i, j *commitment.Commitment) bool {
	if i.Priority != j.Priority {
		return i.Priority < j.Priority
	}

	if !i.StartDate.Equal(j.StartDate) {
		return i.StartDate.Before(j.StartDate)
	}

	return i.ID < j.ID
}

func (s *InMemoryCommitmentStore) Create(ctx context.Context, c *commitment.Commitment) (*commitment.Commitment, error) {
	if c.IdempotencyKey != nil {
		if existing, err := s.GetByIdempotencyKey(ctx, *c.IdempotencyKey); err == nil && existing != nil {
			return nil, errors.NewError("duplicate idempotency key").
				Mark(errors.ErrAlreadyExists)
		}
	}

	if err := s.InMemoryStore.Create(ctx, c.ID, c); err != nil {
		return nil, err
	}

	return c, nil
}

func (s *InMemoryCommitmentStore) Get(ctx context.Context, id string) (*commitment.Commitment, error) {
	c, err := s.InMemoryStore.Get(ctx, id)
	if err != nil || !commitmentFilterFn(ctx, c, nil) {
		return nil, errors.NewError("commitment not found").
			Mark(errors.ErrNotFound)
	}

	return c, nil
}

func (s *InMemoryCommitmentStore) GetByIdempotencyKey(ctx context.Context, key string) (*commitment.Commitment, error) {
	all, err := s.InMemoryStore.List(ctx, nil, commitmentFilterFn, commitmentSortFn)
	if err != nil {
		return nil, err
	}

	for _, c := range all {
		if c.IdempotencyKey != nil && *c.IdempotencyKey == key {
			return c, nil
		}
	}

	return nil, errors.NewError("commitment not found").
		Mark(errors.ErrNotFound)
}

// Update mirrors the ent repository: it is scoped to the ctx tenant and environment and
// writes only the fields that may change after creation.
func (s *InMemoryCommitmentStore) Update(ctx context.Context, c *commitment.Commitment) (*commitment.Commitment, error) {
	stored, err := s.InMemoryStore.Get(ctx, c.ID)
	if err != nil || !CheckTenantFilter(ctx, stored.TenantID) || !CheckEnvironmentFilter(ctx, stored.EnvironmentID) {
		return nil, errors.NewError("commitment not found").
			Mark(errors.ErrNotFound)
	}

	updated := *stored
	updated.Name = c.Name
	updated.InvoiceLabel = c.InvoiceLabel
	updated.InvoiceDescription = c.InvoiceDescription
	updated.Metadata = c.Metadata
	updated.TerminatedAt = c.TerminatedAt
	updated.UpdatedAt = time.Now().UTC()
	updated.UpdatedBy = types.GetUserID(ctx)

	if c.CommitmentStatus != "" {
		updated.CommitmentStatus = c.CommitmentStatus
	}

	if err := s.InMemoryStore.Update(ctx, c.ID, &updated); err != nil {
		return nil, err
	}

	return &updated, nil
}

// Delete mirrors the ent repository's soft delete: the row stays, archived.
func (s *InMemoryCommitmentStore) Delete(ctx context.Context, id string) error {
	stored, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	archived := *stored
	archived.Status = types.StatusArchived
	archived.UpdatedAt = time.Now().UTC()
	archived.UpdatedBy = types.GetUserID(ctx)
	return s.InMemoryStore.Update(ctx, id, &archived)
}

func (s *InMemoryCommitmentStore) List(ctx context.Context, f *commitment.Filter) ([]*commitment.Commitment, error) {
	if f == nil {
		f = commitment.NewNoLimitFilter()
	}

	return s.InMemoryStore.List(ctx, f, commitmentFilterFn, commitmentSortFn)
}

func (s *InMemoryCommitmentStore) Count(ctx context.Context, f *commitment.Filter) (int, error) {
	if f == nil {
		f = commitment.NewNoLimitFilter()
	}

	return s.InMemoryStore.Count(ctx, f, commitmentFilterFn)
}
