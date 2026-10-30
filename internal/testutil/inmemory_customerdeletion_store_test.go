package testutil

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/customerdeletion"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deletionCtx(tenantID, environmentID string) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	return ctx
}

func newSuppressedRequest(ctx context.Context, id, customerID, externalID string) (*customerdeletion.CustomerDeletionRequest, []*customerdeletion.CustomerDeletionCategory) {
	now := time.Now().UTC()
	request := &customerdeletion.CustomerDeletionRequest{
		ID:              id,
		CustomerID:      customerID,
		ExternalID:      externalID,
		State:           customerdeletion.StateSuppressed,
		RequestedAt:     now,
		SuppressedAt:    &now,
		RequestedVia:    customerdeletion.RequestedViaAPI,
		RetentionPolicy: map[string]interface{}{},
		BaseModel:       types.GetDefaultBaseModel(ctx),
	}
	categories := []*customerdeletion.CustomerDeletionCategory{
		{
			ID:          id + "_cat",
			Category:    customerdeletion.CategoryCustomerPG,
			Disposition: customerdeletion.DispositionPending,
			BaseModel:   types.GetDefaultBaseModel(ctx),
		},
	}
	return request, categories
}

func TestInMemoryCustomerDeletionStore_CreateAndRead(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctx, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctx, request, categories))

	got, err := store.GetByCustomerID(ctx, "cust_1")
	require.NoError(t, err)
	assert.Equal(t, "cdr_1", got.ID)
	assert.Equal(t, customerdeletion.StateSuppressed, got.State)
	assert.Equal(t, "acme-1", got.ExternalID)
	assert.Equal(t, types.StatusPublished, got.Status)

	cats, err := store.ListCategories(ctx, "cdr_1")
	require.NoError(t, err)
	require.Len(t, cats, 1)
	assert.Equal(t, customerdeletion.CategoryCustomerPG, cats[0].Category)
	assert.Equal(t, customerdeletion.DispositionPending, cats[0].Disposition)
	assert.Equal(t, "cdr_1", cats[0].RequestID)
}

func TestInMemoryCustomerDeletionStore_DuplicateCustomerRejected(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctx, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctx, request, categories))

	duplicate, dupCategories := newSuppressedRequest(ctx, "cdr_2", "cust_1", "acme-2")
	err := store.Create(ctx, duplicate, dupCategories)
	require.Error(t, err)
	assert.True(t, ierr.IsAlreadyExists(err))

	// Caller falls back to existing request
	existing, err := store.GetByCustomerID(ctx, "cust_1")
	require.NoError(t, err)
	assert.Equal(t, "cdr_1", existing.ID)
}

func TestInMemoryCustomerDeletionStore_RetiredExternalIDRejected(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctx, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctx, request, categories))

	reuse, reuseCategories := newSuppressedRequest(ctx, "cdr_2", "cust_2", "acme-1")
	err := store.Create(ctx, reuse, reuseCategories)
	require.Error(t, err)
	assert.True(t, ierr.IsAlreadyExists(err))
}

func TestInMemoryCustomerDeletionStore_GetByExternalID(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctx, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctx, request, categories))

	got, err := store.GetByExternalID(ctx, "acme-1")
	require.NoError(t, err)
	assert.Equal(t, "cdr_1", got.ID)
	assert.Equal(t, "cust_1", got.CustomerID)

	_, err = store.GetByExternalID(ctx, "never-requested")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))
}

func TestInMemoryCustomerDeletionStore_UpdateState(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctx, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctx, request, categories))

	require.NoError(t, store.UpdateState(ctx, "cdr_1", customerdeletion.StateSuppressed, customerdeletion.StateRedacting))

	got, err := store.GetByCustomerID(ctx, "cust_1")
	require.NoError(t, err)
	assert.Equal(t, customerdeletion.StateRedacting, got.State)

	err = store.UpdateState(ctx, "cdr_1", customerdeletion.StateSuppressed, customerdeletion.StateCompleted)
	require.Error(t, err)

	got, err = store.GetByCustomerID(ctx, "cust_1")
	require.NoError(t, err)
	assert.Equal(t, customerdeletion.StateRedacting, got.State)
}

