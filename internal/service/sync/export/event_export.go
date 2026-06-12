package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
	"github.com/samber/lo"
)

// EventExporter handles feature usage export operations
type EventExporter struct {
	featureUsageRepo   events.FeatureUsageRepository
	meterUsageRepo     events.MeterUsageRepository
	priceRepo          price.Repository
	integrationFactory *integration.Factory
	config             *config.Configuration
	logger             *logger.Logger
}

// FeatureUsageCSV represents the CSV structure for feature usage export
type FeatureUsageCSV struct {
	ID                      string `csv:"id"`
	TenantID                string `csv:"tenant_id"`
	EnvironmentID           string `csv:"environment_id"`
	ExternalCustomerID      string `csv:"external_customer_id"`
	CustomerID              string `csv:"customer_id"`
	SubscriptionID          string `csv:"subscription_id"`
	SubLineItemID           string `csv:"sub_line_item_id"`
	PriceID                 string `csv:"price_id"`
	MeterID                 string `csv:"meter_id"`
	FeatureID               string `csv:"feature_id"`
	EventName               string `csv:"event_name"`
	Source                  string `csv:"source"`
	Timestamp               string `csv:"timestamp"`                 // RFC3339 format
	IngestedAt              string `csv:"ingested_at"`               // RFC3339 format
	PeriodID                string `csv:"period_id"`                 // Billing period ID (uint64 as string)
	QtyTotal                string `csv:"qty_total"`                 // Total quantity (decimal as string)
	ProvisionalUsageCharges string `csv:"provisional_usage_charges"` // price.Amount * quantity (decimal as string)
	Properties              string `csv:"properties"`                // Event properties as JSON string
	UniqueHash              string `csv:"unique_hash"`               // Deduplication hash
}

// NewEventExporter creates a new event exporter
func NewEventExporter(
	featureUsageRepo events.FeatureUsageRepository,
	meterUsageRepo events.MeterUsageRepository,
	priceRepo price.Repository,
	integrationFactory *integration.Factory,
	cfg *config.Configuration,
	logger *logger.Logger,
) *EventExporter {
	return &EventExporter{
		featureUsageRepo:   featureUsageRepo,
		meterUsageRepo:     meterUsageRepo,
		priceRepo:          priceRepo,
		integrationFactory: integrationFactory,
		config:             cfg,
		logger:             logger,
	}
}

// PrepareData fetches feature usage data in batches and converts it to CSV format
func (e *EventExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	const batchSize = 500

	useMeterUsage := e.config != nil && e.config.FeatureFlag.IsMeterUsageEnabledForAnalytics(request.TenantID)

	e.logger.Info(ctx, "starting batched feature usage data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime,
		"batch_size", batchSize,
		"source_table", lo.Ternary(useMeterUsage, "meter_usage", "feature_usage"))

	if useMeterUsage {
		return e.prepareDataFromMeterUsage(ctx, request, batchSize)
	}

	// Collect all CSV records
	var csvRecords []*FeatureUsageCSV
	totalRecords := 0
	offset := 0

	// Fetch and process data in batches
	for {
		e.logger.Debug(ctx, "fetching batch",
			"offset", offset,
			"batch_size", batchSize)

		usageData, err := e.featureUsageRepo.GetFeatureUsageForExport(
			ctx,
			request.StartTime,
			request.EndTime,
			batchSize,
			offset,
		)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to fetch feature usage data batch").
				WithReportableDetails(map[string]interface{}{
					"offset":     offset,
					"batch_size": batchSize,
				}).
				Mark(ierr.ErrDatabase)
		}

		// If no data returned, we've reached the end
		if len(usageData) == 0 {
			break
		}

		e.logger.Debug(ctx, "fetched batch",
			"offset", offset,
			"records_in_batch", len(usageData),
			"total_so_far", totalRecords+len(usageData))

		// Collect unique price IDs from this batch (skip empty)
		priceIDSet := make(map[string]struct{})
		for _, u := range usageData {
			if u.PriceID != "" {
				priceIDSet[u.PriceID] = struct{}{}
			}
		}
		uniquePriceIDs := lo.Keys(priceIDSet)

		var priceByID map[string]*price.Price
		if len(uniquePriceIDs) > 0 {
			prices, listErr := e.priceRepo.List(ctx, types.NewNoLimitPriceFilter().WithPriceIDs(uniquePriceIDs))
			if listErr != nil {
				return nil, 0, ierr.WithError(listErr).
					WithHint("Failed to fetch prices for export batch").
					WithReportableDetails(map[string]interface{}{
						"offset":    offset,
						"price_ids": len(uniquePriceIDs),
					}).
					Mark(ierr.ErrDatabase)
			}
			priceByID = make(map[string]*price.Price, len(prices))
			for _, p := range prices {
				priceByID[p.ID] = p
			}
		} else {
			priceByID = nil
		}

		// Convert batch to CSV records
		batchRecords, err := e.convertToCSVRecords(usageData, priceByID)
		if err != nil {
			return nil, 0, err
		}
		csvRecords = append(csvRecords, batchRecords...)

		totalRecords += len(usageData)
		offset += batchSize

		// If we got fewer records than batch size, we've reached the end
		if len(usageData) < batchSize {
			break
		}
	}

	// Marshal to CSV using gocsv
	var buf bytes.Buffer
	if err := gocsv.Marshal(csvRecords, &buf); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to marshal data to CSV").
			Mark(ierr.ErrInternal)
	}

	csvBytes := buf.Bytes()

	if totalRecords == 0 {
		e.logger.Info(ctx, "no feature usage data found for export - will upload empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID,
			"csv_size_bytes", len(csvBytes))
	} else {
		e.logger.Info(ctx, "completed batched data fetch and CSV conversion",
			"total_records", totalRecords,
			"csv_size_bytes", len(csvBytes))
	}

	return csvBytes, totalRecords, nil
}

