package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/connectionassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryConnectionAssociationStore implements connectionassociation.Repository
type InMemoryConnectionAssociationStore struct {
	*InMemoryStore[*connectionassociation.ConnectionAssociation]
}

func NewInMemoryConnectionAssociationStore() *InMemoryConnectionAssociationStore {
	return &InMemoryConnectionAssociationStore{
		InMemoryStore: NewInMemoryStore[*connectionassociation.ConnectionAssociation](),
	}
}

func copyConnectionAssociation(a *connectionassociation.ConnectionAssociation) *connectionassociation.ConnectionAssociation {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

func (s *InMemoryConnectionAssociationStore) Create(ctx context.Context, a *connectionassociation.ConnectionAssociation) error {
	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if err := s.checkUnique(ctx, a); err != nil {
		return err
	}
	return s.InMemoryStore.Create(ctx, a.ID, copyConnectionAssociation(a))
}

func (s *InMemoryConnectionAssociationStore) Get(ctx context.Context, id string) (*connectionassociation.ConnectionAssociation, error) {
	a, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !CheckTenantFilter(ctx, a.TenantID) || !CheckEnvironmentFilter(ctx, a.EnvironmentID) {
		return nil, ierr.NewErrorf("connection association %s not found", id).Mark(ierr.ErrNotFound)
	}
	return copyConnectionAssociation(a), nil
}

func (s *InMemoryConnectionAssociationStore) List(ctx context.Context, filter *types.ConnectionAssociationFilter) ([]*connectionassociation.ConnectionAssociation, error) {
	items, err := s.InMemoryStore.List(ctx, filter, connectionAssociationFilterFn, nil)
	if err != nil {
		return nil, err
	}
	return lo.Map(items, func(a *connectionassociation.ConnectionAssociation, _ int) *connectionassociation.ConnectionAssociation {
		return copyConnectionAssociation(a)
	}), nil
}

func (s *InMemoryConnectionAssociationStore) Delete(ctx context.Context, a *connectionassociation.ConnectionAssociation) error {
	stored, err := s.Get(ctx, a.ID)
	if err != nil {
		return err
	}
	stored.Status = types.StatusArchived
	stored.UpdatedAt = a.UpdatedAt
	stored.UpdatedBy = a.UpdatedBy
	return s.InMemoryStore.Update(ctx, a.ID, stored)
}

// checkUnique mirrors the database's partial unique indexes on published associations.
func (s *InMemoryConnectionAssociationStore) checkUnique(ctx context.Context, a *connectionassociation.ConnectionAssociation) error {
	filter := types.NewNoLimitConnectionAssociationFilter()
	filter.EntityType = a.EntityType
	published, err := s.List(ctx, filter)
	if err != nil {
		return err
	}
	for _, other := range published {
		switch {
		case a.EntityType == types.ConnectionAssociationEntityTypeBillingEntity && other.ConnectionID == a.ConnectionID:
			return ierr.NewError("This connection already belongs to a billing entity").Mark(ierr.ErrAlreadyExists)
		case a.EntityType == types.ConnectionAssociationEntityTypeBillingEntity && other.EntityID == a.EntityID && other.ProviderType == a.ProviderType:
			return ierr.NewError("This billing entity already has a connection for this provider").Mark(ierr.ErrAlreadyExists)
		case a.EntityType == types.ConnectionAssociationEntityTypeCustomer && other.EntityID == a.EntityID:
			return ierr.NewError("This customer already has a preferred connection").Mark(ierr.ErrAlreadyExists)
		}
	}
	return nil
}

func connectionAssociationFilterFn(ctx context.Context, a *connectionassociation.ConnectionAssociation, filter interface{}) bool {
	if !CheckTenantFilter(ctx, a.TenantID) || !CheckEnvironmentFilter(ctx, a.EnvironmentID) {
		return false
	}
	f, ok := filter.(*types.ConnectionAssociationFilter)
	if !ok || f == nil {
		return a.Status == types.StatusPublished
	}
	status := types.Status(f.GetStatus())
	if status == "" {
		status = types.StatusPublished
	}
	if a.Status != status {
		return false
	}
	if f.EntityType != "" && a.EntityType != f.EntityType {
		return false
	}
	if len(f.EntityIDs) > 0 && !lo.Contains(f.EntityIDs, a.EntityID) {
		return false
	}
	if len(f.ConnectionIDs) > 0 && !lo.Contains(f.ConnectionIDs, a.ConnectionID) {
		return false
	}
	if f.ProviderType != "" && a.ProviderType != f.ProviderType {
		return false
	}
	return true
}
