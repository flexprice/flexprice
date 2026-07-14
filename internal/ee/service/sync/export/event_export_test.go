package export

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
	"github.com/shopspring/decimal"
)

// Behavior contract for the event export path after the feature_usage → meter_usage
// migration: EventExporter reads from meter_usage. meter_usage does NOT carry price,
// subscription, or feature identifiers, so these columns are emitted empty:
//   - SubscriptionID, SubLineItemID, PriceID, CustomerID, FeatureID, PeriodID
//   - ProvisionalUsageCharges (was price.Amount * qty in the feature_usage export)
// The CSV header row is preserved (schema-stable) so downstream consumers don't
// see a shape change.

func newExporterWithMeterUsage(t *testing.T) (*EventExporter, *testutil.InMemoryMeterUsageStore) {
	t.Helper()
	muStore := testutil.NewInMemoryMeterUsageStore()
	log := logger.NewNoopLogger()
	return NewEventExporter(muStore, testutil.NewInMemoryPriceStore(), nil, nil, log), muStore
}

func exportRequest(tenantID, envID string, now time.Time) *dto.ExportRequest {
	return &dto.ExportRequest{
		TenantID:   tenantID,
		EnvID:      envID,
		StartTime:  now.Add(-time.Hour),
		EndTime:    now.Add(time.Hour),
		EntityType: types.ScheduledTaskEntityTypeEvents,
		JobConfig:  &types.S3JobConfig{},
	}
}

func seedMeterUsage(t *testing.T, ctx context.Context, store *testutil.InMemoryMeterUsageStore, tenantID, envID string, now time.Time, id string, qty int64) {
	t.Helper()
	rec := &events.MeterUsage{
		Event: events.Event{
			ID:                 id,
			TenantID:           tenantID,
			EnvironmentID:      envID,
			EventName:          "test_event",
			Source:             "test",
			Timestamp:          now,
			IngestedAt:         now,
			ExternalCustomerID: "cust-1",
			Properties:         map[string]interface{}{"k": "v"},
		},
		MeterID:    "meter-1",
		QtyTotal:   decimal.NewFromInt(qty),
		UniqueHash: fmt.Sprintf("hash-%s", id),
	}
	if err := store.BulkInsertMeterUsage(ctx, []*events.MeterUsage{rec}); err != nil {
		t.Fatalf("seed meter_usage: %v", err)
	}
}

// TestEventExporter_PrepareData_MapsMeterUsageRow verifies a single meter_usage row
// is converted to one CSV record with the correct field mapping, and that
// feature_usage-only columns are emitted blank (behavior contract above).
func TestEventExporter_PrepareData_MapsMeterUsageRow(t *testing.T) {
	tenantID := "tenant-1"
	envID := "env-1"
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), tenantID), envID)
	now := time.Now().UTC()

	exporter, store := newExporterWithMeterUsage(t)
	seedMeterUsage(t, ctx, store, tenantID, envID, now, "usage-1", 3)

	csvBytes, count, err := exporter.PrepareData(ctx, exportRequest(tenantID, envID, now))
	if err != nil {
		t.Fatalf("PrepareData: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 record, got %d", count)
	}

	var records []*FeatureUsageCSV
	if err := gocsv.UnmarshalBytes(csvBytes, &records); err != nil {
		t.Fatalf("Unmarshal CSV: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 CSV record, got %d", len(records))
	}
	got := records[0]
	if got.ID != "usage-1" {
		t.Errorf("ID: want usage-1, got %q", got.ID)
	}
	if got.MeterID != "meter-1" {
		t.Errorf("MeterID: want meter-1, got %q", got.MeterID)
	}
	if got.QtyTotal != "3" {
		t.Errorf("QtyTotal: want 3, got %q", got.QtyTotal)
	}
	if got.ExternalCustomerID != "cust-1" {
		t.Errorf("ExternalCustomerID: want cust-1, got %q", got.ExternalCustomerID)
	}
	if got.EventName != "test_event" {
		t.Errorf("EventName: want test_event, got %q", got.EventName)
	}
	if got.UniqueHash != "hash-usage-1" {
		t.Errorf("UniqueHash: want hash-usage-1, got %q", got.UniqueHash)
	}
}

