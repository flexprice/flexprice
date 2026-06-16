package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/checkout"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryCheckoutStore implements checkout.Repository for tests.
type InMemoryCheckoutStore struct {
	*InMemoryStore[*checkout.Checkout]
}

var _ checkout.Repository = (*InMemoryCheckoutStore)(nil)

func NewInMemoryCheckoutStore() *InMemoryCheckoutStore {
	return &InMemoryCheckoutStore{InMemoryStore: NewInMemoryStore[*checkout.Checkout]()}
}

func (m *InMemoryCheckoutStore) Create(ctx context.Context, c *checkout.Checkout) error {
	if c == nil || c.ID == "" {
		return ierr.NewError("checkout id required").
			WithHint("Checkout cannot be nil and must have an ID").
			Mark(ierr.ErrValidation)
	}
	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	return m.InMemoryStore.Create(ctx, c.ID, c)
}

func (m *InMemoryCheckoutStore) Get(ctx context.Context, id string) (*checkout.Checkout, error) {
	return m.InMemoryStore.Get(ctx, id)
}

func (m *InMemoryCheckoutStore) Update(ctx context.Context, c *checkout.Checkout) error {
	if c == nil || c.ID == "" {
		return ierr.NewError("checkout id required").
			WithHint("Checkout cannot be nil and must have an ID").
			Mark(ierr.ErrValidation)
	}
	c.UpdatedAt = time.Now().UTC()
	return m.InMemoryStore.Update(ctx, c.ID, c)
}

func (m *InMemoryCheckoutStore) GetPendingByEntity(
	ctx context.Context,
	entityType types.CheckoutEntityType,
	entityID string,
	objective types.CheckoutObjective,
) (*checkout.Checkout, error) {
	items, err := m.InMemoryStore.List(ctx, nil,
		func(_ context.Context, c *checkout.Checkout, _ interface{}) bool {
			return c.EntityType == entityType &&
				c.EntityID == entityID &&
				c.Objective == objective &&
				c.Status == types.CheckoutStatusPending
		}, nil)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0], nil
}

func (m *InMemoryCheckoutStore) GetPendingBySourceSubscription(
	ctx context.Context,
	sourceSubscriptionID string,
) (*checkout.Checkout, error) {
	items, err := m.InMemoryStore.List(ctx, nil,
		func(_ context.Context, c *checkout.Checkout, _ interface{}) bool {
			return c.SourceSubscriptionID != nil &&
				*c.SourceSubscriptionID == sourceSubscriptionID &&
				c.Status == types.CheckoutStatusPending
		}, nil)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0], nil
}

func (m *InMemoryCheckoutStore) ListPendingExpired(ctx context.Context, cutoff time.Time) ([]*checkout.Checkout, error) {
	return m.InMemoryStore.List(ctx, nil,
		func(_ context.Context, c *checkout.Checkout, _ interface{}) bool {
			return c.Status == types.CheckoutStatusPending && c.ExpiresAt.Before(cutoff)
		},
		func(i, j *checkout.Checkout) bool { return i.ExpiresAt.Before(j.ExpiresAt) })
}
