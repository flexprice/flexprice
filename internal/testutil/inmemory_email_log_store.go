package testutil

import (
	"context"
	"sort"

	"github.com/flexprice/flexprice/internal/domain/emaillog"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryEmailLogStore implements emaillog.Repository for tests.
type InMemoryEmailLogStore struct {
	*InMemoryStore[*emaillog.EmailLog]
}

func NewInMemoryEmailLogStore() *InMemoryEmailLogStore {
	return &InMemoryEmailLogStore{
		InMemoryStore: NewInMemoryStore[*emaillog.EmailLog](),
	}
}

func (s *InMemoryEmailLogStore) Create(ctx context.Context, e *emaillog.EmailLog) error {
	if e.Trigger == types.EmailTriggerAuto {
		all, _ := s.InMemoryStore.List(ctx, nil, nil, nil)
		if lo.ContainsBy(all, func(x *emaillog.EmailLog) bool {
			return x.Trigger == types.EmailTriggerAuto && x.TenantID == e.TenantID &&
				x.EnvironmentID == e.EnvironmentID && x.EntityID == e.EntityID && x.TemplateKey == e.TemplateKey
		}) {
			return ierr.NewError("automatic email already sent").Mark(ierr.ErrAlreadyExists)
		}
	}
	return s.InMemoryStore.Create(ctx, e.ID, e)
}

func (s *InMemoryEmailLogStore) Get(ctx context.Context, id string) (*emaillog.EmailLog, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryEmailLogStore) Update(ctx context.Context, e *emaillog.EmailLog) error {
	return s.InMemoryStore.Update(ctx, e.ID, e)
}

func (s *InMemoryEmailLogStore) List(ctx context.Context, filter *types.EmailLogFilter) ([]*emaillog.EmailLog, error) {
	if filter == nil {
		filter = types.NewEmailLogFilter()
	}
	return s.InMemoryStore.List(ctx, filter, emailLogFilterFn, emailLogSortFn)
}

func (s *InMemoryEmailLogStore) Count(ctx context.Context, filter *types.EmailLogFilter) (int, error) {
	if filter == nil {
		filter = types.NewEmailLogFilter()
	}
	return s.InMemoryStore.Count(ctx, filter, emailLogFilterFn)
}

func (s *InMemoryEmailLogStore) ListEnvironmentsWithQueuedEmails(ctx context.Context) ([]types.TenantEnvironment, error) {
	all, err := s.InMemoryStore.List(ctx, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	queued := lo.Filter(all, func(e *emaillog.EmailLog, _ int) bool {
		return e.DeliveryStatus == types.EmailDeliveryStatusQueued
	})
	return lo.Uniq(lo.Map(queued, func(e *emaillog.EmailLog, _ int) types.TenantEnvironment {
		return types.TenantEnvironment{TenantID: e.TenantID, EnvironmentID: e.EnvironmentID}
	})), nil
}

func (s *InMemoryEmailLogStore) ListQueued(ctx context.Context, limit int) ([]*emaillog.EmailLog, error) {
	all, err := s.InMemoryStore.List(ctx, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	queued := lo.Filter(all, func(e *emaillog.EmailLog, _ int) bool {
		return e.TenantID == types.GetTenantID(ctx) && e.EnvironmentID == types.GetEnvironmentID(ctx) &&
			e.DeliveryStatus == types.EmailDeliveryStatusQueued
	})
	sort.SliceStable(queued, func(i, j int) bool {
		mi, mj := queued[i].Trigger == types.EmailTriggerManual, queued[j].Trigger == types.EmailTriggerManual
		if mi != mj {
			return mi
		}
		return queued[i].CreatedAt.Before(queued[j].CreatedAt)
	})
	if len(queued) > limit {
		queued = queued[:limit]
	}
	return queued, nil
}

func emailLogFilterFn(ctx context.Context, e *emaillog.EmailLog, filter interface{}) bool {
	f, ok := filter.(*types.EmailLogFilter)
	if !ok {
		return false
	}
	if e.TenantID != types.GetTenantID(ctx) || e.EnvironmentID != types.GetEnvironmentID(ctx) {
		return false
	}
	if f.EntityType != "" && e.EntityType != f.EntityType {
		return false
	}
	return len(f.EntityIDs) == 0 || lo.Contains(f.EntityIDs, e.EntityID)
}

func emailLogSortFn(i, j *emaillog.EmailLog) bool {
	return i.CreatedAt.After(j.CreatedAt)
}
