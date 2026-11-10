package testutil

import (
	"context"

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
