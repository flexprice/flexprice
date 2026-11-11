//go:build pgintegration

package ent

import (
	"context"
	"fmt"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// Run with:
//
//	go test -tags pgintegration ./internal/repository/ent -run TestCustomerKeysetPaging
func TestCustomerKeysetPaging(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	client := postgres.NewClient(clients, log, nil)

	runID := types.GenerateUUIDWithPrefix("kset")
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_"+runID)
	repo := NewCustomerRepository(client, log, nil)
	base := types.GetDefaultBaseModel(ctx)

	var created []*domainCustomer.Customer
	for i := 0; i < 5; i++ {
		c := &domainCustomer.Customer{
			ID:            fmt.Sprintf("cust_%s_%d", runID, i),
			ExternalID:    fmt.Sprintf("ext_%s_%d", runID, i),
			Name:          fmt.Sprintf("name %d", i),
			TaxTreatment:  types.TaxTreatmentTaxable,
			Email:         fmt.Sprintf("u%d@example.com", i),
			Metadata:      map[string]string{fmt.Sprintf("k%d", i): "v"},
			EnvironmentID: types.GetEnvironmentID(ctx),
			BaseModel:     base,
		}
		require.NoError(t, repo.Create(ctx, c))
		created = append(created, c)
	}
	t.Cleanup(func() {
		for _, c := range created {
			_ = repo.Delete(ctx, c)
		}
	})

	refs, err := repo.ListRefsByFilter(ctx, nil, 0)
	require.NoError(t, err)
	require.Len(t, refs, 5)
	require.Equal(t, created[0].ID, refs[0].ID)

	_, err = repo.ListRefsByFilter(ctx, nil, 4)
	require.Error(t, err)
	require.True(t, ierr.IsValidation(err))

	var got []string
	after := ""
	for {
		page, err := repo.ListByFilterPage(ctx, nil, after, 2)
		require.NoError(t, err)
		for _, c := range page {
			got = append(got, c.ID)
		}
		if len(page) < 2 {
			break
		}
		after = page[len(page)-1].ID
	}
	require.Len(t, got, 5)

	meta, err := repo.ListMetadataPageByFilter(ctx, nil, "", 10)
	require.NoError(t, err)
	require.Len(t, meta, 5)
	require.Equal(t, map[string]string{"k0": "v"}, meta[0].Metadata)
}
