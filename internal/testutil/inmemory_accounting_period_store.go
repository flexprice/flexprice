package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/accountingperiod"
	ierr "github.com/flexprice/flexprice/internal/errors"
	accountingPeriodRepo "github.com/flexprice/flexprice/internal/repository/accountingperiod"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryAccountingPeriodStore implements accountingPeriodRepo.Repository.
// Items are keyed by tenant, environment and period start, mirroring the unique index.
type InMemoryAccountingPeriodStore struct {
	*InMemoryStore[*accountingperiod.AccountingPeriod]
}

func NewInMemoryAccountingPeriodStore() *InMemoryAccountingPeriodStore {
	return &InMemoryAccountingPeriodStore{InMemoryStore: NewInMemoryStore[*accountingperiod.AccountingPeriod]()}
}

func (s *InMemoryAccountingPeriodStore) GetOrCreate(ctx context.Context, periodStart time.Time) (*accountingperiod.AccountingPeriod, error) {
	key := types.GetTenantID(ctx) + ":" + types.GetEnvironmentID(ctx) + ":" + periodStart.Format(time.DateOnly)
	now := time.Now().UTC()
	p := accountingperiod.NewBuilder(nil).
		WithID(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ACCOUNTING_PERIOD)).
		WithPeriodStart(periodStart).
		WithPeriodStatus(types.AccountingPeriodStatusOpen).
		WithStatus(types.StatusPublished).
		WithCreatedAt(now).
		WithUpdatedAt(now).
		Build()
	if err := s.InMemoryStore.Create(ctx, key, p); err != nil && !ierr.IsAlreadyExists(err) {
		return nil, err
	}
	existing, err := s.InMemoryStore.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return accountingperiod.NewBuilder(existing).Build(), nil
}

func (s *InMemoryAccountingPeriodStore) List(ctx context.Context, filter *accountingPeriodRepo.Filter) ([]*accountingperiod.AccountingPeriod, error) {
	if filter == nil {
		filter = accountingPeriodRepo.NewFilter()
	}
	return s.InMemoryStore.List(ctx, filter, accountingPeriodFilterFn, nil)
}

func (s *InMemoryAccountingPeriodStore) Count(ctx context.Context, filter *accountingPeriodRepo.Filter) (int, error) {
	if filter == nil {
		filter = accountingPeriodRepo.NewFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, accountingPeriodFilterFn)
}

func accountingPeriodFilterFn(_ context.Context, p *accountingperiod.AccountingPeriod, filter interface{}) bool {
	f, ok := filter.(*accountingPeriodRepo.Filter)
	if !ok {
		return true
	}
	status := lo.Ternary(f.GetStatus() == "", types.StatusPublished, types.Status(f.GetStatus()))
	if p.Status() != status {
		return false
	}
	if len(f.AccountingPeriodIDs) > 0 && !lo.Contains(f.AccountingPeriodIDs, p.ID()) {
		return false
	}
	if len(f.PeriodStatuses) > 0 && !lo.Contains(f.PeriodStatuses, p.PeriodStatus()) {
		return false
	}
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && p.PeriodStart().Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && !p.PeriodStart().Before(*f.EndTime) {
			return false
		}
	}
	return true
}
