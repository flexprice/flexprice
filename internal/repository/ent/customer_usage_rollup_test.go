package ent

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/cache"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestCustomerUsageRollupEligibility(t *testing.T) {
	client := newRealPostgresTestClient(t)
	repo := NewCustomerRepository(client, logger.NewNoopLogger(), cache.NewRedisCache())
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.GenerateUUIDWithPrefix("tenant")), "env_usage_rollup")
	ctx = types.SetUserID(ctx, "realtime_test")
	create := func() string {
		c := &domainCustomer.Customer{ID: types.GenerateUUIDWithPrefix("cus"), ExternalID: types.GenerateUUID(), Name: "rt", TaxTreatment: types.TaxTreatmentTaxable,
			EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
		require.NoError(t, repo.Create(ctx, c))
		return c.ID
	}
	first, second := create(), create()

	eligible, err := repo.GetUsageRollupEligible(ctx, first)
	require.NoError(t, err)
	require.Nil(t, eligible, "a new customer is not evaluated yet")

	require.NoError(t, repo.SetUsageRollupEligible(ctx, first, true))
	require.NoError(t, repo.SetUsageRollupEligible(ctx, second, false))
	eligible, err = repo.GetUsageRollupEligible(ctx, first)
	require.NoError(t, err)
	require.Equal(t, lo.ToPtr(true), eligible)

	require.NoError(t, repo.ClearUsageRollupEligible(ctx, []string{first, second}))
	for _, id := range []string{first, second} {
		eligible, err = repo.GetUsageRollupEligible(ctx, id)
		require.NoError(t, err)
		require.Nil(t, eligible, "a cleared flag is not evaluated")
	}

	require.NoError(t, repo.SetUsageRollupEligible(ctx, first, true))
	otherTenant := types.SetTenantID(ctx, types.GenerateUUIDWithPrefix("tenant"))
	require.NoError(t, repo.SetUsageRollupEligible(otherTenant, first, false))
	require.NoError(t, repo.ClearUsageRollupEligible(otherTenant, []string{first}))
	eligible, err = repo.GetUsageRollupEligible(ctx, first)
	require.NoError(t, err)
	require.Equal(t, lo.ToPtr(true), eligible, "another tenant cannot change the flag")
	_, err = repo.GetUsageRollupEligible(otherTenant, first)
	require.Error(t, err, "another tenant cannot read it")
}
