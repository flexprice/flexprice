package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// UsageAnalyticsGetter avoids an import cycle with the service package
type UsageAnalyticsGetter interface {
	GetDetailedUsageAnalytics(ctx context.Context, req *dto.GetUsageAnalyticsRequest) (*dto.GetUsageAnalyticsResponse, error)
}

// UsageAnalyticsExporter handles customer usage analytics export
type UsageAnalyticsExporter struct {
	customerRepo         customer.Repository
	eventRepo            events.Repository
	usageAnalyticsGetter UsageAnalyticsGetter
	logger               *logger.Logger
}

// NewUsageAnalyticsExporter creates a new usage analytics exporter
func NewUsageAnalyticsExporter(
	customerRepo customer.Repository,
	eventRepo events.Repository,
	usageAnalyticsGetter UsageAnalyticsGetter,
	logger *logger.Logger,
) *UsageAnalyticsExporter {
	return &UsageAnalyticsExporter{
		customerRepo:         customerRepo,
		eventRepo:            eventRepo,
		usageAnalyticsGetter: usageAnalyticsGetter,
		logger:               logger,
	}
}

// UsageAnalyticsCSVHeaders represents the column header strings for the usage analytics CSV export
type UsageAnalyticsCSVHeaders string

const (
	UsageAnalyticsCSVHeadersCustomerID         UsageAnalyticsCSVHeaders = "customer_id"
	UsageAnalyticsCSVHeadersCustomerExternalID UsageAnalyticsCSVHeaders = "customer_external_id"
	UsageAnalyticsCSVHeadersStartTime          UsageAnalyticsCSVHeaders = "start_time"
	UsageAnalyticsCSVHeadersEndTime            UsageAnalyticsCSVHeaders = "end_time"
	UsageAnalyticsCSVHeadersFeatureName        UsageAnalyticsCSVHeaders = "feature_name"
	UsageAnalyticsCSVHeadersFeatureID          UsageAnalyticsCSVHeaders = "feature_id"
	UsageAnalyticsCSVHeadersEventName          UsageAnalyticsCSVHeaders = "event_name"
	UsageAnalyticsCSVHeadersEventCount         UsageAnalyticsCSVHeaders = "event_count"
	UsageAnalyticsCSVHeadersTotalUsage         UsageAnalyticsCSVHeaders = "total_usage"
	UsageAnalyticsCSVHeadersTotalCost          UsageAnalyticsCSVHeaders = "total_cost"
	UsageAnalyticsCSVHeadersCurrency           UsageAnalyticsCSVHeaders = "currency"
)

// usageAnalyticsStaticHeaders is the fixed set of base CSV columns.
var usageAnalyticsStaticHeaders = []string{
	string(UsageAnalyticsCSVHeadersCustomerID),
	string(UsageAnalyticsCSVHeadersCustomerExternalID),
	string(UsageAnalyticsCSVHeadersStartTime),
	string(UsageAnalyticsCSVHeadersEndTime),
	string(UsageAnalyticsCSVHeadersFeatureName),
	string(UsageAnalyticsCSVHeadersFeatureID),
	string(UsageAnalyticsCSVHeadersEventName),
	string(UsageAnalyticsCSVHeadersEventCount),
	string(UsageAnalyticsCSVHeadersTotalUsage),
	string(UsageAnalyticsCSVHeadersTotalCost),
	string(UsageAnalyticsCSVHeadersCurrency),
}

// usageAnalyticsRecord is one row per (customer × feature) before CSV serialisation.
type usageAnalyticsRecord struct {
	CustomerID         string
	CustomerExternalID string
	StartTime          time.Time
	EndTime            time.Time
	FeatureName        string
	FeatureID          string
	EventName          string
	EventCount         int64
	TotalUsage         decimal.Decimal
	TotalCost          decimal.Decimal
	Currency           string
	// CustomerMetadata holds the customer's raw metadata for dynamic column lookup.
	CustomerMetadata types.Metadata
}

