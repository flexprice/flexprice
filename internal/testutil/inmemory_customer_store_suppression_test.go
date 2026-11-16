package testutil

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func suppressionCtx(tenantID, environmentID string) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	return ctx
}

func newCustomerWithStatus(ctx context.Context, id, externalID string, status types.Status) *customer.Customer {
	base := types.GetDefaultBaseModel(ctx)
	base.Status = status
	return &customer.Customer{
		ID:            id,
		ExternalID:    externalID,
		Name:          id,
		Email:         id + "@example.com",
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     base,
	}
}

func seedSuppressionCustomers(ctx context.Context, t *testing.T) *InMemoryCustomerStore {
	t.Helper()
	store := NewInMemoryCustomerStore()
	require.NoError(t, store.Create(ctx, newCustomerWithStatus(ctx, "cust_pub", "ext_pub", types.StatusPublished)))
	require.NoError(t, store.Create(ctx, newCustomerWithStatus(ctx, "cust_arch", "ext_arch", types.StatusArchived)))
	require.NoError(t, store.Create(ctx, newCustomerWithStatus(ctx, "cust_sup", "ext_sup", types.StatusSuppressed)))
	return store
}

func TestSuppressedCustomerNotFoundByGet(t *testing.T) {
	ctx := suppressionCtx("tenant_a", "env_a")
	store := seedSuppressionCustomers(ctx, t)

	c, err := store.Get(ctx, "cust_sup")
	assert.Nil(t, c)
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err), "suppressed customer must read as not-found")
}

func TestSuppressedCustomerNotFoundByLookupKey(t *testing.T) {
	ctx := suppressionCtx("tenant_a", "env_a")
	store := seedSuppressionCustomers(ctx, t)

	c, err := store.GetByLookupKey(ctx, "ext_sup")
	assert.Nil(t, c)
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err), "suppressed external id must read as not-found")
}

func TestSuppressedCustomerAbsentFromDefaultListCountListAll(t *testing.T) {
	ctx := suppressionCtx("tenant_a", "env_a")
	store := seedSuppressionCustomers(ctx, t)

	filter := types.NewCustomerFilter()
	filter.QueryFilter.Status = nil
	// Empty Filters makes GetStatus return empty
	filter.Filters = []*types.FilterCondition{}

	listed, err := store.List(ctx, filter)
	require.NoError(t, err)
	ids := lo.Map(listed, func(c *customer.Customer, _ int) string { return c.ID })
	assert.NotContains(t, ids, "cust_sup")
	assert.Contains(t, ids, "cust_arch")

	count, err := store.Count(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, len(listed), count)
	assert.NotContains(t, ids, "cust_sup")

	all, err := store.ListAll(ctx, filter)
	require.NoError(t, err)
	allIDs := lo.Map(all, func(c *customer.Customer, _ int) string { return c.ID })
	assert.NotContains(t, allIDs, "cust_sup")
}

func TestExplicitSuppressedStatusFilterCannotSurfaceCustomer(t *testing.T) {
	ctx := suppressionCtx("tenant_a", "env_a")
	store := seedSuppressionCustomers(ctx, t)

	filter := types.NewCustomerFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusSuppressed)

	listed, err := store.List(ctx, filter)
	require.NoError(t, err)
	assert.Empty(t, listed, "explicit suppressed filter must match nothing")

	count, err := store.Count(ctx, filter)
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestSuppressedCustomerUnreachableViaFiltersDSL(t *testing.T) {
	ctx := suppressionCtx("tenant_a", "env_a")
	store := seedSuppressionCustomers(ctx, t)

	filter := types.NewCustomerFilter()
	filter.QueryFilter.Status = nil
	filter.Filters = []*types.FilterCondition{
		{
			Field:    lo.ToPtr("status"),
			Operator: lo.ToPtr(types.IN),
			Value:    &types.Value{Array: []string{string(types.StatusSuppressed)}},
		},
	}

	listed, err := store.List(ctx, filter)
	require.NoError(t, err)
	assert.Empty(t, listed, "DSL status filter must not reach suppressed rows")
}

func TestArchivedCustomerStillReadable(t *testing.T) {
	ctx := suppressionCtx("tenant_a", "env_a")
	store := seedSuppressionCustomers(ctx, t)

	c, err := store.Get(ctx, "cust_arch")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, types.StatusArchived, c.Status)

	filter := types.NewCustomerFilter()
	filter.QueryFilter.Status = nil
	// Empty Filters makes GetStatus return empty
	filter.Filters = []*types.FilterCondition{}
	listed, err := store.List(ctx, filter)
	require.NoError(t, err)
	ids := lo.Map(listed, func(c *customer.Customer, _ int) string { return c.ID })
	assert.Contains(t, ids, "cust_arch")
	assert.Contains(t, ids, "cust_pub")
}
