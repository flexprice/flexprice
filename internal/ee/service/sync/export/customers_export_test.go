package export

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func newCustomersEnv(t *testing.T) (*CustomersExporter, *testutil.InMemoryCustomerStore, context.Context) {
	t.Helper()
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "t1"), "e1")
	store := testutil.NewInMemoryCustomerStore()
	return NewCustomersExporter(store), store, ctx
}

func addCust(t *testing.T, store *testutil.InMemoryCustomerStore, ctx context.Context, id, tenant, env string, status types.Status, meta map[string]string) {
	t.Helper()
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, store.Create(ctx, &customer.Customer{
		ID: id, ExternalID: "ext-" + id, Name: "N " + id, Email: id + "@x.io",
		AddressCity: "Pune", BillingCurrency: lo.ToPtr("usd"), Metadata: meta,
		EnvironmentID: env,
		BaseModel:     types.BaseModel{TenantID: tenant, Status: status, CreatedAt: ts, UpdatedAt: ts},
	}))
}

func exportReq(cf *types.CustomerFilter) *dto.ExportRequest {
	return &dto.ExportRequest{
		TenantID: "t1", EnvID: "e1",
		EntityType:   types.ScheduledTaskEntityTypeCustomers,
		SourceConfig: &types.ExportSourceConfig{CustomerFilter: cf},
	}
}

func TestCustomersExporter_ZeroMatchesHeaderOnly(t *testing.T) {
	e, _, ctx := newCustomersEnv(t)
	out, n, err := e.PrepareData(ctx, exportReq(nil))
	require.NoError(t, err)
	require.Equal(t, 0, n)
	h, rows := parseCSVOutput(t, out)
	require.Empty(t, rows)
	require.Equal(t, "customer_id,external_id,name,email,status,billing_currency,billing_entity_id,address_line1,address_line2,city,state,postal_code,country,timezone,created_at,updated_at",
		strings.Join(h, ","))
}

func TestCustomersExporter_NeutralizesFormulaCells(t *testing.T) {
	e, store, ctx := newCustomersEnv(t)
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, store.Create(ctx, &customer.Customer{
		ID: "c1", ExternalID: "-1", Name: "=HYPERLINK(\"http://x\")", Email: "+a@x.io",
		AddressCity: "@Pune", AddressState: "\tTab", AddressCountry: "\rCR", EnvironmentID: "e1",
		Metadata:  map[string]string{"a": "=1+1", "b": "safe-text", "c": "-5"},
		BaseModel: types.BaseModel{TenantID: "t1", Status: types.StatusPublished, CreatedAt: ts, UpdatedAt: ts},
	}))

	out, _, err := e.PrepareData(ctx, exportReq(nil))
	require.NoError(t, err)
	h, rows := parseCSVOutput(t, out)
	require.Len(t, rows, 1)
	col := map[string]string{}
	for i, name := range h {
		col[name] = rows[0][i]
	}
	require.Equal(t, "'-1", col["external_id"])
	require.Equal(t, "'=HYPERLINK(\"http://x\")", col["name"])
	require.Equal(t, "'+a@x.io", col["email"])
	require.Equal(t, "'@Pune", col["city"])
	require.Equal(t, "'\tTab", col["state"])
	require.Equal(t, "'\rCR", col["country"])
	require.Equal(t, "'=1+1", col["metadata_a"])
	require.Equal(t, "safe-text", col["metadata_b"])
	require.Equal(t, "'-5", col["metadata_c"])
	require.Equal(t, "c1", col["customer_id"])
}

