package export

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
	"github.com/shopspring/decimal"
)

func TestEventExporter_PrepareData_CostColumn(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-1"
	envID := "env-1"
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, envID)

	featureUsageStore := testutil.NewInMemoryFeatureUsageStore()
	priceStore := testutil.NewInMemoryPriceStore()
	log := logger.GetLogger()
	now := time.Now().UTC()

	// Create a price: amount 2.5
	priceID := "price-export-1"
	priceAmount := decimal.NewFromFloat(2.5)
	err := priceStore.Create(ctx, &price.Price{
		ID:             priceID,
		EnvironmentID:  envID,
		Amount:         priceAmount,
		Currency:       "usd",
		Type:           types.PRICE_TYPE_USAGE,
		MeterID:        "meter-1",
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		DisplayName:    "Usage price",
		EntityType:     types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:       "plan-1",
		InvoiceCadence: types.InvoiceCadenceArrear,
		BaseModel:      types.BaseModel{TenantID: tenantID, Status: types.StatusPublished, CreatedAt: now, UpdatedAt: now},
	})
	if err != nil {
		t.Fatalf("setup price: %v", err)
	}

	// Create feature usage with quantity 3 -> expected cost = 2.5 * 3 = 7.5
	usageID := "usage-1"
	qty := decimal.NewFromInt(3)
	usage := &events.FeatureUsage{
		Event: events.Event{
			ID:                 usageID,
			TenantID:           tenantID,
			EnvironmentID:      envID,
			EventName:          "test_event",
			Source:             "test",
			Timestamp:          now,
			IngestedAt:         now,
			ExternalCustomerID: "cust-1",
			CustomerID:         "customer-1",
			Properties:         map[string]interface{}{},
		},
		SubscriptionID: "sub-1",
		SubLineItemID:  "item-1",
		PriceID:        priceID,
		MeterID:        "meter-1",
		FeatureID:      "feature-1",
		PeriodID:       1,
		UniqueHash:     "hash-1",
		QtyTotal:       qty,
		Sign:           1,
	}
	err = featureUsageStore.InsertProcessedEvent(ctx, usage)
	if err != nil {
		t.Fatalf("setup usage: %v", err)
	}

	exporter := NewEventExporter(featureUsageStore, priceStore, nil, log)
	req := &dto.ExportRequest{
		TenantID:    tenantID,
		EnvID:       envID,
		StartTime:   now.Add(-time.Hour),
		EndTime:     now.Add(time.Hour),
		EntityType:  types.ScheduledTaskEntityTypeEvents,
		JobConfig:   &types.S3JobConfig{},
	}

	csvBytes, count, err := exporter.PrepareData(ctx, req)
	if err != nil {
		t.Fatalf("PrepareData: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 record, got %d", count)
	}

	// Parse CSV and check Cost column
	var records []*FeatureUsageCSV
	if err := gocsv.UnmarshalBytes(csvBytes, &records); err != nil {
		t.Fatalf("Unmarshal CSV: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 CSV record, got %d", len(records))
	}
	// cost = 2.5 * 3 = 7.5
	if records[0].Cost != "7.5" {
		t.Errorf("expected Cost 7.5, got %q", records[0].Cost)
	}
	if records[0].PriceID != priceID {
		t.Errorf("expected PriceID %q, got %q", priceID, records[0].PriceID)
	}
}

func TestEventExporter_PrepareData_CostEmptyWhenPriceIDEmpty(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-2"
	envID := "env-2"
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, envID)

	featureUsageStore := testutil.NewInMemoryFeatureUsageStore()
	priceStore := testutil.NewInMemoryPriceStore()
	log := logger.GetLogger()

	// Usage with empty PriceID
	usageID := "usage-no-price"
	now := time.Now().UTC()
	usage := &events.FeatureUsage{
		Event: events.Event{
			ID:                 usageID,
			TenantID:           tenantID,
			EnvironmentID:      envID,
			EventName:          "test_event",
			Source:             "test",
			Timestamp:          now,
			IngestedAt:         now,
			ExternalCustomerID: "cust-1",
			CustomerID:         "customer-1",
			Properties:         map[string]interface{}{},
		},
		SubscriptionID: "sub-1",
		SubLineItemID:  "item-1",
		PriceID:        "", // empty
		MeterID:        "meter-1",
		FeatureID:      "feature-1",
		PeriodID:       1,
		UniqueHash:     "hash-2",
		QtyTotal:       decimal.NewFromInt(5),
		Sign:           1,
	}
	err := featureUsageStore.InsertProcessedEvent(ctx, usage)
	if err != nil {
		t.Fatalf("setup usage: %v", err)
	}

	exporter := NewEventExporter(featureUsageStore, priceStore, nil, log)
	req := &dto.ExportRequest{
		TenantID:   tenantID,
		EnvID:      envID,
		StartTime:  now.Add(-time.Hour),
		EndTime:    now.Add(time.Hour),
		EntityType: types.ScheduledTaskEntityTypeEvents,
		JobConfig:  &types.S3JobConfig{},
	}

	csvBytes, count, err := exporter.PrepareData(ctx, req)
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
	if records[0].Cost != "" {
		t.Errorf("expected empty Cost when price_id is empty, got %q", records[0].Cost)
	}
}

