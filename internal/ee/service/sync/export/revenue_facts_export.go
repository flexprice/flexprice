package export

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
	"github.com/samber/lo"
)

// RevenueFactsExporter exports revenue_facts rows recomputed inside the task
// window — the scheduled counterpart of the pull API, so tenants receive a
// daily CSV in their own S3. Rows re-export whenever they are recomputed;
// consumers keep the latest version per id.
type RevenueFactsExporter struct {
	revenueFactRepo revenuefact.Repository
	logger          *logger.Logger
}

// RevenueFactCSV is the export row. Column meanings: docs/export/revenue-facts.md.
type RevenueFactCSV struct {
	ID                string `csv:"id"`
	CustomerID        string `csv:"customer_id"`
	SubscriptionID    string `csv:"subscription_id"`
	SubLineItemID     string `csv:"sub_line_item_id"`
	PriceID           string `csv:"price_id"`
	MeterID           string `csv:"meter_id"`
	AggregationType   string `csv:"aggregation_type"`
	RevenueSource     string `csv:"revenue_source"`
	PeriodStart       string `csv:"period_start"` // YYYY-MM-DD
	PeriodEnd         string `csv:"period_end"`   // YYYY-MM-DD, inclusive
	Day               string `csv:"day"`          // YYYY-MM-DD
	UsageAtListRate   string `csv:"usage_at_list_rate"`
	TierDelta         string `csv:"tier_delta"`
	EntitlementAmount string `csv:"entitlement_amount"`
	LineDiscount      string `csv:"line_discount"`
	InvoiceDiscount   string `csv:"invoice_discount"`
	NetAmount         string `csv:"net_amount"`
	BillableQty       string `csv:"billable_qty"`
	EntitlementQty    string `csv:"entitlement_qty"`
	DecompositionMode string `csv:"decomposition_mode"`
	Currency          string `csv:"currency"`
	Status            string `csv:"status"`
	IsRevert          bool   `csv:"is_revert"`
	InvoiceID         string `csv:"invoice_id"`
	InvoiceLineItemID string `csv:"invoice_line_item_id"`
	ComputedAt        string `csv:"computed_at"` // RFC3339Nano — the export watermark
	Version           int64  `csv:"version"`
}

// NewRevenueFactsExporter creates a new revenue facts exporter.
func NewRevenueFactsExporter(revenueFactRepo revenuefact.Repository, logger *logger.Logger) *RevenueFactsExporter {
	return &RevenueFactsExporter{revenueFactRepo: revenueFactRepo, logger: logger}
}

// PrepareData pages facts recomputed inside [StartTime, EndTime] and converts
// them to CSV. Pagination keys on (computed_at, id), so an interrupted run
// re-executed with the same window is deterministic.
func (e *RevenueFactsExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	const batchSize = 1000
	const dayLayout = "2006-01-02"

	e.logger.Info(ctx, "starting revenue facts export fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime)

	var csvRows []*RevenueFactCSV
	since, afterID := request.StartTime, ""
	for {
		facts, err := e.revenueFactRepo.ListForExport(ctx, since, afterID, batchSize)
		if err != nil {
			return nil, 0, err
		}
		done := len(facts) < batchSize
		for _, f := range facts {
			if !request.EndTime.IsZero() && f.ComputedAt.After(request.EndTime) {
				// Rows past the window belong to the next scheduled run.
				done = true
				break
			}
			csvRows = append(csvRows, &RevenueFactCSV{
				ID:                f.ID,
				CustomerID:        f.CustomerID,
				SubscriptionID:    f.SubscriptionID,
				SubLineItemID:     lo.FromPtr(f.SubLineItemID),
				PriceID:           lo.FromPtr(f.PriceID),
				MeterID:           lo.FromPtr(f.MeterID),
				AggregationType:   string(lo.FromPtr(f.AggregationType)),
				RevenueSource:     string(f.RevenueSource),
				PeriodStart:       f.PeriodStart.Format(dayLayout),
				PeriodEnd:         f.PeriodEnd.Format(dayLayout),
				Day:               f.Day.Format(dayLayout),
				UsageAtListRate:   f.UsageAtListRate.String(),
				TierDelta:         f.TierDelta.String(),
				EntitlementAmount: f.EntitlementAmount.String(),
				LineDiscount:      f.LineDiscount.String(),
				InvoiceDiscount:   f.InvoiceDiscount.String(),
				NetAmount:         f.NetAmount.String(),
				BillableQty:       f.BillableQty.String(),
				EntitlementQty:    f.EntitlementQty.String(),
				DecompositionMode: string(f.DecompositionMode),
				Currency:          f.Currency,
				Status:            string(f.Status),
				IsRevert:          f.IsRevert,
				InvoiceID:         lo.FromPtr(f.InvoiceID),
				InvoiceLineItemID: lo.FromPtr(f.InvoiceLineItemID),
				ComputedAt:        f.ComputedAt.UTC().Format(time.RFC3339Nano),
				Version:           f.Version,
			})
			since, afterID = f.ComputedAt, f.ID
		}
		if done {
			break
		}
	}

	csvData, err := gocsv.MarshalBytes(&csvRows)
	if err != nil {
		return nil, 0, err
	}

	e.logger.Info(ctx, "revenue facts export prepared",
		"tenant_id", request.TenantID,
		"record_count", len(csvRows))
	return csvData, len(csvRows), nil
}

// GetFilenamePrefix returns the prefix for the exported file.
func (e *RevenueFactsExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeRevenueFacts)
}