func TestInMemoryCustomerDeletionStore_TenantIsolation(t *testing.T) {
	ctxA := deletionCtx("tenant_a", "env_a")
	ctxB := deletionCtx("tenant_b", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctxA, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctxA, request, categories))

	_, err := store.GetByCustomerID(ctxB, "cust_1")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))

	_, err = store.GetByExternalID(ctxB, "acme-1")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))

	cats, err := store.ListCategories(ctxB, "cdr_1")
	require.NoError(t, err)
	assert.Empty(t, cats)

	err = store.UpdateState(ctxB, "cdr_1", customerdeletion.StateSuppressed, customerdeletion.StateRedacting)
	require.Error(t, err)
}

func TestInMemoryCustomerDeletionStore_EnvironmentIsolation(t *testing.T) {
	ctxSandbox := deletionCtx("tenant_a", "env_sandbox")
	ctxProd := deletionCtx("tenant_a", "env_prod")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctxSandbox, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctxSandbox, request, categories))

	_, err := store.GetByCustomerID(ctxProd, "cust_1")
	require.Error(t, err)
	assert.True(t, ierr.IsNotFound(err))

	// Retirement is per environment
	prodRequest, prodCategories := newSuppressedRequest(ctxProd, "cdr_2", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctxProd, prodRequest, prodCategories))
}

func TestInMemoryCustomerDeletionStore_ConcurrentDuplicateCreate(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	const goroutines = 16
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			request, categories := newSuppressedRequest(ctx, fmt.Sprintf("cdr_%d", i), "cust_1", "acme-1")
			errs[i] = store.Create(ctx, request, categories)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		assert.True(t, ierr.IsAlreadyExists(err), "unexpected error: %v", err)
	}
	assert.Equal(t, 1, succeeded)

	stored, err := store.requests.List(ctx, nil, nil, nil)
	require.NoError(t, err)
	assert.Len(t, stored, 1)

	cats, err := store.ListCategories(ctx, stored[0].ID)
	require.NoError(t, err)
	assert.Len(t, cats, 1)
}

func TestInMemoryCustomerDeletionStore_ConcurrentUpdateStateFromSameState(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_a")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctx, "cdr_1", "cust_1", "acme-1")
	require.NoError(t, store.Create(ctx, request, categories))

	targets := []string{
		customerdeletion.StateRedacting,
		customerdeletion.StateCompleted,
		customerdeletion.StateFailed,
	}
	const perTarget = 5
	errs := make([]error, len(targets)*perTarget)

	var wg sync.WaitGroup
	for i, to := range targets {
		for j := 0; j < perTarget; j++ {
			wg.Add(1)
			idx := i*perTarget + j
			go func(idx int, to string) {
				defer wg.Done()
				errs[idx] = store.UpdateState(ctx, "cdr_1", customerdeletion.StateSuppressed, to)
			}(idx, to)
		}
	}
	wg.Wait()

	winner := ""
	succeeded := 0
	for idx, err := range errs {
		if err == nil {
			succeeded++
			winner = targets[idx/perTarget]
			continue
		}
		assert.True(t, ierr.IsNotFound(err), "unexpected error: %v", err)
	}
	require.Equal(t, 1, succeeded)

	got, err := store.GetByCustomerID(ctx, "cust_1")
	require.NoError(t, err)
	assert.Equal(t, winner, got.State)
}

func TestInMemoryCustomerDeletionStore_NestedPolicyIsolated(t *testing.T) {
	ctx := deletionCtx("tenant_a", "env_sandbox")
	store := NewInMemoryCustomerDeletionStore()

	request, categories := newSuppressedRequest(ctx, "cdr_1", "cust_1", "ext_1")
	request.RetentionPolicy = map[string]interface{}{
		"invoices": map[string]interface{}{"basis": "tax"},
		"stores":   []interface{}{"pg", "clickhouse"},
	}
	require.NoError(t, store.Create(ctx, request, categories))

	// mutating the caller's map must not reach the store
	request.RetentionPolicy["invoices"].(map[string]interface{})["basis"] = "tampered"
	request.RetentionPolicy["stores"].([]interface{})[0] = "tampered"

	stored, err := store.GetByCustomerID(ctx, "cust_1")
	require.NoError(t, err)
	assert.Equal(t, "tax", stored.RetentionPolicy["invoices"].(map[string]interface{})["basis"])
	assert.Equal(t, "pg", stored.RetentionPolicy["stores"].([]interface{})[0])

	// and a mutation of what we read back must not reach it either
	stored.RetentionPolicy["invoices"].(map[string]interface{})["basis"] = "tampered_again"
	again, err := store.GetByCustomerID(ctx, "cust_1")
	require.NoError(t, err)
	assert.Equal(t, "tax", again.RetentionPolicy["invoices"].(map[string]interface{})["basis"])
}
