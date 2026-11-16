//go:build pgintegration

package ent

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/ent/customer"
	"github.com/flexprice/flexprice/internal/config"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// search_query is an OR of name/email/external_id ILIKE and id equality, ANDed with the
// rest of the filter. Only Postgres runs the real predicate. Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run TestCustomerSearchQuery
func TestCustomerSearchQuery(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	// Reads go to the writer, so a configured reader's replication lag cannot hide the fixtures.
	ctx := types.WithForceWriter(types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_customer_search_test"))
	// List and Create do not touch the cache.
	repo := NewCustomerRepository(client, log, nil)

	runID := types.GenerateUUIDWithPrefix("search")
	base := types.GetDefaultBaseModel(ctx)
	mk := func(id, name, externalID, email string) *domainCustomer.Customer {
		return &domainCustomer.Customer{
			ID:            id,
			Name:          name,
			ExternalID:    externalID,
			Email:         email,
			TaxTreatment:  types.TaxTreatmentTaxable,
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     base,
		}
	}
	acme := mk("cust_acme_"+runID, "Acme Robotics "+runID, "usw1:tenant_"+runID, "billing-"+runID+"@corp.test")
	globex := mk("cust_globex_"+runID, "Globex "+runID, "globex-"+runID, "globex@example.com")
	for _, c := range []*domainCustomer.Customer{acme, globex} {
		require.NoError(t, repo.Create(ctx, c))
	}
	t.Cleanup(func() {
		_, _ = client.Writer(ctx).Customer.Delete().
			Where(customer.IDIn(acme.ID, globex.ID)).Exec(ctx)
	})

	search := func(term string) []string {
		filter := &types.CustomerFilter{QueryFilter: types.NewNoLimitQueryFilter(), SearchQuery: term}
		got, err := repo.List(ctx, filter)
		require.NoError(t, err)
		count, err := repo.Count(ctx, filter)
		require.NoError(t, err)
		require.Equal(t, len(got), count, "count follows the same search")
		return lo.Map(got, func(c *domainCustomer.Customer, _ int) string { return c.ID })
	}

	require.ElementsMatch(t, []string{acme.ID}, search("ACME ROBOTICS "+runID))
	require.ElementsMatch(t, []string{acme.ID}, search("tenant_"+runID))
	require.ElementsMatch(t, []string{acme.ID}, search("BILLING-"+runID))
	require.ElementsMatch(t, []string{globex.ID}, search(globex.ID))
	// An id is matched exactly, not as a substring.
	require.Empty(t, search("globex_"+runID[:4]+"_no_such"))
	// ANDed with the other filters.
	got, err := repo.List(ctx, &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		SearchQuery: runID,
		ExternalIDs: []string{globex.ExternalID},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, globex.ID, got[0].ID)
}