// TestEventExporter_PrepareData_LeavesFeatureUsageOnlyColumnsBlank locks in the
// behavior that price/subscription/feature/customer columns are emitted blank
// under the meter_usage pipeline. If cost derivation is re-added later, this
// test must be updated in the same PR.
func TestEventExporter_PrepareData_LeavesFeatureUsageOnlyColumnsBlank(t *testing.T) {
	tenantID := "tenant-2"
	envID := "env-2"
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), tenantID), envID)
	now := time.Now().UTC()

	exporter, store := newExporterWithMeterUsage(t)
	seedMeterUsage(t, ctx, store, tenantID, envID, now, "usage-2", 5)

	csvBytes, _, err := exporter.PrepareData(ctx, exportRequest(tenantID, envID, now))
	if err != nil {
		t.Fatalf("PrepareData: %v", err)
	}
	var records []*FeatureUsageCSV
	if err := gocsv.UnmarshalBytes(csvBytes, &records); err != nil {
		t.Fatalf("Unmarshal CSV: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 CSV record, got %d", len(records))
	}
	blank := map[string]string{
		"SubscriptionID":          records[0].SubscriptionID,
		"SubLineItemID":           records[0].SubLineItemID,
		"PriceID":                 records[0].PriceID,
		"CustomerID":              records[0].CustomerID,
		"FeatureID":               records[0].FeatureID,
		"PeriodID":                records[0].PeriodID,
		"ProvisionalUsageCharges": records[0].ProvisionalUsageCharges,
	}
	for field, v := range blank {
		if v != "" {
			t.Errorf("%s: want empty (not derivable from meter_usage), got %q", field, v)
		}
	}
}

// TestEventExporter_CSVHeadersStable verifies the CSV schema is stable —
// downstream consumers depend on the header row not changing across releases,
// including the provisional_usage_charges column (kept in the schema even
// though the value is currently blank).
func TestEventExporter_CSVHeadersStable(t *testing.T) {
	tenantID := "tenant-3"
	envID := "env-3"
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), tenantID), envID)
	now := time.Now().UTC()

	exporter, _ := newExporterWithMeterUsage(t)
	csvBytes, _, err := exporter.PrepareData(ctx, exportRequest(tenantID, envID, now))
	if err != nil {
		t.Fatalf("PrepareData: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(csvBytes)), "\n")
	if len(lines) < 1 {
		t.Fatalf("expected at least a header line")
	}
	headers := lines[0]
	expected := []string{
		"id", "tenant_id", "environment_id", "external_customer_id", "customer_id",
		"subscription_id", "sub_line_item_id", "price_id", "meter_id", "feature_id",
		"event_name", "source", "timestamp", "ingested_at", "period_id",
		"qty_total", "provisional_usage_charges", "properties", "unique_hash",
	}
	for _, col := range expected {
		if !strings.Contains(headers, col) {
			t.Errorf("expected CSV headers to include %q, got: %s", col, headers)
		}
	}
}

// TestEventExporter_PrepareData_EmptyProducesHeaderOnly verifies that an empty
// data set still produces a header-only CSV with count=0 — downstream jobs
// must not choke on the sentinel empty export.
func TestEventExporter_PrepareData_EmptyProducesHeaderOnly(t *testing.T) {
	tenantID := "tenant-4"
	envID := "env-4"
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), tenantID), envID)
	now := time.Now().UTC()

	exporter, _ := newExporterWithMeterUsage(t)
	csvBytes, count, err := exporter.PrepareData(ctx, exportRequest(tenantID, envID, now))
	if err != nil {
		t.Fatalf("PrepareData: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 records, got %d", count)
	}
	lines := strings.Split(strings.TrimSpace(string(csvBytes)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly 1 line (header), got %d: %q", len(lines), lines)
	}
}

// TestEventExporter_PrepareData_BatchesAcrossPages verifies that PrepareData
// paginates via GetMeterUsageForExport (batchSize=500 in impl) — seeding
// > batchSize rows produces the full row count in the output CSV.
func TestEventExporter_PrepareData_BatchesAcrossPages(t *testing.T) {
	tenantID := "tenant-5"
	envID := "env-5"
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), tenantID), envID)
	now := time.Now().UTC()

	exporter, store := newExporterWithMeterUsage(t)
	// impl batchSize is 500; seed 501 to force a second page.
	const total = 501
	records := make([]*events.MeterUsage, 0, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("usage-batch-%d", i)
		records = append(records, &events.MeterUsage{
			Event: events.Event{
				ID:                 id,
				TenantID:           tenantID,
				EnvironmentID:      envID,
				EventName:          "batched_event",
				Timestamp:          now,
				IngestedAt:         now,
				ExternalCustomerID: "cust-batch",
				Properties:         map[string]interface{}{},
			},
			MeterID:    "meter-batch",
			QtyTotal:   decimal.NewFromInt(1),
			UniqueHash: id,
		})
	}
	if err := store.BulkInsertMeterUsage(ctx, records); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, count, err := exporter.PrepareData(ctx, exportRequest(tenantID, envID, now))
	if err != nil {
		t.Fatalf("PrepareData: %v", err)
	}
	if count != total {
		t.Errorf("expected %d records across pages, got %d", total, count)
	}
}
