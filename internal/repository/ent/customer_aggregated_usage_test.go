package ent

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/cache"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCustomerAggregatedUsageEligibility(t *testing.T) {
	client := newRealPostgresTestClient(t)
	repo := NewCustomerRepository(client, logger.NewNoopLogger(), cache.NewRedisCache())
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.GenerateUUIDWithPrefix("tenant")), "env_aggregated")
	ctx = types.SetUserID(ctx, "realtime_test")
	create := func() string {
		c := &domainCustomer.Customer{ID: types.GenerateUUIDWithPrefix("cus"), ExternalID: types.GenerateUUID(), Name: "rt", TaxTreatment: types.TaxTreatmentTaxable,
			EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx)}
		require.NoError(t, repo.Create(ctx, c))
		return c.ID
	}
	first, second := create(), create()

	eligible, err := repo.IsAggregatedUsageEligible(ctx, first)
	require.NoError(t, err)
	require.False(t, eligible, "a new customer starts ineligible")

	changed, err := repo.SetAggregatedUsageEligible(ctx, []string{first, second}, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{first, second}, changed)
	changed, err = repo.SetAggregatedUsageEligible(ctx, []string{first, second}, true)
	require.NoError(t, err)
	require.Empty(t, changed, "an unchanged flag is not rewritten")

	eligible, err = repo.IsAggregatedUsageEligible(ctx, first)
	require.NoError(t, err)
	require.True(t, eligible)

	otherTenant := types.SetTenantID(ctx, types.GenerateUUIDWithPrefix("tenant"))
	changed, err = repo.SetAggregatedUsageEligible(otherTenant, []string{first}, false)
	require.NoError(t, err)
	require.Empty(t, changed, "another tenant cannot change the flag")
	_, err = repo.IsAggregatedUsageEligible(otherTenant, first)
	require.Error(t, err, "another tenant cannot read it")
}
