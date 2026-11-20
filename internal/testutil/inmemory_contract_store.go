package testutil

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryContractStore implements contractRepo.Repository
type InMemoryContractStore struct {
	*InMemoryStore[*contract.Contract]
	// associations are keyed by entity type and id, mirroring the published-unique index.
	associations *InMemoryStore[*contract.Association]
	mu           sync.Mutex
}

func NewInMemoryContractStore() *InMemoryContractStore {
	return &InMemoryContractStore{
		InMemoryStore: NewInMemoryStore[*contract.Contract](),
		associations:  NewInMemoryStore[*contract.Association](),
	}
}

func associationKey(entityType types.RevRecEntityType, entityID string) string {
	return string(entityType) + ":" + entityID
}

func (s *InMemoryContractStore) Create(ctx context.Context, c *contract.Contract) error {
	return s.InMemoryStore.Create(ctx, c.ID(), contract.NewBuilder(c).Build())
}

func (s *InMemoryContractStore) CreateAssociation(ctx context.Context, a *contract.Association) error {
	if err := s.associations.Create(ctx, associationKey(a.EntityType(), a.EntityID()), contract.NewAssociationBuilder(a).Build()); err != nil {
		return ierr.WithError(err).WithHint("Entity already belongs to a contract").Mark(ierr.ErrAlreadyExists)
	}
	return nil
}

// CreateWithAssociation seeds a contract linked to an entity, for tests.
func (s *InMemoryContractStore) CreateWithAssociation(ctx context.Context, c *contract.Contract, a *contract.Association) error {
	if err := s.CreateAssociation(ctx, contract.NewAssociationBuilder(a).WithContractID(c.ID()).Build()); err != nil {
		return err
	}
	return s.Create(ctx, c)
}

func (s *InMemoryContractStore) GetByID(ctx context.Context, id string) (*contract.Contract, error) {
	c, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("contract not found").
			WithHintf("Contract with ID %s was not found", id).
			Mark(ierr.ErrNotFound)
	}
	return contract.NewBuilder(c).Build(), nil
}

func (s *InMemoryContractStore) GetBySubscriptionID(ctx context.Context, subscriptionID string) (*contract.Contract, error) {
	a, err := s.associations.Get(ctx, associationKey(types.RevRecEntityTypeSubscription, subscriptionID))
	if err != nil {
		return nil, ierr.NewError("contract not found").
			WithHintf("Contract with subscription_id %s was not found", subscriptionID).
			Mark(ierr.ErrNotFound)
	}
	return s.GetByID(ctx, a.ContractID())
}

func (s *InMemoryContractStore) ListAssociations(ctx context.Context, contractID string) ([]*contract.Association, error) {
	return s.associations.List(ctx, nil, func(_ context.Context, a *contract.Association, _ interface{}) bool {
		return a.ContractID() == contractID && a.Status() == types.StatusPublished
	}, nil)
}

// Update keeps the stored review, as the ent repository does.
func (s *InMemoryContractStore) Update(ctx context.Context, c *contract.Contract) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.InMemoryStore.Get(ctx, c.ID())
	if err != nil {
		return err
	}
	return s.InMemoryStore.Update(ctx, c.ID(), contract.NewBuilder(c).
		WithReview(stored.Review()).
		WithReviewVersion(stored.ReviewVersion()).
		Build())
}

func (s *InMemoryContractStore) UpdateReview(ctx context.Context, id string, review json.RawMessage, version int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.GetByID(ctx, id)
	if err != nil {
		return 0, err
	}
	if stored.ReviewVersion() != version {
		return 0, ierr.NewError("review version conflict").
			WithHint("The review was changed by someone else. Reload it and try again").
			Mark(ierr.ErrVersionConflict)
	}
	next := contract.NewBuilder(stored).WithReview(review).WithReviewVersion(version + 1).Build()
	if err := s.InMemoryStore.Update(ctx, id, next); err != nil {
		return 0, err
	}
	return version + 1, nil
}

func (s *InMemoryContractStore) List(ctx context.Context, filter *contractRepo.Filter) ([]*contract.Contract, error) {
	if filter == nil {
		filter = contractRepo.NewFilter()
	}
	return s.InMemoryStore.List(ctx, filter, s.filterFn, nil)
}

func (s *InMemoryContractStore) Count(ctx context.Context, filter *contractRepo.Filter) (int, error) {
	if filter == nil {
		filter = contractRepo.NewFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, s.filterFn)
}

func (s *InMemoryContractStore) filterFn(ctx context.Context, c *contract.Contract, filter interface{}) bool {
	f, ok := filter.(*contractRepo.Filter)
	if !ok {
		return true
	}
	status := lo.Ternary(f.GetStatus() == "", types.StatusPublished, types.Status(f.GetStatus()))
	if c.Status() != status {
		return false
	}
	if len(f.ContractIDs) > 0 && !lo.Contains(f.ContractIDs, c.ID()) {
		return false
	}
	if len(f.CustomerIDs) > 0 && !lo.Contains(f.CustomerIDs, c.CustomerID()) {
		return false
	}
	if len(f.ContractStatuses) > 0 && !lo.Contains(f.ContractStatuses, c.ContractStatus()) {
		return false
	}
	if f.EndedAtGTE != nil && (c.EndedAt() == nil || c.EndedAt().Before(*f.EndedAtGTE)) {
		return false
	}
	if len(f.SubscriptionIDs) > 0 {
		return lo.ContainsBy(f.SubscriptionIDs, func(subID string) bool {
			a, err := s.associations.Get(ctx, associationKey(types.RevRecEntityTypeSubscription, subID))
			return err == nil && a.ContractID() == c.ID()
		})
	}
	return true
}
