package export

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
)

// EventExporter handles meter usage export operations
type EventExporter struct {
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
	meterUsageRepo events.MeterUsageRepository,
	priceRepo price.Repository,
	integrationFactory *integration.Factory,
	cfg *config.Configuration,
	logger *logger.Logger,
) *EventExporter {
	return &EventExporter{
		meterUsageRepo:     meterUsageRepo,
		priceRepo:          priceRepo,
		integrationFactory: integrationFactory,
		config:             cfg,
		logger:             logger,
	}
}

// PrepareData fetches meter usage data in batches and converts it to CSV format.
func (e *EventExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	var buf bytes.Buffer
	totalRecords, err := e.WriteData(ctx, request, &buf)
	if err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), totalRecords, nil
}

// WriteData writes meter usage as CSV in batches, preserving the FeatureUsageCSV schema.
// Fields unavailable in meter_usage remain empty.
func (e *EventExporter) WriteData(ctx context.Context, request *dto.ExportRequest, w io.Writer) (int, error) {
	const batchSize = 500

	e.logger.Info(ctx, "starting batched meter usage data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime,
		"batch_size", batchSize)

	rowCount, err := e.meterUsageRepo.CountMeterUsageForExport(ctx, request.StartTime, request.EndTime)
	if err != nil {
		return 0, err
	}
	if err := enforceExportVolume(ctx, e.logger, request.EntityType, request.TenantID, request.StartTime, request.EndTime, rowCount); err != nil {
		return 0, err
	}

	csvWriter := gocsv.DefaultCSVWriter(w)
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
			return 0, ierr.WithError(err).
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
			return 0, err
		}
		// The header row goes out with the first batch only.
		if totalRecords == 0 {
			err = gocsv.MarshalCSV(batchRecords, csvWriter)
		} else {
			err = gocsv.MarshalCSVWithoutHeaders(batchRecords, csvWriter)
		}
		if err != nil {
			return 0, ierr.WithError(err).
				WithHint("Failed to marshal data to CSV").
				Mark(ierr.ErrInternal)
		}

		totalRecords += len(usageData)
		offset += batchSize

		if len(usageData) < batchSize {
			break
		}
	}

	if totalRecords == 0 {
		if err := gocsv.MarshalCSV([]*FeatureUsageCSV{}, csvWriter); err != nil {
			return 0, ierr.WithError(err).
				WithHint("Failed to marshal data to CSV").
				Mark(ierr.ErrInternal)
		}
		e.logger.Info(ctx, "no meter usage data found for export - will upload empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID)
	} else {
		e.logger.Info(ctx, "completed batched meter_usage fetch and CSV conversion",
			"total_records", totalRecords)
	}

	return totalRecords, nil
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

// GetFilenamePrefix returns the prefix for the exported file
func (e *EventExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeEvents)
}
