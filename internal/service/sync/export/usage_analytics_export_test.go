package export

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/group"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// inMemoryUsageAnalyticsGetter is a test double for UsageAnalyticsGetter.
type inMemoryUsageAnalyticsGetter struct {
	responses map[string]*dto.GetUsageAnalyticsResponse
}

func newInMemoryUsageAnalyticsGetter() *inMemoryUsageAnalyticsGetter {
	return &inMemoryUsageAnalyticsGetter{responses: make(map[string]*dto.GetUsageAnalyticsResponse)}
}

func (m *inMemoryUsageAnalyticsGetter) set(externalCustomerID string, resp *dto.GetUsageAnalyticsResponse) {
	m.responses[externalCustomerID] = resp
}

func (m *inMemoryUsageAnalyticsGetter) GetDetailedUsageAnalytics(_ context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error) {
	if resp, ok := m.responses[req.ExternalCustomerID]; ok {
		return resp, nil
	}
	return &dto.GetUsageAnalyticsResponse{}, nil
}

// usageAnalyticsTestEnv bundles everything needed for a usage analytics export test.
type usageAnalyticsTestEnv struct {
	exporter        *UsageAnalyticsExporter
	customerStore   *testutil.InMemoryCustomerStore
	eventRepo       *testutil.InMemoryEventStore
	analyticsGetter *inMemoryUsageAnalyticsGetter
	req             *dto.ExportRequest
	ctx             context.Context
	tenantID        string
	envID           string
	now             time.Time
	eventSeq        int
}

func newUsageAnalyticsTestEnv(t *testing.T) *usageAnalyticsTestEnv {
	t.Helper()

	tenantID := "tenant-ua-1"
	envID := "env-ua-1"
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, envID)

	customerStore := testutil.NewInMemoryCustomerStore()
	analyticsGetter := newInMemoryUsageAnalyticsGetter()
	log := logger.GetLogger()
	eventRepo := testutil.NewInMemoryEventStore()

	exporter := NewUsageAnalyticsExporter(customerStore, eventRepo, analyticsGetter, log)

	now := time.Now().UTC()
	req := &dto.ExportRequest{
		TenantID:   tenantID,
		EnvID:      envID,
		StartTime:  now.Add(-24 * time.Hour),
		EndTime:    now,
		EntityType: types.ScheduledTaskEntityTypeUsageAnalytics,
		JobConfig:  &types.S3JobConfig{},
	}

	return &usageAnalyticsTestEnv{
		exporter:        exporter,
		customerStore:   customerStore,
		eventRepo:       eventRepo,
		analyticsGetter: analyticsGetter,
		req:             req,
		ctx:             ctx,
		tenantID:        tenantID,
		envID:           envID,
		now:             now,
	}
}

