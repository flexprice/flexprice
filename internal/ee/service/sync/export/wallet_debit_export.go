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
)

// WalletDebitExporter exports wallet debits, one row per credit batch each debit drew from.
type WalletDebitExporter struct {
	walletRepo wallet.Repository
	logger     *logger.Logger
}

// WalletDebitCSV represents the CSV structure for wallet debit export
type WalletDebitCSV struct {
	DebitID                    string `csv:"debit_id"`
	ExternalID                 string `csv:"external_id"`
	CustomerName               string `csv:"name"`
	WalletID                   string `csv:"wallet_id"`
	Currency                   string `csv:"currency"`
	TransactionReason          string `csv:"transaction_reason"`
	InvoiceID                  string `csv:"invoice_id"`
	CreditTransactionID        string `csv:"credit_transaction_id"`
	Credits                    string `csv:"credits"`
	CreditBalanceBefore        string `csv:"credit_balance_before"`
	CreditBalanceAfter         string `csv:"credit_balance_after"`
	WalletBalanceBefore        string `csv:"wallet_balance_before"`
	WalletBalanceAfter         string `csv:"wallet_balance_after"`
	TopUpAmountRemainingBefore string `csv:"topup_amount_remaining_before"`
	TopUpAmountUsed            string `csv:"topup_amount_used"` // Empty when the credit drawn from has no paid amount
	TopUpAmountRemainingAfter  string `csv:"topup_amount_remaining_after"`
	CreatedAt                  string `csv:"created_at"` // RFC3339 format
}

// NewWalletDebitExporter creates a new wallet debit exporter
func NewWalletDebitExporter(walletRepo wallet.Repository, logger *logger.Logger) *WalletDebitExporter {
	return &WalletDebitExporter{
		walletRepo: walletRepo,
		logger:     logger,
	}
}

// PrepareData fetches wallet debit data in batches and converts it to CSV format
func (e *WalletDebitExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	const batchSize = 500

	var csvRecords []*WalletDebitCSV
	cursor := wallet.WalletDebitsExportCursor{CreatedAt: request.StartTime}
	for {
		rows, err := e.walletRepo.GetWalletDebitsForExport(
			ctx,
			request.TenantID,
			request.EnvID,
			cursor,
			request.EndTime,
			batchSize,
		)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to fetch wallet debit data batch").
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
			csvRecords = append(csvRecords, toWalletDebitCSV(d))
		}

		// The limit counts debits, not rows: a full page means there may be more.
		if debits < batchSize {
			break
		}
		last := rows[len(rows)-1]
		cursor = wallet.WalletDebitsExportCursor{CreatedAt: last.CreatedAt, DebitID: last.DebitID}
	}

	var buf bytes.Buffer
	if err := gocsv.Marshal(csvRecords, &buf); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to marshal data to CSV").
			Mark(ierr.ErrInternal)
	}

	e.logger.Info(ctx, "completed wallet debit export data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"total_records", len(csvRecords),
		"csv_size_bytes", buf.Len())

	return buf.Bytes(), len(csvRecords), nil
}

func toWalletDebitCSV(d *wallet.WalletDebitsExportData) *WalletDebitCSV {
	return &WalletDebitCSV{
		DebitID:                    d.DebitID,
		ExternalID:                 d.ExternalID,
		CustomerName:               d.CustomerName,
		WalletID:                   d.WalletID,
		Currency:                   d.Currency,
		TransactionReason:          string(d.TransactionReason),
		InvoiceID:                  d.InvoiceID,
		CreditTransactionID:        d.CreditTransactionID,
		Credits:                    d.Credits.String(),
		CreditBalanceBefore:        d.CreditBalanceBefore.String(),
		CreditBalanceAfter:         d.CreditBalanceAfter.String(),
		WalletBalanceBefore:        types.RoundToCurrencyPrecision(d.WalletBalanceBefore, d.Currency).String(),
		WalletBalanceAfter:         types.RoundToCurrencyPrecision(d.WalletBalanceAfter, d.Currency).String(),
		TopUpAmountRemainingBefore: types.RoundToCurrencyPrecision(d.TopUpAmountRemainingBefore, d.Currency).String(),
		TopUpAmountRemainingAfter:  types.RoundToCurrencyPrecision(d.TopUpAmountRemainingAfter, d.Currency).String(),
		TopUpAmountUsed:            topUpAmountUsed(d),
		CreatedAt:                  d.CreatedAt.Format(time.RFC3339),
	}
}

// topUpAmountUsed spreads the wallet transaction's paid amount evenly over its credits and takes
// the share for the credits this debit drew.
func topUpAmountUsed(d *wallet.WalletDebitsExportData) string {
	if d.WalletTxnAmountPaid == nil || d.WalletTxnCredits == nil || d.WalletTxnCredits.IsZero() {
		return ""
	}
	amount := d.Credits.Mul(*d.WalletTxnAmountPaid).Div(*d.WalletTxnCredits)
	return types.RoundToCurrencyPrecision(amount, d.Currency).String()
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *WalletDebitExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeWalletDebits)
}
