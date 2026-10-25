package testutil

import (
	"context"

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

func (s *InMemoryContractStore) CreateWithAssociation(ctx context.Context, c *contract.Contract, a *contract.Association) error {
	a = contract.NewAssociationBuilder(a).WithContractID(c.ID()).Build()
	if err := s.associations.Create(ctx, associationKey(a.EntityType(), a.EntityID()), a); err != nil {
		return err
	}
	return s.InMemoryStore.Create(ctx, c.ID(), contract.NewBuilder(c).Build())
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

func (s *InMemoryContractStore) Update(ctx context.Context, c *contract.Contract) error {
	return s.InMemoryStore.Update(ctx, c.ID(), contract.NewBuilder(c).Build())
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
