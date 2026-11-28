package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/product"
	ierr "github.com/flexprice/flexprice/internal/errors"
	productRepo "github.com/flexprice/flexprice/internal/repository/product"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryProductStore implements productRepo.Repository
type InMemoryProductStore struct {
	*InMemoryStore[*product.Product]
}

func NewInMemoryProductStore() *InMemoryProductStore {
	return &InMemoryProductStore{InMemoryStore: NewInMemoryStore[*product.Product]()}
}

func (s *InMemoryProductStore) Create(ctx context.Context, p *product.Product) error {
	return s.InMemoryStore.Create(ctx, p.ID(), product.NewBuilder(p).Build())
}

func (s *InMemoryProductStore) GetByID(ctx context.Context, id string) (*product.Product, error) {
	p, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("product not found").
			WithHintf("Product with ID %s was not found", id).
			Mark(ierr.ErrNotFound)
	}
	return product.NewBuilder(p).Build(), nil
}

func (s *InMemoryProductStore) Update(ctx context.Context, p *product.Product) error {
	return s.InMemoryStore.Update(ctx, p.ID(), product.NewBuilder(p).Build())
}

func (s *InMemoryProductStore) Delete(ctx context.Context, id string) error {
	p, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}
	archived := product.NewBuilder(p).
		WithStatus(types.StatusArchived).
		WithUpdatedAt(time.Now().UTC()).
		Build()
	return s.InMemoryStore.Update(ctx, id, archived)
}

func (s *InMemoryProductStore) List(ctx context.Context, filter *productRepo.Filter) ([]*product.Product, error) {
	if filter == nil {
		filter = productRepo.NewFilter()
	}
	return s.InMemoryStore.List(ctx, filter, productFilterFn, nil)
}

func (s *InMemoryProductStore) Count(ctx context.Context, filter *productRepo.Filter) (int, error) {
	if filter == nil {
		filter = productRepo.NewFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, productFilterFn)
}

func productFilterFn(_ context.Context, p *product.Product, filter interface{}) bool {
	f, ok := filter.(*productRepo.Filter)
	if !ok {
		return true
	}
	status := lo.Ternary(f.GetStatus() == "", types.StatusPublished, types.Status(f.GetStatus()))
	if p.Status() != status {
		return false
	}
	if len(f.ProductIDs) > 0 && !lo.Contains(f.ProductIDs, p.ID()) {
		return false
	}
	if len(f.LookupKeys) > 0 && !lo.Contains(f.LookupKeys, p.LookupKey()) {
		return false
	}
	return true
}
