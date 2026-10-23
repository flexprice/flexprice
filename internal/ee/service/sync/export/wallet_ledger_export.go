package export

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
	"github.com/samber/lo"
)

// WalletLedgerExporter exports every credit and debit of each wallet in the order they changed
// its balance, a debit split into the credit batches it drew from.
type WalletLedgerExporter struct {
	walletRepo wallet.Repository
	logger     *logger.Logger
}

// WalletLedgerCSV is one row of the wallet ledger export.
type WalletLedgerCSV struct {
	TransactionID              string `csv:"transaction_id"`
	Type                       string `csv:"type"`
	TransactionReason          string `csv:"transaction_reason"`
	ExternalID                 string `csv:"external_id"`
	CustomerName               string `csv:"name"`
	WalletID                   string `csv:"wallet_id"`
	Currency                   string `csv:"currency"`
	InvoiceID                  string `csv:"invoice_id"`
	InvoicePeriodStart         string `csv:"invoice_period_start"` // RFC3339; empty when the invoice has no period
	InvoicePeriodEnd           string `csv:"invoice_period_end"`
	CreditTransactionID        string `csv:"credit_transaction_id"`
	Credits                    string `csv:"credits"` // Negative for a debit
	CreditBalanceBefore        string `csv:"credit_balance_before"`
	CreditBalanceAfter         string `csv:"credit_balance_after"`
	WalletBalanceBefore        string `csv:"wallet_balance_before"`
	WalletBalanceAfter         string `csv:"wallet_balance_after"`
	TopUpAmountRemainingBefore string `csv:"topup_amount_remaining_before"`
	AmountPaid                 string `csv:"amount_paid"`       // Credit rows only
	TopUpAmountUsed            string `csv:"topup_amount_used"` // Debit rows only
	TopUpAmountRemainingAfter  string `csv:"topup_amount_remaining_after"`
	EffectiveAt                string `csv:"effective_at"` // RFC3339; when the row changed the balance
	CreatedAt                  string `csv:"created_at"`   // RFC3339
}

// NewWalletLedgerExporter creates a new wallet ledger exporter
func NewWalletLedgerExporter(walletRepo wallet.Repository, logger *logger.Logger) *WalletLedgerExporter {
	return &WalletLedgerExporter{
		walletRepo: walletRepo,
		logger:     logger,
	}
}

// PrepareData returns the whole ledger CSV in memory; the export service streams it via WriteData.
func (e *WalletLedgerExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	var buf bytes.Buffer
	count, err := e.WriteData(ctx, request, &buf)
	if err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), count, nil
}

// WriteData writes the ledger a batch of wallets at a time, reading each wallet's history once.
func (e *WalletLedgerExporter) WriteData(ctx context.Context, request *dto.ExportRequest, w io.Writer) (int, error) {
	const walletsPerBatch = 50

	walletIDs, err := e.walletRepo.ListWalletIDsForLedgerExport(ctx, request.TenantID, request.EnvID, request.StartTime, request.EndTime, request.CustomerExternalIDs)
	if err != nil {
		return 0, err
	}

	csvWriter := gocsv.DefaultCSVWriter(w)
	if err := gocsv.MarshalCSV([]*WalletLedgerCSV{}, csvWriter); err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to marshal data to CSV").
			Mark(ierr.ErrInternal)
	}

	totalRecords := 0
	for _, batch := range lo.Chunk(walletIDs, walletsPerBatch) {
		rows, err := e.walletRepo.GetWalletLedgerForExport(ctx, request.TenantID, request.EnvID, batch, request.StartTime, request.EndTime)
		if err != nil {
			return 0, ierr.WithError(err).
				WithHint("Failed to fetch wallet ledger data batch").
				WithReportableDetails(map[string]interface{}{
					"first_wallet_id": batch[0],
					"batch_size":      len(batch),
				}).
				Mark(ierr.ErrDatabase)
		}
		if err := gocsv.MarshalCSVWithoutHeaders(lo.Map(rows, func(r *wallet.WalletLedgerExportData, _ int) *WalletLedgerCSV {
			return toWalletLedgerCSV(r)
		}), csvWriter); err != nil {
			return 0, ierr.WithError(err).
				WithHint("Failed to marshal data to CSV").
				Mark(ierr.ErrInternal)
		}
		totalRecords += len(rows)
	}

	e.logger.Info(ctx, "completed wallet ledger export data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"customer_filter_count", len(request.CustomerExternalIDs),
		"wallet_count", len(walletIDs),
		"total_records", totalRecords)

	return totalRecords, nil
}

func toWalletLedgerCSV(r *wallet.WalletLedgerExportData) *WalletLedgerCSV {
	record := &WalletLedgerCSV{
		TransactionID:              r.TransactionID,
		Type:                       string(r.Type),
		TransactionReason:          string(r.TransactionReason),
		ExternalID:                 r.ExternalID,
		CustomerName:               r.CustomerName,
		WalletID:                   r.WalletID,
		Currency:                   r.Currency,
		InvoiceID:                  r.InvoiceID,
		InvoicePeriodStart:         timeOrEmpty(r.InvoicePeriodStart),
		InvoicePeriodEnd:           timeOrEmpty(r.InvoicePeriodEnd),
		CreditTransactionID:        r.CreditTransactionID,
		Credits:                    r.Credits.String(),
		CreditBalanceBefore:        r.CreditBalanceBefore.String(),
		CreditBalanceAfter:         r.CreditBalanceAfter.String(),
		WalletBalanceBefore:        types.RoundToCurrencyPrecision(r.WalletBalanceBefore, r.Currency).String(),
		WalletBalanceAfter:         types.RoundToCurrencyPrecision(r.WalletBalanceAfter, r.Currency).String(),
		TopUpAmountRemainingBefore: types.RoundToCurrencyPrecision(r.TopUpAmountRemainingBefore, r.Currency).String(),
		TopUpAmountRemainingAfter:  types.RoundToCurrencyPrecision(r.TopUpAmountRemainingAfter, r.Currency).String(),
		EffectiveAt:                r.EffectiveAt.Format(time.RFC3339),
		CreatedAt:                  r.CreatedAt.Format(time.RFC3339),
	}

	if r.Type == types.TransactionTypeDebit {
		record.Credits = r.Credits.Neg().String()
		record.TopUpAmountUsed = shareOfAmountPaid(r.Credits, r.WalletTxnCredits, r.WalletTxnAmountPaid, r.Currency)
		return record
	}
	if r.AmountPaid != nil {
		record.AmountPaid = types.RoundToCurrencyPrecision(*r.AmountPaid, r.Currency).String()
	}
	return record
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *WalletLedgerExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeWalletLedger)
}