// prepareDataFromMeterUsage fetches rows from the meter_usage table and maps each
// into the existing FeatureUsageCSV schema. Columns that meter_usage does not
// carry (subscription_id, sub_line_item_id, price_id, customer_id, feature_id,
// period_id, provisional_usage_charges) are left as empty strings — the schema
// stays stable so downstream consumers don't see a header-row change.
func (e *EventExporter) prepareDataFromMeterUsage(ctx context.Context, request *dto.ExportRequest, batchSize int) ([]byte, int, error) {
	var csvRecords []*FeatureUsageCSV
	totalRecords := 0
	offset := 0

	for {
		e.logger.Debug(ctx, "fetching meter_usage batch",
			"offset", offset,
			"batch_size", batchSize)

		usageData, err := e.meterUsageRepo.GetMeterUsageForExport(
			ctx,
			request.StartTime,
			request.EndTime,
			batchSize,
			offset,
		)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to fetch meter usage data batch").
				WithReportableDetails(map[string]interface{}{
					"offset":     offset,
					"batch_size": batchSize,
				}).
				Mark(ierr.ErrDatabase)
		}

		if len(usageData) == 0 {
			break
		}

		batchRecords, err := e.convertMeterUsageToCSVRecords(usageData)
		if err != nil {
			return nil, 0, err
		}
		csvRecords = append(csvRecords, batchRecords...)

		totalRecords += len(usageData)
		offset += batchSize

		if len(usageData) < batchSize {
			break
		}
	}

	var buf bytes.Buffer
	if err := gocsv.Marshal(csvRecords, &buf); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to marshal data to CSV").
			Mark(ierr.ErrInternal)
	}
	csvBytes := buf.Bytes()

	if totalRecords == 0 {
		e.logger.Info(ctx, "no meter usage data found for export - will upload empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID,
			"csv_size_bytes", len(csvBytes))
	} else {
		e.logger.Info(ctx, "completed batched meter_usage fetch and CSV conversion",
			"total_records", totalRecords,
			"csv_size_bytes", len(csvBytes))
	}

	return csvBytes, totalRecords, nil
}

// convertMeterUsageToCSVRecords maps MeterUsage rows into FeatureUsageCSV with
// feature_usage-only columns left blank.
func (e *EventExporter) convertMeterUsageToCSVRecords(usageData []*events.MeterUsage) ([]*FeatureUsageCSV, error) {
	records := make([]*FeatureUsageCSV, 0, len(usageData))

	for _, usage := range usageData {
		propertiesJSON, err := json.Marshal(usage.Properties)
		if err != nil {
			e.logger.Info(context.Background(), "failed to marshal properties, using empty object",
				"usage_id", usage.ID,
				"error", err)
			propertiesJSON = []byte("{}")
		}

		records = append(records, &FeatureUsageCSV{
			ID:                 usage.ID,
			TenantID:           usage.TenantID,
			EnvironmentID:      usage.EnvironmentID,
			ExternalCustomerID: usage.ExternalCustomerID,
			MeterID:            usage.MeterID,
			EventName:          usage.EventName,
			Source:             usage.Source,
			Timestamp:          usage.Timestamp.Format(time.RFC3339),
			IngestedAt:         usage.IngestedAt.Format(time.RFC3339),
			QtyTotal:           usage.QtyTotal.String(),
			Properties:         string(propertiesJSON),
			UniqueHash:         usage.UniqueHash,
		})
	}

	return records, nil
}

// convertToCSVRecords converts FeatureUsage domain models to CSV records
func (e *EventExporter) convertToCSVRecords(usageData []*events.FeatureUsage, priceByID map[string]*price.Price) ([]*FeatureUsageCSV, error) {
	records := make([]*FeatureUsageCSV, 0, len(usageData))

	for _, usage := range usageData {
		// Convert properties map to JSON string
		propertiesJSON, err := json.Marshal(usage.Properties)
		if err != nil {
			e.logger.Info(context.Background(), "failed to marshal properties, using empty object",
				"usage_id", usage.ID,
				"error", err)
			propertiesJSON = []byte("{}")
		}

		provisionalUsageChargesStr := ""
		if usage.PriceID != "" && priceByID != nil {
			if p, ok := priceByID[usage.PriceID]; ok {
				provisionalUsageChargesStr = p.Amount.Mul(usage.QtyTotal).String()
			}
		}

		record := &FeatureUsageCSV{
			ID:                      usage.ID,
			TenantID:                usage.TenantID,
			EnvironmentID:           usage.EnvironmentID,
			ExternalCustomerID:      usage.ExternalCustomerID,
			CustomerID:              usage.CustomerID,
			SubscriptionID:          usage.SubscriptionID,
			SubLineItemID:           usage.SubLineItemID,
			PriceID:                 usage.PriceID,
			MeterID:                 usage.MeterID,
			FeatureID:               usage.FeatureID,
			EventName:               usage.EventName,
			Source:                  usage.Source,
			Timestamp:               usage.Timestamp.Format(time.RFC3339),
			IngestedAt:              usage.IngestedAt.Format(time.RFC3339),
			PeriodID:                fmt.Sprintf("%d", usage.PeriodID),
			QtyTotal:                usage.QtyTotal.String(),
			ProvisionalUsageCharges: provisionalUsageChargesStr,
			Properties:              string(propertiesJSON),
			UniqueHash:              usage.UniqueHash,
		}

		records = append(records, record)
	}

	return records, nil
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *EventExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeEvents)
}
