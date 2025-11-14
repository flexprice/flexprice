package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
)

// InvoiceExporter handles invoice export operations
type InvoiceExporter struct {
	invoiceRepo        invoice.Repository
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

// InvoiceCSV represents the CSV structure for invoice export
type InvoiceCSV struct {
	ID               string `csv:"id"`
	TenantID         string `csv:"tenant_id"`
	EnvironmentID    string `csv:"environment_id"`
	CustomerID       string `csv:"customer_id"`
	SubscriptionID   string `csv:"subscription_id"`
	InvoiceNumber    string `csv:"invoice_number"`
	InvoiceType      string `csv:"invoice_type"`
	InvoiceStatus    string `csv:"invoice_status"`
	PaymentStatus    string `csv:"payment_status"`
	Currency         string `csv:"currency"`
	AmountDue        string `csv:"amount_due"`        // Decimal as string
	AmountPaid       string `csv:"amount_paid"`       // Decimal as string
	AmountRemaining  string `csv:"amount_remaining"`  // Decimal as string
	Subtotal         string `csv:"subtotal"`          // Decimal as string
	Total            string `csv:"total"`             // Decimal as string
	TotalDiscount    string `csv:"total_discount"`    // Decimal as string
	TotalTax         string `csv:"total_tax"`         // Decimal as string
	AdjustmentAmount string `csv:"adjustment_amount"` // Decimal as string
	RefundedAmount   string `csv:"refunded_amount"`   // Decimal as string
	BillingSequence  string `csv:"billing_sequence"`  // Integer as string
	Description      string `csv:"description"`
	DueDate          string `csv:"due_date"`     // RFC3339 format
	PaidAt           string `csv:"paid_at"`      // RFC3339 format
	VoidedAt         string `csv:"voided_at"`    // RFC3339 format
	FinalizedAt      string `csv:"finalized_at"` // RFC3339 format
	BillingPeriod    string `csv:"billing_period"`
	PeriodStart      string `csv:"period_start"` // RFC3339 format
	PeriodEnd        string `csv:"period_end"`   // RFC3339 format
	InvoicePDFURL    string `csv:"invoice_pdf_url"`
	BillingReason    string `csv:"billing_reason"`
	Metadata         string `csv:"metadata"` // JSON string
	IdempotencyKey   string `csv:"idempotency_key"`
	Version          string `csv:"version"` // Integer as string
	Status           string `csv:"status"`
	CreatedBy        string `csv:"created_by"`
	UpdatedBy        string `csv:"updated_by"`
	CreatedAt        string `csv:"created_at"` // RFC3339 format
	UpdatedAt        string `csv:"updated_at"` // RFC3339 format
}

// NewInvoiceExporter creates a new invoice exporter
func NewInvoiceExporter(
	invoiceRepo invoice.Repository,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *InvoiceExporter {
	return &InvoiceExporter{
		invoiceRepo:        invoiceRepo,
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

// PrepareData fetches invoice data in batches and converts it to CSV format
func (e *InvoiceExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	const batchSize = 500

	e.logger.Infow("starting batched invoice data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime,
		"batch_size", batchSize)

	// Collect all CSV records
	var csvRecords []*InvoiceCSV
	totalRecords := 0
	offset := 0

	// Fetch and process data in batches
	for {
		e.logger.Debugw("fetching batch",
			"offset", offset,
			"batch_size", batchSize)

		invoiceData, err := e.invoiceRepo.GetInvoicesForExport(
			ctx,
			request.TenantID,
			request.EnvID,
			request.StartTime,
			request.EndTime,
			batchSize,
			offset,
		)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to fetch invoice data batch").
				WithReportableDetails(map[string]interface{}{
					"offset":     offset,
					"batch_size": batchSize,
				}).
				Mark(ierr.ErrDatabase)
		}

		// If no data returned, we've reached the end
		if len(invoiceData) == 0 {
			break
		}

		e.logger.Debugw("fetched batch",
			"offset", offset,
			"records_in_batch", len(invoiceData),
			"total_so_far", totalRecords+len(invoiceData))

		// Convert batch to CSV records
		batchRecords, err := e.convertToCSVRecords(invoiceData)
		if err != nil {
			return nil, 0, err
		}
		csvRecords = append(csvRecords, batchRecords...)

		totalRecords += len(invoiceData)
		offset += batchSize

		// If we got fewer records than batch size, we've reached the end
		if len(invoiceData) < batchSize {
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
		e.logger.Infow("no invoice data found for export - will upload empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID,
			"csv_size_bytes", len(csvBytes))
	} else {
		e.logger.Infow("completed batched data fetch and CSV conversion",
			"total_records", totalRecords,
			"csv_size_bytes", len(csvBytes))
	}

	return csvBytes, totalRecords, nil
}

// convertToCSVRecords converts Invoice domain models to CSV records
func (e *InvoiceExporter) convertToCSVRecords(invoices []*invoice.Invoice) ([]*InvoiceCSV, error) {
	records := make([]*InvoiceCSV, 0, len(invoices))

	for _, inv := range invoices {
		// Convert metadata map to JSON string
		metadataJSON, err := json.Marshal(inv.Metadata)
		if err != nil {
			e.logger.Warnw("failed to marshal metadata, using empty object",
				"invoice_id", inv.ID,
				"error", err)
			metadataJSON = []byte("{}")
		}

		record := &InvoiceCSV{
			ID:               inv.ID,
			TenantID:         inv.TenantID,
			EnvironmentID:    inv.EnvironmentID,
			CustomerID:       inv.CustomerID,
			SubscriptionID:   stringOrEmpty(inv.SubscriptionID),
			InvoiceNumber:    stringOrEmpty(inv.InvoiceNumber),
			InvoiceType:      string(inv.InvoiceType),
			InvoiceStatus:    string(inv.InvoiceStatus),
			PaymentStatus:    string(inv.PaymentStatus),
			Currency:         inv.Currency,
			AmountDue:        inv.AmountDue.String(),
			AmountPaid:       inv.AmountPaid.String(),
			AmountRemaining:  inv.AmountRemaining.String(),
			Subtotal:         inv.Subtotal.String(),
			Total:            inv.Total.String(),
			TotalDiscount:    inv.TotalDiscount.String(),
			TotalTax:         inv.TotalTax.String(),
			AdjustmentAmount: inv.AdjustmentAmount.String(),
			RefundedAmount:   inv.RefundedAmount.String(),
			BillingSequence:  intOrEmpty(inv.BillingSequence),
			Description:      inv.Description,
			DueDate:          timeOrEmpty(inv.DueDate),
			PaidAt:           timeOrEmpty(inv.PaidAt),
			VoidedAt:         timeOrEmpty(inv.VoidedAt),
			FinalizedAt:      timeOrEmpty(inv.FinalizedAt),
			BillingPeriod:    stringOrEmpty(inv.BillingPeriod),
			PeriodStart:      timeOrEmpty(inv.PeriodStart),
			PeriodEnd:        timeOrEmpty(inv.PeriodEnd),
			InvoicePDFURL:    stringOrEmpty(inv.InvoicePDFURL),
			BillingReason:    inv.BillingReason,
			Metadata:         string(metadataJSON),
			IdempotencyKey:   stringOrEmpty(inv.IdempotencyKey),
			Version:          fmt.Sprintf("%d", inv.Version),
			Status:           string(inv.Status),
			CreatedBy:        inv.CreatedBy,
			UpdatedBy:        inv.UpdatedBy,
			CreatedAt:        inv.CreatedAt.Format(time.RFC3339),
			UpdatedAt:        inv.UpdatedAt.Format(time.RFC3339),
		}

		records = append(records, record)
	}

	return records, nil
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *InvoiceExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeInvoice)
}

// Helper functions to convert pointer types to strings
func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intOrEmpty(i *int) string {
	if i == nil {
		return ""
	}
	return fmt.Sprintf("%d", *i)
}

func timeOrEmpty(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