// PrepareData lists all customers, fetches analytics per customer, and returns CSV bytes.
// Dynamic metadata columns (customer entity only) are appended after the static columns
// when the caller includes export_metadata_fields in the job config.
func (e *UsageAnalyticsExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	e.logger.Infow("starting usage analytics data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime)

	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	metadataFields := request.JobConfig.GetExportMetadataFields()

	var buf bytes.Buffer
	csvWriter := csv.NewWriter(&buf)

	if err := csvWriter.Write(e.resolveHeaders(metadataFields)); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to write CSV headers").
			Mark(ierr.ErrInternal)
	}

	externalCustomerIDs, err := e.eventRepo.GetDistinctExternalCustomerIDs(ctx, request.StartTime, request.EndTime)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to get distinct external customer ids").
			Mark(ierr.ErrDatabase)
	}

	// if no external customer ids found, return empty CSV with headers only
	if len(externalCustomerIDs) == 0 {
		e.logger.Infow("no external customer ids found, uploading empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID,
			"start_time", request.StartTime,
			"end_time", request.EndTime)
		csvWriter.Flush()
		if err := csvWriter.Error(); err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to flush CSV writer").
				Mark(ierr.ErrInternal)
		}
		return buf.Bytes(), 0, nil
	}

	filter := types.NewCustomerFilter()
	filter.ExternalIDs = externalCustomerIDs

	customers, err := e.customerRepo.ListAll(ctx, filter)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to list customers").
			Mark(ierr.ErrDatabase)
	}

	e.logger.Infow("found customers to process",
		"customer_count", len(customers),
		"tenant_id", request.TenantID,
		"env_id", request.EnvID)

	recordCount := 0
	failedCount := 0
	for _, c := range customers {
		response, err := e.usageAnalyticsGetter.GetDetailedUsageAnalytics(ctx, &dto.GetUsageAnalyticsRequest{
			ExternalCustomerID: c.ExternalID,
			StartTime:          request.StartTime,
			EndTime:            request.EndTime,
		})
		if err != nil {
			failedCount++
			e.logger.Warnw("failed to fetch usage analytics for customer, skipping",
				"customer_id", c.ID,
				"external_id", c.ExternalID,
				"error", err)
			continue
		}

		for _, item := range response.Items {
			record := &usageAnalyticsRecord{
				CustomerID:         c.ID,
				CustomerExternalID: c.ExternalID,
				StartTime:          request.StartTime,
				EndTime:            request.EndTime,
				FeatureName:        item.FeatureName,
				FeatureID:          item.FeatureID,
				EventName:          item.EventName,
				EventCount:         int64(item.EventCount),
				TotalUsage:         item.TotalUsage,
				TotalCost:          item.TotalCost,
				Currency:           item.Currency,
				CustomerMetadata:   c.Metadata,
			}

			if err := csvWriter.Write(e.buildRow(record, metadataFields)); err != nil {
				return nil, 0, ierr.WithError(err).
					WithHint("Failed to write CSV row").
					Mark(ierr.ErrInternal)
			}
			recordCount++
		}
	}

	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to flush CSV writer").
			Mark(ierr.ErrInternal)
	}

	csvBytes := buf.Bytes()

	if recordCount == 0 {
		e.logger.Infow("no usage analytics data found for export - uploading empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID,
			"csv_size_bytes", len(csvBytes))
	} else if failedCount > 0 {
		e.logger.Warnw("usage analytics export completed with partial data",
			"total_customers", len(customers),
			"failed_customers", failedCount,
			"exported_records", recordCount,
			"tenant_id", request.TenantID,
			"env_id", request.EnvID)
	} else {
		e.logger.Infow("completed usage analytics export",
			"total_records", recordCount,
			"csv_size_bytes", len(csvBytes))
	}

	return csvBytes, recordCount, nil
}

// resolveHeaders returns the full CSV header row: static columns followed by one column per
// requested metadata field. f.ColumnName is the user-defined display label.
func (e *UsageAnalyticsExporter) resolveHeaders(metadataFields types.ExportMetadataFields) []string {
	headers := make([]string, len(usageAnalyticsStaticHeaders), len(usageAnalyticsStaticHeaders)+len(metadataFields))
	copy(headers, usageAnalyticsStaticHeaders)
	for _, f := range metadataFields {
		colName := f.ColumnName
		if colName == "" {
			colName = f.FieldKey
		}
		headers = append(headers, colName)
	}
	return headers
}

// buildRow converts one usageAnalyticsRecord into a CSV row aligned with resolveHeaders.
// Only customer metadata is supported as a dynamic column source.
// Absent keys produce an empty string cell.
func (e *UsageAnalyticsExporter) buildRow(record *usageAnalyticsRecord, metadataFields types.ExportMetadataFields) []string {
	row := make([]string, 0, len(usageAnalyticsStaticHeaders)+len(metadataFields))
	row = append(row,
		record.CustomerID,
		record.CustomerExternalID,
		record.StartTime.Format(time.RFC3339),
		record.EndTime.Format(time.RFC3339),
		record.FeatureName,
		record.FeatureID,
		record.EventName,
		strconv.FormatInt(record.EventCount, 10),
		record.TotalUsage.String(),
		record.TotalCost.String(),
		record.Currency,
	)
	for _, f := range metadataFields {
		var val string
		if f.EntityType == types.ExportMetadataEntityTypeCustomer {
			val = record.CustomerMetadata[f.FieldKey]
		}
		row = append(row, val)
	}
	return row
}

// GetFilenamePrefix returns the S3 filename prefix for this export type
func (e *UsageAnalyticsExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeUsageAnalytics)
}
