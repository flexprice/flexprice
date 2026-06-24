package testutil

import (
	"context"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryCheckoutSessionStore implements domainCheckout.Repository for tests.
type InMemoryCheckoutSessionStore struct {
	*InMemoryStore[*domainCheckout.CheckoutSession]
}

func NewInMemoryCheckoutSessionStore() *InMemoryCheckoutSessionStore {
	return &InMemoryCheckoutSessionStore{
		InMemoryStore: NewInMemoryStore[*domainCheckout.CheckoutSession](),
	}
}

func (s *InMemoryCheckoutSessionStore) Create(ctx context.Context, session *domainCheckout.CheckoutSession) error {
	if session.IdempotencyKey != nil {
		items, _ := s.InMemoryStore.List(ctx, nil, nil, nil)
		for _, existing := range items {
			if existing.IdempotencyKey != nil &&
				*existing.IdempotencyKey == *session.IdempotencyKey &&
				(existing.CheckoutStatus == types.CheckoutStatusInitiated ||
					existing.CheckoutStatus == types.CheckoutStatusPending) {
				return ierr.NewError("active checkout session with this idempotency key already exists").
					Mark(ierr.ErrAlreadyExists)
			}
		}
	}
	return s.InMemoryStore.Create(ctx, session.ID, session)
}

func (s *InMemoryCheckoutSessionStore) Get(ctx context.Context, id string) (*domainCheckout.CheckoutSession, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryCheckoutSessionStore) Update(ctx context.Context, session *domainCheckout.CheckoutSession) error {
	return s.InMemoryStore.Update(ctx, session.ID, session)
}

func checkoutSessionFilterFn(ctx context.Context, session *domainCheckout.CheckoutSession, f interface{}) bool {
	if session == nil {
		return false
	}
	if f == nil {
		return true
	}
	filter, ok := f.(*types.CheckoutSessionFilter)
	if !ok {
		return true
	}
	if len(filter.CustomerIDs) > 0 {
		found := false
		for _, id := range filter.CustomerIDs {
			if session.CustomerID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(filter.CheckoutStatuses) > 0 {
		found := false
		for _, st := range filter.CheckoutStatuses {
			if session.CheckoutStatus == st {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func checkoutSessionSortFn(i, j *domainCheckout.CheckoutSession) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryCheckoutSessionStore) List(ctx context.Context, filter *types.CheckoutSessionFilter) ([]*domainCheckout.CheckoutSession, error) {
	return s.InMemoryStore.List(ctx, filter, checkoutSessionFilterFn, checkoutSessionSortFn)
}

func (s *InMemoryCheckoutSessionStore) Count(ctx context.Context, filter *types.CheckoutSessionFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, checkoutSessionFilterFn)
}

func (s *InMemoryCheckoutSessionStore) GetByIdempotencyKey(ctx context.Context, key string) (*domainCheckout.CheckoutSession, error) {
	items, err := s.InMemoryStore.List(ctx, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.IdempotencyKey != nil && *item.IdempotencyKey == key &&
			(item.CheckoutStatus == types.CheckoutStatusInitiated ||
				item.CheckoutStatus == types.CheckoutStatusPending) {
			return item, nil
		}
	}
	return nil, ierr.NewError("checkout session not found").Mark(ierr.ErrNotFound)
}
