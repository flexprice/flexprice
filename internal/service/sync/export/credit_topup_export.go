package export

import (
	"bytes"
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
)

// CreditTopupExporter handles credit topup export operations
type CreditTopupExporter struct {
	walletRepo         wallet.Repository
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

// CreditTopupCSV represents the CSV structure for credit topup export
type CreditTopupCSV struct {
	TopupID             string `csv:"topup_id"`
	ExternalID          string `csv:"external_id"`
	CustomerName        string `csv:"name"`
	WalletID            string `csv:"wallet_id"`
	Amount              string `csv:"amount"`                // Decimal as string
	CreditBalanceBefore string `csv:"credit_balance_before"` // Decimal as string
	CreditBalanceAfter  string `csv:"credit_balance_after"`  // Decimal as string
	ReferenceID         string `csv:"reference_id"`
	TransactionReason   string `csv:"transaction_reason"`
	CreatedAt           string `csv:"created_at"` // RFC3339 format
}

// NewCreditTopupExporter creates a new credit topup exporter
func NewCreditTopupExporter(
	walletRepo wallet.Repository,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *CreditTopupExporter {
	return &CreditTopupExporter{
		walletRepo:         walletRepo,
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

// PrepareData fetches credit topup data in batches and converts it to CSV format
func (e *CreditTopupExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	const batchSize = 500

	e.logger.Infow("starting batched credit topup data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime,
		"batch_size", batchSize)

	// Collect all CSV records
	var csvRecords []*CreditTopupCSV
	totalRecords := 0
	offset := 0

	// Fetch and process data in batches
	for {
		e.logger.Debugw("fetching batch",
			"offset", offset,
			"batch_size", batchSize)

		topupData, err := e.walletRepo.GetCreditTopupsForExport(
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
				WithHint("Failed to fetch credit topup data batch").
				WithReportableDetails(map[string]interface{}{
					"offset":     offset,
					"batch_size": batchSize,
				}).
				Mark(ierr.ErrDatabase)
		}

		// If no data returned, we've reached the end
		if len(topupData) == 0 {
			break
		}

		e.logger.Debugw("fetched batch",
			"offset", offset,
			"records_in_batch", len(topupData),
			"total_so_far", totalRecords+len(topupData))

		// Convert batch to CSV records
		batchRecords := e.convertToCSVRecords(topupData)
		csvRecords = append(csvRecords, batchRecords...)

		totalRecords += len(topupData)
		offset += batchSize

		// If we got fewer records than batch size, we've reached the end
		if len(topupData) < batchSize {
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
		e.logger.Infow("no credit topup data found for export - will upload empty CSV with headers only",
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

// convertToCSVRecords converts CreditTopupExportData to CSV records
func (e *CreditTopupExporter) convertToCSVRecords(topupData []*wallet.CreditTopupsExportData) []*CreditTopupCSV {
	records := make([]*CreditTopupCSV, 0, len(topupData))

	for _, topup := range topupData {
		record := &CreditTopupCSV{
			TopupID:             topup.TopupID,
			ExternalID:          topup.ExternalID,
			CustomerName:        topup.CustomerName,
			WalletID:            topup.WalletID,
			Amount:              topup.Amount.String(),
			CreditBalanceBefore: topup.CreditBalanceBefore.String(),
			CreditBalanceAfter:  topup.CreditBalanceAfter.String(),
			ReferenceID:         topup.ReferenceID,
			TransactionReason:   string(topup.TransactionReason),
			CreatedAt:           topup.CreatedAt.Format(time.RFC3339),
		}

		records = append(records, record)
	}

	return records
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *CreditTopupExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeCreditTopups)
}