func TestEventExporter_PrepareData_CostEmptyWhenPriceNotFound(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-3"
	envID := "env-3"
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, envID)

	featureUsageStore := testutil.NewInMemoryFeatureUsageStore()
	priceStore := testutil.NewInMemoryPriceStore() // no prices created
	log := logger.GetLogger()

	// Usage with non-existent PriceID
	usageID := "usage-missing-price"
	now := time.Now().UTC()
	usage := &events.FeatureUsage{
		Event: events.Event{
			ID:                 usageID,
			TenantID:           tenantID,
			EnvironmentID:      envID,
			EventName:          "test_event",
			Source:             "test",
			Timestamp:          now,
			IngestedAt:         now,
			ExternalCustomerID: "cust-1",
			CustomerID:         "customer-1",
			Properties:         map[string]interface{}{},
		},
		SubscriptionID: "sub-1",
		SubLineItemID:  "item-1",
		PriceID:        "non-existent-price-id",
		MeterID:        "meter-1",
		FeatureID:      "feature-1",
		PeriodID:       1,
		UniqueHash:     "hash-3",
		QtyTotal:       decimal.NewFromInt(10),
		Sign:           1,
	}
	err := featureUsageStore.InsertProcessedEvent(ctx, usage)
	if err != nil {
		t.Fatalf("setup usage: %v", err)
	}

	exporter := NewEventExporter(featureUsageStore, priceStore, nil, log)
	req := &dto.ExportRequest{
		TenantID:   tenantID,
		EnvID:      envID,
		StartTime:  now.Add(-time.Hour),
		EndTime:    now.Add(time.Hour),
		EntityType: types.ScheduledTaskEntityTypeEvents,
		JobConfig:  &types.S3JobConfig{},
	}

	csvBytes, count, err := exporter.PrepareData(ctx, req)
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
	// Cost should be empty when price is not found (no error, graceful degradation)
	if records[0].Cost != "" {
		t.Errorf("expected empty Cost when price not found, got %q", records[0].Cost)
	}
}

func TestEventExporter_CSVHeadersIncludeCost(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-4"
	envID := "env-4"
	ctx = types.SetTenantID(ctx, tenantID)
	ctx = types.SetEnvironmentID(ctx, envID)

	featureUsageStore := testutil.NewInMemoryFeatureUsageStore()
	priceStore := testutil.NewInMemoryPriceStore()
	log := logger.GetLogger()

	exporter := NewEventExporter(featureUsageStore, priceStore, nil, log)
	now := time.Now().UTC()
	req := &dto.ExportRequest{
		TenantID:   tenantID,
		EnvID:      envID,
		StartTime:  now.Add(-time.Hour),
		EndTime:    now.Add(time.Hour),
		EntityType: types.ScheduledTaskEntityTypeEvents,
		JobConfig:  &types.S3JobConfig{},
	}

	csvBytes, _, err := exporter.PrepareData(ctx, req)
	if err != nil {
		t.Fatalf("PrepareData: %v", err)
	}
	// Empty export still produces CSV with headers
	lines := strings.Split(strings.TrimSpace(string(csvBytes)), "\n")
	if len(lines) < 1 {
		t.Fatalf("expected at least header line")
	}
	headers := lines[0]
	if !strings.Contains(headers, "cost") {
		t.Errorf("expected CSV headers to include 'cost', got: %s", headers)
	}
}