func TestCustomersExporter_StatusSemantics(t *testing.T) {
	e, store, ctx := newCustomersEnv(t)
	addCust(t, store, ctx, "pub", "t1", "e1", types.StatusPublished, nil)
	addCust(t, store, ctx, "arc", "t1", "e1", types.StatusArchived, nil)

	ids := func(cf *types.CustomerFilter) []string {
		out, _, err := e.PrepareData(ctx, exportReq(cf))
		require.NoError(t, err)
		_, rows := parseCSVOutput(t, out)
		return lo.Map(rows, func(r []string, _ int) string { return r[0] })
	}

	require.ElementsMatch(t, []string{"pub"}, ids(nil))
	require.ElementsMatch(t, []string{"pub"}, ids(&types.CustomerFilter{}))
	require.ElementsMatch(t, []string{"pub", "arc"}, ids(&types.CustomerFilter{Filters: []*types.FilterCondition{}}))
	archived := types.StatusArchived
	require.ElementsMatch(t, []string{"arc"}, ids(&types.CustomerFilter{QueryFilter: &types.QueryFilter{Status: &archived}}))
}

func TestCustomersExporter_RowsAndMetadataColumns(t *testing.T) {
	e, store, ctx := newCustomersEnv(t)
	addCust(t, store, ctx, "a", "t1", "e1", types.StatusPublished, map[string]string{"zeta": "1", "alpha": "2"})
	addCust(t, store, ctx, "b", "t1", "e1", types.StatusPublished, map[string]string{"alpha": "3"})

	out, n, err := e.PrepareData(ctx, exportReq(nil))
	require.NoError(t, err)
	require.Equal(t, 2, n)
	h, rows := parseCSVOutput(t, out)
	require.Equal(t, []string{"metadata_alpha", "metadata_zeta"}, h[16:])
	byID := map[string][]string{}
	for _, r := range rows {
		byID[r[0]] = r
	}
	a := byID["a"]
	require.Equal(t, "ext-a", a[1])
	require.Equal(t, "usd", a[5])
	require.Equal(t, "", a[6])
	require.Equal(t, "Pune", a[9])
	require.Equal(t, "2026-01-02T03:04:05Z", a[14])
	require.Equal(t, []string{"2", "1"}, a[16:])
	require.Equal(t, []string{"3", ""}, byID["b"][16:])
}

func TestCustomersExporter_TenantAndEnvScoping(t *testing.T) {
	e, store, ctx := newCustomersEnv(t)
	addCust(t, store, ctx, "mine", "t1", "e1", types.StatusPublished, nil)
	addCust(t, store, ctx, "othertenant", "t2", "e1", types.StatusPublished, nil)
	addCust(t, store, ctx, "otherenv", "t1", "e2", types.StatusPublished, nil)

	out, n, err := e.PrepareData(ctx, exportReq(nil))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, rows := parseCSVOutput(t, out)
	require.Equal(t, "mine", rows[0][0])

	refs, err := store.ListRefsByFilter(ctx, nil, 0)
	require.NoError(t, err)
	require.Equal(t, []customer.Ref{{ID: "mine", ExternalID: "ext-mine"}}, refs)
}

func TestListRefsByFilter_Cap(t *testing.T) {
	_, store, ctx := newCustomersEnv(t)
	for _, id := range []string{"a", "b", "c"} {
		addCust(t, store, ctx, id, "t1", "e1", types.StatusPublished, nil)
	}
	_, err := store.ListRefsByFilter(ctx, &types.CustomerFilter{}, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Filter matches more than 2 customers; narrow it")

	refs, err := store.ListRefsByFilter(ctx, &types.CustomerFilter{}, 3)
	require.NoError(t, err)
	require.Len(t, refs, 3)
}

func TestCustomersExporter_PagesAcrossBatches(t *testing.T) {
	e, store, ctx := newCustomersEnv(t)
	total := customersPageSize*2 + 5
	for i := 0; i < total; i++ {
		addCust(t, store, ctx, fmt.Sprintf("c%05d", i), "t1", "e1", types.StatusPublished, map[string]string{fmt.Sprintf("k%d", i%3): "v"})
	}
	out, n, err := e.PrepareData(ctx, exportReq(nil))
	require.NoError(t, err)
	require.Equal(t, total, n)
	h, rows := parseCSVOutput(t, out)
	require.Len(t, rows, total)
	require.Equal(t, []string{"metadata_k0", "metadata_k1", "metadata_k2"}, h[16:])
	require.Equal(t, "c00000", rows[0][0])
	require.Equal(t, fmt.Sprintf("c%05d", total-1), rows[total-1][0])
}