func (e *usageAnalyticsTestEnv) addCustomer(t *testing.T, id, externalID, name string, metadata map[string]string) *customer.Customer {
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

func (e *usageAnalyticsTestEnv) setAnalytics(externalID string, items []dto.UsageAnalyticItem) {
	e.analyticsGetter.set(externalID, &dto.GetUsageAnalyticsResponse{Items: items})
}

func (e *usageAnalyticsTestEnv) addEvent(t *testing.T, externalCustomerID string, ts time.Time) {
	t.Helper()
	e.eventSeq++
	event := &events.Event{
		ID:                 fmt.Sprintf("evt-%s-%d", externalCustomerID, e.eventSeq),
		TenantID:           e.tenantID,
		EnvironmentID:      e.envID,
		ExternalCustomerID: externalCustomerID,
		EventName:          "usage_analytics_seed",
		Timestamp:          ts.UTC(),
		Properties:         map[string]any{},
	}
	if err := e.eventRepo.InsertEvent(e.ctx, event); err != nil {
		t.Fatalf("insert event for external customer %s: %v", externalCustomerID, err)
	}
}

func TestUsageAnalyticsExporter_PrepareData(t *testing.T) {
	staticCols := []string{
		string(UsageAnalyticsCSVHeadersCustomerName),
		string(UsageAnalyticsCSVHeadersCustomerID),
		string(UsageAnalyticsCSVHeadersCustomerExternalID),
		string(UsageAnalyticsCSVHeadersStartTime),
		string(UsageAnalyticsCSVHeadersEndTime),
		string(UsageAnalyticsCSVHeadersFeatureName),
		string(UsageAnalyticsCSVHeadersFeatureID),
		string(UsageAnalyticsCSVHeadersFeatureGroupName),
		string(UsageAnalyticsCSVHeadersEventName),
		string(UsageAnalyticsCSVHeadersEventCount),
		string(UsageAnalyticsCSVHeadersAggregationField),
		string(UsageAnalyticsCSVHeadersTotalUsage),
		string(UsageAnalyticsCSVHeadersTotalCost),
		string(UsageAnalyticsCSVHeadersCurrency),
		string(UsageAnalyticsCSVHeadersSource),
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T, env *usageAnalyticsTestEnv)
		wantCount int
		wantRows  int
		assertRow func(t *testing.T, headers []string, rows [][]string, env *usageAnalyticsTestEnv)
	}{
		{
			name:      "empty customers produces headers only",
			setup:     func(t *testing.T, env *usageAnalyticsTestEnv) {},
			wantCount: 0,
			wantRows:  0,
			assertRow: func(t *testing.T, headers []string, _ [][]string, _ *usageAnalyticsTestEnv) {
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
			name: "customer with no events in window produces no rows",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				env.addCustomer(t, "cust-1", "ext-1", "Idle Corp", nil)
			},
			wantCount: 0,
			wantRows:  0,
		},
		{
			name: "customer with events but empty analytics produces no rows",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				c := env.addCustomer(t, "cust-ghost", "ext-ghost", "Ghost Corp", nil)
				env.addEvent(t, c.ExternalID, env.req.StartTime.Add(1*time.Minute))
				// No explicit setAnalytics call: getter returns empty Items by default.
			},
			wantCount: 0,
			wantRows:  0,
		},
		{
			name: "single customer single feature row",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				c := env.addCustomer(t, "cust-2", "ext-2", "Acme Corp", nil)
				env.addEvent(t, c.ExternalID, env.req.StartTime.Add(1*time.Minute))
				env.setAnalytics(c.ExternalID, []dto.UsageAnalyticItem{
					{
						FeatureID:       "feat-1",
						FeatureName:     "API Calls",
						EventName:       "api_call",
						EventCount:      42,
						TotalUsage:      decimal.NewFromInt(42),
						TotalCost:       decimal.NewFromFloat(4.20),
						Currency:        "USD",
						AggregationType: types.AggregationCount,
					},
				})
			},
			wantCount: 1,
			wantRows:  1,
			assertRow: func(t *testing.T, headers []string, rows [][]string, env *usageAnalyticsTestEnv) {
				col := func(name string) string { return colVal(t, headers, rows[0], name) }
				if got := col(string(UsageAnalyticsCSVHeadersCustomerName)); got != "Acme Corp" {
					t.Errorf("customer_name: want Acme Corp got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersCustomerID)); got != "cust-2" {
					t.Errorf("customer_id: want cust-2 got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersCustomerExternalID)); got != "ext-2" {
					t.Errorf("customer_external_id: want ext-2 got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersFeatureName)); got != "API Calls" {
					t.Errorf("feature_name: want 'API Calls' got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersFeatureID)); got != "feat-1" {
					t.Errorf("feature_id: want feat-1 got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersEventName)); got != "api_call" {
					t.Errorf("event_name: want api_call got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersEventCount)); got != "42" {
					t.Errorf("event_count: want 42 got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersTotalUsage)); got != "42" {
					t.Errorf("total_usage: want 42 got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersTotalCost)); got != "4.2" {
					t.Errorf("total_cost: want 4.2 got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersCurrency)); got != "USD" {
					t.Errorf("currency: want USD got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersFeatureGroupName)); got != "" {
					t.Errorf("feature_group_name: want empty got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersAggregationField)); got != string(types.AggregationCount) {
					t.Errorf("aggregation_field: want %q got %q", types.AggregationCount, got)
				}
			},
		},
		{
			name: "feature group name and aggregation field populated",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				c := env.addCustomer(t, "cust-grp", "ext-grp", "Group Corp", nil)
				env.addEvent(t, c.ExternalID, env.req.StartTime.Add(1*time.Minute))
				env.setAnalytics(c.ExternalID, []dto.UsageAnalyticItem{
					{
						FeatureID:       "feat-grp",
						FeatureName:     "LLM Calls",
						EventName:       "llm_call",
						EventCount:      10,
						TotalUsage:      decimal.NewFromInt(10),
						TotalCost:       decimal.NewFromInt(1),
						Currency:        "USD",
						AggregationType: types.AggregationSum,
						Group:           &group.Group{ID: "grp-1", Name: "AI Features"},
					},
				})
			},
			wantCount: 1,
			wantRows:  1,
			assertRow: func(t *testing.T, headers []string, rows [][]string, _ *usageAnalyticsTestEnv) {
				col := func(name string) string { return colVal(t, headers, rows[0], name) }
				if got := col(string(UsageAnalyticsCSVHeadersFeatureGroupName)); got != "AI Features" {
					t.Errorf("feature_group_name: want 'AI Features' got %q", got)
				}
				if got := col(string(UsageAnalyticsCSVHeadersAggregationField)); got != string(types.AggregationSum) {
					t.Errorf("aggregation_field: want %q got %q", types.AggregationSum, got)
				}
			},
		},
		{
			name: "multiple customers multiple features produce one row each",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				c1 := env.addCustomer(t, "cust-3", "ext-3", "Alpha Inc", nil)
				c2 := env.addCustomer(t, "cust-4", "ext-4", "Beta Ltd", nil)
				env.addEvent(t, c1.ExternalID, env.req.StartTime.Add(1*time.Minute))
				env.addEvent(t, c2.ExternalID, env.req.StartTime.Add(2*time.Minute))
				env.setAnalytics(c1.ExternalID, []dto.UsageAnalyticItem{
					{FeatureID: "feat-a", FeatureName: "Storage", EventName: "storage_write", EventCount: 10, TotalUsage: decimal.NewFromInt(10), TotalCost: decimal.NewFromInt(1), Currency: "USD"},
					{FeatureID: "feat-b", FeatureName: "Compute", EventName: "compute_run", EventCount: 5, TotalUsage: decimal.NewFromInt(5), TotalCost: decimal.NewFromFloat(0.5), Currency: "USD"},
				})
				env.setAnalytics(c2.ExternalID, []dto.UsageAnalyticItem{
					{FeatureID: "feat-a", FeatureName: "Storage", EventName: "storage_write", EventCount: 20, TotalUsage: decimal.NewFromInt(20), TotalCost: decimal.NewFromInt(2), Currency: "USD"},
				})
			},
			wantCount: 3,
			wantRows:  3,
		},
		{
			name: "customer metadata dynamic columns",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				c := env.addCustomer(t, "cust-5", "ext-5", "Meta Corp", map[string]string{"plan_tier": "enterprise", "region": "us-east"})
				env.addEvent(t, c.ExternalID, env.req.StartTime.Add(1*time.Minute))
				env.setAnalytics(c.ExternalID, []dto.UsageAnalyticItem{
					{FeatureID: "feat-1", FeatureName: "API Calls", EventName: "api_call", EventCount: 100, TotalUsage: decimal.NewFromInt(100), TotalCost: decimal.NewFromInt(10), Currency: "USD"},
				})
				env.req.JobConfig = &types.S3JobConfig{
					ExportMetadataFields: types.ExportMetadataFields{
						{EntityType: types.ExportMetadataEntityTypeCustomer, FieldKey: "plan_tier", ColumnName: "Plan Tier"},
						{EntityType: types.ExportMetadataEntityTypeCustomer, FieldKey: "region", ColumnName: "Region"},
					},
				}
				if err := env.req.JobConfig.ExportMetadataFields.ValidateAndDefault(types.ScheduledTaskEntityTypeUsageAnalytics); err != nil {
					t.Fatalf("ValidateAndDefault: %v", err)
				}
			},
			wantCount: 1,
			wantRows:  1,
			assertRow: func(t *testing.T, headers []string, rows [][]string, _ *usageAnalyticsTestEnv) {
				col := func(name string) string { return colVal(t, headers, rows[0], name) }
				if got := col("Plan Tier"); got != "enterprise" {
					t.Errorf("Plan Tier: want enterprise got %q", got)
				}
				if got := col("Region"); got != "us-east" {
					t.Errorf("Region: want us-east got %q", got)
				}
			},
		},
		{
			name: "wallet metadata entity type rejected for usage analytics",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				env.req.JobConfig = &types.S3JobConfig{
					ExportMetadataFields: types.ExportMetadataFields{
						{EntityType: types.ExportMetadataEntityTypeWallet, FieldKey: "tier", ColumnName: "Tier"},
					},
				}
			},
			wantCount: 0,
			wantRows:  0,
			assertRow: func(t *testing.T, headers []string, rows [][]string, env *usageAnalyticsTestEnv) {
				err := env.req.JobConfig.ExportMetadataFields.ValidateAndDefault(types.ScheduledTaskEntityTypeUsageAnalytics)
				if err == nil {
					t.Error("expected validation error for wallet entity_type on usage_analytics export, got nil")
				}
			},
		},
		{
			name: "source column populated when analytics returns source",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				c := env.addCustomer(t, "cust-src", "ext-src", "Source Corp", nil)
				env.addEvent(t, c.ExternalID, env.req.StartTime.Add(1*time.Minute))
				env.setAnalytics(c.ExternalID, []dto.UsageAnalyticItem{
					{FeatureID: "feat-1", FeatureName: "LLM Calls", EventName: "llm_call", Source: "gemma4_389f6963-c14f-44d0-afc3-d7e89c6a5ab8", EventCount: 10, TotalUsage: decimal.NewFromInt(10), TotalCost: decimal.NewFromInt(1), Currency: "USD"},
					{FeatureID: "feat-1", FeatureName: "LLM Calls", EventName: "llm_call", Source: "gpt4o_7a2b1c3d-beef-cafe-dead-000000000001", EventCount: 5, TotalUsage: decimal.NewFromInt(5), TotalCost: decimal.NewFromFloat(0.5), Currency: "USD"},
				})
			},
			wantCount: 2,
			wantRows:  2,
			assertRow: func(t *testing.T, headers []string, rows [][]string, _ *usageAnalyticsTestEnv) {
				sources := make(map[string]bool)
				for _, row := range rows {
					sources[colVal(t, headers, row, string(UsageAnalyticsCSVHeadersSource))] = true
				}
				for _, want := range []string{"gemma4_389f6963-c14f-44d0-afc3-d7e89c6a5ab8", "gpt4o_7a2b1c3d-beef-cafe-dead-000000000001"} {
					if !sources[want] {
						t.Errorf("source %q not found in rows", want)
					}
				}
			},
		},
		{
			name: "missing metadata key produces empty cell",
			setup: func(t *testing.T, env *usageAnalyticsTestEnv) {
				c := env.addCustomer(t, "cust-6", "ext-6", "Sparse Corp", map[string]string{"plan_tier": "starter"})
				env.addEvent(t, c.ExternalID, env.req.StartTime.Add(1*time.Minute))
				env.setAnalytics(c.ExternalID, []dto.UsageAnalyticItem{
					{FeatureID: "feat-1", FeatureName: "API Calls", EventName: "api_call", EventCount: 1, TotalUsage: decimal.NewFromInt(1), TotalCost: decimal.NewFromFloat(0.1), Currency: "USD"},
				})
				env.req.JobConfig = &types.S3JobConfig{
					ExportMetadataFields: types.ExportMetadataFields{
						{EntityType: types.ExportMetadataEntityTypeCustomer, FieldKey: "plan_tier", ColumnName: "Plan Tier"},
						{EntityType: types.ExportMetadataEntityTypeCustomer, FieldKey: "nonexistent_key", ColumnName: "Missing"},
					},
				}
				if err := env.req.JobConfig.ExportMetadataFields.ValidateAndDefault(types.ScheduledTaskEntityTypeUsageAnalytics); err != nil {
					t.Fatalf("ValidateAndDefault: %v", err)
				}
			},
			wantCount: 1,
			wantRows:  1,
			assertRow: func(t *testing.T, headers []string, rows [][]string, _ *usageAnalyticsTestEnv) {
				col := func(name string) string { return colVal(t, headers, rows[0], name) }
				if got := col("Plan Tier"); got != "starter" {
					t.Errorf("Plan Tier: want starter got %q", got)
				}
				if got := col("Missing"); got != "" {
					t.Errorf("Missing: want empty string got %q", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newUsageAnalyticsTestEnv(t)
			tc.setup(t, env)

			// Skip PrepareData for the validation-only test case
			if tc.name == "wallet metadata entity type rejected for usage analytics" {
				if tc.assertRow != nil {
					tc.assertRow(t, nil, nil, env)
				}
				return
			}

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
