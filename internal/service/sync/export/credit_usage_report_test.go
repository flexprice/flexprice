package export

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// inMemoryWalletBalanceGetter is a test double for WalletBalanceGetter.
type inMemoryWalletBalanceGetter struct {
	responses map[string]*dto.WalletBalanceResponse
}

func newInMemoryWalletBalanceGetter() *inMemoryWalletBalanceGetter {
	return &inMemoryWalletBalanceGetter{responses: make(map[string]*dto.WalletBalanceResponse)}
}

func (m *inMemoryWalletBalanceGetter) set(walletID string, resp *dto.WalletBalanceResponse) {
	m.responses[walletID] = resp
}

func (m *inMemoryWalletBalanceGetter) GetWalletBalanceV2(_ context.Context, walletID string) (*dto.WalletBalanceResponse, error) {
	if resp, ok := m.responses[walletID]; ok {
		return resp, nil
	}
	return &dto.WalletBalanceResponse{}, nil
}

// creditUsageTestEnv bundles everything needed for a credit usage export test.
type creditUsageTestEnv struct {
	exporter      *CreditUsageExporter
	walletStore   *testutil.InMemoryWalletStore
	customerStore *testutil.InMemoryCustomerStore
	balanceGetter *inMemoryWalletBalanceGetter
	req           *dto.ExportRequest
	ctx           context.Context
	tenantID      string
	envID         string
	now           time.Time
}

func newCreditUsageTestEnv(t *testing.T) *creditUsageTestEnv {
	t.Helper()

	tenantID := "tenant-cu-1"
	envID := "env-cu-1"
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, envID)

	walletStore := testutil.NewInMemoryWalletStore()
	customerStore := testutil.NewInMemoryCustomerStore()
	balanceGetter := newInMemoryWalletBalanceGetter()
	log := logger.GetLogger()

	exporter := NewCreditUsageExporter(walletStore, customerStore, balanceGetter, nil, log)

	now := time.Now().UTC()
	req := &dto.ExportRequest{
		TenantID:   tenantID,
		EnvID:      envID,
		StartTime:  now.Add(-time.Hour),
		EndTime:    now.Add(time.Hour),
		EntityType: types.ScheduledTaskEntityTypeCreditUsage,
		JobConfig:  &types.S3JobConfig{},
	}

	return &creditUsageTestEnv{
		exporter:      exporter,
		walletStore:   walletStore,
		customerStore: customerStore,
		balanceGetter: balanceGetter,
		req:           req,
		ctx:           ctx,
		tenantID:      tenantID,
		envID:         envID,
		now:           now,
	}
}

func (e *creditUsageTestEnv) addCustomer(t *testing.T, id, externalID, name string, metadata map[string]string) *customer.Customer {
	t.Helper()
	c := &customer.Customer{
		ID:            id,
		ExternalID:    externalID,
		Name:          name,
		Metadata:      metadata,
		EnvironmentID: e.envID,
		BaseModel:     types.BaseModel{TenantID: e.tenantID, Status: types.StatusPublished, CreatedAt: e.now, UpdatedAt: e.now},
	}
	if err := e.customerStore.Create(e.ctx, c); err != nil {
		t.Fatalf("create customer %s: %v", id, err)
	}
	return c
}

func (e *creditUsageTestEnv) addWallet(t *testing.T, walletID, customerID string, creditBalance float64, metadata map[string]string) *wallet.Wallet {
	t.Helper()
	w := &wallet.Wallet{
		ID:            walletID,
		CustomerID:    customerID,
		Currency:      "USD",
		CreditBalance: decimal.NewFromFloat(creditBalance),
		WalletStatus:  types.WalletStatusActive,
		Metadata:      metadata,
		EnvironmentID: e.envID,
		BaseModel:     types.BaseModel{TenantID: e.tenantID, Status: types.StatusPublished, CreatedAt: e.now, UpdatedAt: e.now},
	}
	if err := e.walletStore.CreateWallet(e.ctx, w); err != nil {
		t.Fatalf("create wallet %s: %v", walletID, err)
	}
	return w
}

func (e *creditUsageTestEnv) setBalance(walletID string, w *wallet.Wallet, realtime float64) {
	rt := decimal.NewFromFloat(realtime)
	e.balanceGetter.set(walletID, &dto.WalletBalanceResponse{
		Wallet:                w,
		RealTimeCreditBalance: &rt,
	})
}

