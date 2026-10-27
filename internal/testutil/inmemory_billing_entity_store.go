package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryBillingEntityStore implements billingentity.Repository
type InMemoryBillingEntityStore struct {
	*InMemoryStore[*billingentity.BillingEntity]
}

func NewInMemoryBillingEntityStore() *InMemoryBillingEntityStore {
	return &InMemoryBillingEntityStore{
		InMemoryStore: NewInMemoryStore[*billingentity.BillingEntity](),
	}
}

func copyBillingEntity(e *billingentity.BillingEntity) *billingentity.BillingEntity {
	if e == nil {
		return nil
	}
	cp := *e
	cp.TaxIdentifiers = append([]types.TaxIdentifier(nil), e.TaxIdentifiers...)
	cp.Metadata = lo.Assign(map[string]string{}, e.Metadata)
	return &cp
}

func (s *InMemoryBillingEntityStore) Create(ctx context.Context, e *billingentity.BillingEntity) error {
	if e.EnvironmentID == "" {
		e.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	if err := s.checkUnique(ctx, e); err != nil {
		return err
	}
	return s.InMemoryStore.Create(ctx, e.ID, copyBillingEntity(e))
}

func (s *InMemoryBillingEntityStore) Get(ctx context.Context, id string) (*billingentity.BillingEntity, error) {
	e, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !billingEntityInScope(ctx, e) {
		return nil, ierr.NewErrorf("billing entity %s not found", id).Mark(ierr.ErrNotFound)
	}
	return copyBillingEntity(e), nil
}

func (s *InMemoryBillingEntityStore) GetDefault(ctx context.Context) (*billingentity.BillingEntity, error) {
	items, err := s.List(ctx, types.NewNoLimitBillingEntityFilter())
	if err != nil {
		return nil, err
	}
	for _, e := range items {
		if e.IsDefault {
			return e, nil
		}
	}
	return nil, ierr.NewError("this environment has no default billing entity").Mark(ierr.ErrNotFound)
}

func (s *InMemoryBillingEntityStore) List(ctx context.Context, filter *types.BillingEntityFilter) ([]*billingentity.BillingEntity, error) {
	items, err := s.InMemoryStore.List(ctx, filter, billingEntityFilterFn, nil)
	if err != nil {
		return nil, err
	}
	return lo.Map(items, func(e *billingentity.BillingEntity, _ int) *billingentity.BillingEntity {
		return copyBillingEntity(e)
	}), nil
}

func (s *InMemoryBillingEntityStore) Count(ctx context.Context, filter *types.BillingEntityFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, billingEntityFilterFn)
}

func (s *InMemoryBillingEntityStore) ListByIDs(ctx context.Context, ids []string) ([]*billingentity.BillingEntity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	filter := types.NewNoLimitBillingEntityFilter()
	filter.BillingEntityIDs = ids
	return s.List(ctx, filter)
}

func (s *InMemoryBillingEntityStore) Update(ctx context.Context, e *billingentity.BillingEntity) error {
	if err := s.checkUnique(ctx, e); err != nil {
		return err
	}
	return s.InMemoryStore.Update(ctx, e.ID, copyBillingEntity(e))
}

// checkUnique mirrors the database's partial unique indexes on default and prefix.
func (s *InMemoryBillingEntityStore) checkUnique(ctx context.Context, e *billingentity.BillingEntity) error {
	items, err := s.List(ctx, types.NewNoLimitBillingEntityFilter())
	if err != nil {
		return err
	}
	for _, other := range items {
		if other.ID == e.ID {
			continue
		}
		if other.InvoiceNumberPrefix == e.InvoiceNumberPrefix {
			return ierr.NewErrorf("invoice prefix %s is already used", e.InvoiceNumberPrefix).Mark(ierr.ErrAlreadyExists)
		}
		if other.IsDefault && e.IsDefault {
			return ierr.NewError("default billing entity already exists").Mark(ierr.ErrAlreadyExists)
		}
	}
	return nil
}

func billingEntityInScope(ctx context.Context, e *billingentity.BillingEntity) bool {
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && e.TenantID != tenantID {
		return false
	}
	return CheckEnvironmentFilter(ctx, e.EnvironmentID) && e.Status == types.StatusPublished
}

func billingEntityFilterFn(ctx context.Context, e *billingentity.BillingEntity, filter interface{}) bool {
	if !billingEntityInScope(ctx, e) {
		return false
	}
	f, ok := filter.(*types.BillingEntityFilter)
	if !ok || f == nil {
		return true
	}
	if len(f.BillingEntityIDs) > 0 && !lo.Contains(f.BillingEntityIDs, e.ID) {
		return false
	}
	if f.IsDefault != nil && e.IsDefault != *f.IsDefault {
		return false
	}
	return true
}
