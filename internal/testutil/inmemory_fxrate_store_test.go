package testutil

import (
	"context"
	"testing"
	"time"

	fxrate "github.com/flexprice/flexprice/internal/domain/fxrate"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fxRateCtx() context.Context {
	return types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
}

func newTestFXRate(ctx context.Context, id string, scope types.FXRateScope, scopeID, from, to, rate string) *fxrate.FXRate {
	return &fxrate.FXRate{
		ID:            id,
		Scope:         scope,
		ScopeID:       scopeID,
		FromCurrency:  from,
		ToCurrency:    to,
		Rate:          decimal.RequireFromString(rate),
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
	}
}

func TestFXRateStore_CreateGetList(t *testing.T) {
	ctx := fxRateCtx()
	s := NewInMemoryFXRateStore()
	r := newTestFXRate(ctx, "fxr_1", types.FXRateScopeTenant, types.FXRateScopeIDTenant, "usd", "inr", "83")
	require.NoError(t, s.Create(ctx, r))

	got, err := s.Get(ctx, "fxr_1")
	require.NoError(t, err)
	assert.True(t, decimal.RequireFromString("83").Equal(got.Rate))

	list, err := s.List(ctx, types.NewFXRateFilter())
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestFXRateStore_SoftArchiveOnDelete(t *testing.T) {
	ctx := fxRateCtx()
	s := NewInMemoryFXRateStore()
	r := newTestFXRate(ctx, "fxr_1", types.FXRateScopeCustomer, "cust_1", "usd", "inr", "83")
	require.NoError(t, s.Create(ctx, r))

	require.NoError(t, s.Delete(ctx, r))

	// default list hides archived
	list, err := s.List(ctx, types.NewFXRateFilter())
	require.NoError(t, err)
	assert.Empty(t, list, "archived rows are hidden by default")

	// row still exists, now archived
	got, err := s.Get(ctx, "fxr_1")
	require.NoError(t, err)
	assert.Equal(t, types.StatusArchived, got.Status)
}

func TestFXRateStore_TenantEnvIsolation(t *testing.T) {
	ctxA := fxRateCtx()
	s := NewInMemoryFXRateStore()
	require.NoError(t, s.Create(ctxA, newTestFXRate(ctxA, "fxr_a", types.FXRateScopeTenant, types.FXRateScopeIDTenant, "usd", "inr", "83")))

	ctxB := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_other")
	list, err := s.List(ctxB, types.NewFXRateFilter())
	require.NoError(t, err)
	assert.Empty(t, list, "a rate in env_test must not be visible in env_other")
}

func TestFXRateStore_GetTenantRate(t *testing.T) {
	ctx := fxRateCtx()
	s := NewInMemoryFXRateStore()
	require.NoError(t, s.Create(ctx, newTestFXRate(ctx, "fxr_t", types.FXRateScopeTenant, types.FXRateScopeIDTenant, "usd", "inr", "83")))

	got, err := s.GetTenantRate(ctx, "USD", "INR") // case-insensitive
	require.NoError(t, err)
	assert.Equal(t, "fxr_t", got.ID)

	_, err = s.GetTenantRate(ctx, "eur", "inr")
	assert.True(t, ierr.IsNotFound(err), "missing pair returns not found")
}

func TestFXRateStore_FindOverlapping(t *testing.T) {
	ctx := fxRateCtx()
	s := NewInMemoryFXRateStore()

	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	// open-ended override (nil..nil)
	open := newTestFXRate(ctx, "fxr_open", types.FXRateScopeCustomer, "cust_1", "usd", "inr", "84")
	require.NoError(t, s.Create(ctx, open))

	// a bounded window [nov, +inf) overlaps the open-ended one
	got, err := s.FindOverlapping(ctx, types.FXRateScopeCustomer, "cust_1", "usd", "inr", &nov, nil, "")
	require.NoError(t, err)
	assert.Len(t, got, 1, "open-ended override overlaps any window")

	// excluding self yields none
	got, err = s.FindOverlapping(ctx, types.FXRateScopeCustomer, "cust_1", "usd", "inr", &nov, nil, "fxr_open")
	require.NoError(t, err)
	assert.Empty(t, got)
}
