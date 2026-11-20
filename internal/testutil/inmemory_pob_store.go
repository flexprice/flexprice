package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/pob"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	pobRepo "github.com/flexprice/flexprice/internal/repository/pob"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryPOBStore implements pobRepo.Repository
type InMemoryPOBStore struct {
	*InMemoryStore[*pob.PerformanceObligation]
	// contracts resolves the customer filter.
	contracts contractRepo.Repository
}

func NewInMemoryPOBStore(contracts contractRepo.Repository) *InMemoryPOBStore {
	return &InMemoryPOBStore{
		InMemoryStore: NewInMemoryStore[*pob.PerformanceObligation](),
		contracts:     contracts,
	}
}

func (s *InMemoryPOBStore) Create(ctx context.Context, p *pob.PerformanceObligation) error {
	return s.InMemoryStore.Create(ctx, p.ID(), pob.NewBuilder(p).Build())
}

func (s *InMemoryPOBStore) GetByID(ctx context.Context, id string) (*pob.PerformanceObligation, error) {
	p, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("performance obligation not found").
			WithHintf("Performance obligation with ID %s was not found", id).
			Mark(ierr.ErrNotFound)
	}
	return pob.NewBuilder(p).Build(), nil
}

func (s *InMemoryPOBStore) Update(ctx context.Context, p *pob.PerformanceObligation) error {
	return s.InMemoryStore.Update(ctx, p.ID(), pob.NewBuilder(p).Build())
}

func (s *InMemoryPOBStore) List(ctx context.Context, filter *pobRepo.Filter) ([]*pob.PerformanceObligation, error) {
	if filter == nil {
		filter = pobRepo.NewFilter()
	}
	return s.InMemoryStore.List(ctx, filter, s.filterFn, nil)
}

func (s *InMemoryPOBStore) Count(ctx context.Context, filter *pobRepo.Filter) (int, error) {
	if filter == nil {
		filter = pobRepo.NewFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, s.filterFn)
}

func (s *InMemoryPOBStore) filterFn(ctx context.Context, p *pob.PerformanceObligation, filter interface{}) bool {
	f, ok := filter.(*pobRepo.Filter)
	if !ok {
		return true
	}
	status := lo.Ternary(f.GetStatus() == "", types.StatusPublished, types.Status(f.GetStatus()))
	if p.Status() != status {
		return false
	}
	if len(f.PerformanceObligationIDs) > 0 && !lo.Contains(f.PerformanceObligationIDs, p.ID()) {
		return false
	}
	if len(f.ContractIDs) > 0 && !lo.Contains(f.ContractIDs, p.ContractID()) {
		return false
	}
	if len(f.POBStatuses) > 0 && !lo.Contains(f.POBStatuses, p.POBStatus()) {
		return false
	}
	if len(f.RecognitionMethods) > 0 && !lo.Contains(f.RecognitionMethods, p.RecognitionMethod()) {
		return false
	}
	if len(f.CustomerIDs) > 0 {
		c, err := s.contracts.GetByID(ctx, p.ContractID())
		return err == nil && lo.Contains(f.CustomerIDs, c.CustomerID())
	}
	return true
}
