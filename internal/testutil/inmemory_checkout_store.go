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
	if c.TenantID == "" {
		c.TenantID = types.GetTenantID(ctx)
	}
	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}
	return m.InMemoryStore.Create(ctx, c.ID, c)
}

func (m *InMemoryCheckoutStore) Get(ctx context.Context, id string) (*checkout.Checkout, error) {
	got, err := m.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if got.TenantID != types.GetTenantID(ctx) || got.EnvironmentID != types.GetEnvironmentID(ctx) {
		return nil, ierr.NewError("checkout not found").
			WithHintf("Checkout with ID %s was not found", id).
			Mark(ierr.ErrNotFound)
	}
	return got, nil
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
	params checkout.GetPendingByEntityParams,
) (*checkout.Checkout, error) {
	items, err := m.InMemoryStore.List(ctx, nil,
		func(_ context.Context, c *checkout.Checkout, _ interface{}) bool {
			return c.EntityType == params.EntityType &&
				c.EntityID == params.EntityID &&
				c.Mode == params.Mode &&
				c.TenantID == types.GetTenantID(ctx) &&
				c.EnvironmentID == types.GetEnvironmentID(ctx) &&
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

func (m *InMemoryCheckoutStore) ListPendingExpired(ctx context.Context, cutoff time.Time, filter *types.QueryFilter) ([]*checkout.Checkout, error) {
	items, err := m.InMemoryStore.List(ctx, nil,
		func(_ context.Context, c *checkout.Checkout, _ interface{}) bool {
			return c.Status == types.CheckoutStatusPending && c.ExpiresAt.Before(cutoff)
		},
		func(i, j *checkout.Checkout) bool { return i.ExpiresAt.Before(j.ExpiresAt) })
	if err != nil {
		return nil, err
	}
	if filter != nil && !filter.IsUnlimited() {
		if offset := filter.GetOffset(); offset > 0 {
			if offset >= len(items) {
				return []*checkout.Checkout{}, nil
			}
			items = items[offset:]
		}
		limit := filter.GetLimit()
		if limit > 0 && len(items) > limit {
			items = items[:limit]
		}
	}
	return items, nil
}