// parseCSVOutput splits raw CSV bytes into a header row and data rows.
func parseCSVOutput(t *testing.T, csvBytes []byte) (headers []string, rows [][]string) {
	t.Helper()
	r := csv.NewReader(strings.NewReader(string(csvBytes)))
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("failed to parse CSV: %v", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	return records[0], records[1:]
}

func colVal(t *testing.T, headers []string, row []string, name string) string {
	t.Helper()
	for i, h := range headers {
		if h == name {
			return row[i]
		}
	}
	t.Errorf("column %q not found in headers %v", name, headers)
	return ""
}

func TestCreditUsageExporter_PrepareData(t *testing.T) {
	staticCols := []string{
		string(wallet.CreditUsageCSVHeadersCustomerName),
		string(wallet.CreditUsageCSVHeadersCustomerExternalID),
		string(wallet.CreditUsageCSVHeadersCustomerID),
		string(wallet.CreditUsageCSVHeadersCurrentBalance),
		string(wallet.CreditUsageCSVHeadersRealtimeBalance),
		string(wallet.CreditUsageCSVHeadersNumberOfWallets),
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T, env *creditUsageTestEnv)
		wantCount int
		wantRows  int
		assertRow func(t *testing.T, headers []string, rows [][]string, env *creditUsageTestEnv)
	}{
		{
			name:      "empty customers produces headers only",
			setup:     func(t *testing.T, env *creditUsageTestEnv) {},
			wantCount: 0,
			wantRows:  0,
			assertRow: func(t *testing.T, headers []string, rows [][]string, _ *creditUsageTestEnv) {
				for _, want := range staticCols {
					found := false
					for _, h := range headers {
						if h == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("static header %q missing; got %v", want, headers)
					}
				}
			},
		},
		{
			name: "single customer single wallet static fields",
			setup: func(t *testing.T, env *creditUsageTestEnv) {
				c := env.addCustomer(t, "cust-1", "ext-1", "Acme Corp", nil)
				w := env.addWallet(t, "wallet-1", c.ID, 200, nil)
				env.setBalance(w.ID, w, 180)
			},
			wantCount: 1,
			wantRows:  1,
			assertRow: func(t *testing.T, headers []string, rows [][]string, env *creditUsageTestEnv) {
				col := func(name string) string { return colVal(t, headers, rows[0], name) }
				if got := col(string(wallet.CreditUsageCSVHeadersCustomerName)); got != "Acme Corp" {
					t.Errorf("customer_name: want Acme Corp got %q", got)
				}
				if got := col(string(wallet.CreditUsageCSVHeadersCustomerExternalID)); got != "ext-1" {
					t.Errorf("customer_external_id: want ext-1 got %q", got)
				}
				if got := col(string(wallet.CreditUsageCSVHeadersCustomerID)); got != "cust-1" {
					t.Errorf("customer_id: want cust-1 got %q", got)
				}
				if got := col(string(wallet.CreditUsageCSVHeadersCurrentBalance)); got != "200" {
					t.Errorf("current_balance: want 200 got %q", got)
				}
				if got := col(string(wallet.CreditUsageCSVHeadersRealtimeBalance)); got != "180" {
					t.Errorf("realtime_balance: want 180 got %q", got)
				}
				if got := col(string(wallet.CreditUsageCSVHeadersNumberOfWallets)); got != "1" {
					t.Errorf("number_of_wallets: want 1 got %q", got)
				}
			},
		},
		{
			name: "multiple wallets aggregate balances",
			setup: func(t *testing.T, env *creditUsageTestEnv) {
				c := env.addCustomer(t, "cust-2", "ext-2", "Multi Wallet Co", nil)
				w1 := env.addWallet(t, "wallet-a", c.ID, 100, nil)
				w2 := env.addWallet(t, "wallet-b", c.ID, 50, nil)
				env.setBalance(w1.ID, w1, 90)
				env.setBalance(w2.ID, w2, 45)
			},
			wantCount: 1,
			wantRows:  1,
			assertRow: func(t *testing.T, headers []string, rows [][]string, _ *creditUsageTestEnv) {
				col := func(name string) string { return colVal(t, headers, rows[0], name) }
				if got := col(string(wallet.CreditUsageCSVHeadersCurrentBalance)); got != "150" {
					t.Errorf("current_balance: want 150 got %q", got)
				}
				if got := col(string(wallet.CreditUsageCSVHeadersRealtimeBalance)); got != "135" {
					t.Errorf("realtime_balance: want 135 got %q", got)
				}
				if got := col(string(wallet.CreditUsageCSVHeadersNumberOfWallets)); got != "2" {
					t.Errorf("number_of_wallets: want 2 got %q", got)
				}
			},
		},
		{
			name: "dynamic metadata columns",
			setup: func(t *testing.T, env *creditUsageTestEnv) {
				c := env.addCustomer(t, "cust-3", "ext-3", "Meta Corp", map[string]string{"account_number": "ACC-001"})
				w := env.addWallet(t, "wallet-meta", c.ID, 500, map[string]string{"tier": "gold"})
				env.setBalance(w.ID, w, 490)
				env.req.JobConfig = &types.S3JobConfig{
					ExportMetadataFields: types.ExportMetadataFields{
						{EntityType: types.ExportMetadataEntityTypeCustomer, FieldKey: "account_number", ColumnName: "Account Number"},
						{EntityType: types.ExportMetadataEntityTypeWallet, FieldKey: "tier", ColumnName: "Tier"},
					},
				}
				if err := env.req.JobConfig.ExportMetadataFields.ValidateAndDefault(types.ScheduledTaskEntityTypeCreditUsage); err != nil {
					t.Fatalf("ValidateAndDefault: %v", err)
				}
			},
			wantCount: 1,
			wantRows:  1,
			assertRow: func(t *testing.T, headers []string, rows [][]string, _ *creditUsageTestEnv) {
				col := func(name string) string { return colVal(t, headers, rows[0], name) }
				if got := col("Account Number"); got != "ACC-001" {
					t.Errorf("Account Number: want ACC-001 got %q", got)
				}
				if got := col("Tier"); got != "gold" {
					t.Errorf("Tier: want gold got %q", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newCreditUsageTestEnv(t)
			tc.setup(t, env)

			csvBytes, count, err := env.exporter.PrepareData(env.ctx, env.req)
			if err != nil {
				t.Fatalf("PrepareData: %v", err)
			}
			if count != tc.wantCount {
				t.Errorf("record count: want %d got %d", tc.wantCount, count)
			}

			headers, rows := parseCSVOutput(t, csvBytes)
			if len(rows) != tc.wantRows {
				t.Fatalf("row count: want %d got %d", tc.wantRows, len(rows))
			}

			if tc.assertRow != nil {
				tc.assertRow(t, headers, rows, env)
			}
		})
	}
}
