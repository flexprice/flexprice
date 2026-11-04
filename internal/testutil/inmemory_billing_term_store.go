package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryBillingTermStore implements billingterm.TermRepository in memory.
type InMemoryBillingTermStore struct {
	*InMemoryStore[*billingterm.Term]
}

// NewInMemoryBillingTermStore returns an empty in-memory billing term store.
func NewInMemoryBillingTermStore() *InMemoryBillingTermStore {
	return &InMemoryBillingTermStore{
		InMemoryStore: NewInMemoryStore[*billingterm.Term](),
	}
}

// billingTermInScope mirrors the ent repository's scoped query: tenant, environment and published.
func billingTermInScope(ctx context.Context, t *billingterm.Term) bool {
	return t != nil &&
		t.Status == types.StatusPublished &&
		CheckTenantFilter(ctx, t.TenantID) &&
		CheckEnvironmentFilter(ctx, t.EnvironmentID)
}

func billingTermSortFn(i, j *billingterm.Term) bool {
	return i.ID < j.ID
}

func (s *InMemoryBillingTermStore) CreateMany(ctx context.Context, terms []*billingterm.Term) ([]*billingterm.Term, error) {
	created := make([]*billingterm.Term, 0, len(terms))
	for _, t := range terms {
		row := *t
		if row.EnvironmentID == "" {
			row.EnvironmentID = types.GetEnvironmentID(ctx)
		}

		if err := s.InMemoryStore.Create(ctx, row.ID, &row); err != nil {
			return nil, err
		}

		created = append(created, lo.ToPtr(row))
	}

	return created, nil
}

func (s *InMemoryBillingTermStore) List(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string) ([]*billingterm.Term, error) {
	rows, err := s.InMemoryStore.List(ctx, nil, func(ctx context.Context, t *billingterm.Term, _ interface{}) bool {
		return billingTermInScope(ctx, t) && t.OwnerType == ownerType && t.OwnerID == ownerID
	}, billingTermSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(rows, func(t *billingterm.Term, _ int) *billingterm.Term { return lo.ToPtr(*t) }), nil
}

func (s *InMemoryBillingTermStore) Archive(ctx context.Context, ownerType types.BillingTermOwnerType, ownerID string) ([]*billingterm.Term, error) {
	rows, err := s.List(ctx, ownerType, ownerID)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		row.Status = types.StatusArchived
		row.UpdatedAt = clock.Now()
		row.UpdatedBy = types.GetUserID(ctx)

		if err := s.InMemoryStore.Update(ctx, row.ID, lo.ToPtr(*row)); err != nil {
			return nil, err
		}
	}

	return rows, nil
}

// GetLinkedCustomerID always fails with ErrNotFound: the store cannot see the owning commitment.
func (s *InMemoryBillingTermStore) GetLinkedCustomerID(ctx context.Context, id string) (string, error) {
	return "", ierr.NewError("billing term customer is not resolvable in memory").
		WithHintf("Billing term %s has no resolvable customer in the in-memory store", id).
		Mark(ierr.ErrNotFound)
}
