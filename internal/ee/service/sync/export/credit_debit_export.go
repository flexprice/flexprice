package export

import (
	"bytes"
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CreditDebitExporter exports wallet debits, one row per credit batch each debit drew from.
type CreditDebitExporter struct {
	walletRepo wallet.Repository
	logger     *logger.Logger
}

// CreditDebitCSV represents the CSV structure for credit debit export
type CreditDebitCSV struct {
	DebitID             string           `csv:"debit_id"`
	ExternalID          string           `csv:"external_id"`
	CustomerName        string           `csv:"name"`
	WalletID            string           `csv:"wallet_id"`
	Currency            string           `csv:"currency"`
	TransactionReason   string           `csv:"transaction_reason"`
	InvoiceID           string           `csv:"invoice_id"`
	CreditTransactionID string           `csv:"credit_transaction_id"`
	Credits             string           `csv:"credits"`
	RecognizedAmount    *decimal.Decimal `csv:"recognized_amount"` // Empty when the batch has no paid amount
	CreatedAt           string           `csv:"created_at"`        // RFC3339 format
}

// NewCreditDebitExporter creates a new credit debit exporter
func NewCreditDebitExporter(walletRepo wallet.Repository, logger *logger.Logger) *CreditDebitExporter {
	return &CreditDebitExporter{
		walletRepo: walletRepo,
		logger:     logger,
	}
}

// PrepareData fetches credit debit data in batches and converts it to CSV format
func (e *CreditDebitExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	const batchSize = 500

	var csvRecords []*CreditDebitCSV
	cursor := wallet.CreditDebitsExportCursor{CreatedAt: request.StartTime}
	for {
		rows, err := e.walletRepo.GetCreditDebitsForExport(
			ctx,
			request.TenantID,
			request.EnvID,
			cursor,
			request.EndTime,
			batchSize,
		)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to fetch credit debit data batch").
				WithReportableDetails(map[string]interface{}{
					"after_debit_id": cursor.DebitID,
					"batch_size":     batchSize,
				}).
				Mark(ierr.ErrDatabase)
		}

		debits := 0
		for i, d := range rows {
			if i == 0 || d.DebitID != rows[i-1].DebitID {
				debits++
			}
			csvRecords = append(csvRecords, toCreditDebitCSV(d))
		}

		// The limit counts debits, not rows: a full page means there may be more.
		if debits < batchSize {
			break
		}
		last := rows[len(rows)-1]
		cursor = wallet.CreditDebitsExportCursor{CreatedAt: last.CreatedAt, DebitID: last.DebitID}
	}

	var buf bytes.Buffer
	if err := gocsv.Marshal(csvRecords, &buf); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to marshal data to CSV").
			Mark(ierr.ErrInternal)
	}

	e.logger.Info(ctx, "completed credit debit export data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"total_records", len(csvRecords),
		"csv_size_bytes", buf.Len())

	return buf.Bytes(), len(csvRecords), nil
}

func toCreditDebitCSV(d *wallet.CreditDebitsExportData) *CreditDebitCSV {
	return &CreditDebitCSV{
		DebitID:             d.DebitID,
		ExternalID:          d.ExternalID,
		CustomerName:        d.CustomerName,
		WalletID:            d.WalletID,
		Currency:            d.Currency,
		TransactionReason:   string(d.TransactionReason),
		InvoiceID:           d.InvoiceID,
		CreditTransactionID: d.CreditTransactionID,
		Credits:             d.Credits.String(),
		RecognizedAmount:    recognizedAmount(d),
		CreatedAt:           d.CreatedAt.Format(time.RFC3339),
	}
}

// recognizedAmount spreads the batch's paid amount evenly over its credits and takes the share
// for the credits this debit drew.
func recognizedAmount(d *wallet.CreditDebitsExportData) *decimal.Decimal {
	if d.BatchPaidAmount == nil || d.BatchCredits == nil || d.BatchCredits.IsZero() {
		return nil
	}
	amount := d.Credits.Mul(*d.BatchPaidAmount).Div(*d.BatchCredits)
	return lo.ToPtr(types.RoundToCurrencyPrecision(amount, d.Currency))
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *CreditDebitExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeCreditDebits)
}
