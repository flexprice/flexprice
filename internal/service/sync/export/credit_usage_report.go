package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"strconv"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// WalletBalanceGetter is an interface for getting wallet balance
// This avoids import cycle with service package
type WalletBalanceGetter interface {
	GetWalletBalanceV2(ctx context.Context, walletID string) (*dto.WalletBalanceResponse, error)
}

// CreditUsageExporter handles credit usage export operations
type CreditUsageExporter struct {
	walletRepo          wallet.Repository
	customerRepo        customer.Repository
	walletBalanceGetter WalletBalanceGetter
	integrationFactory  *integration.Factory
	logger              *logger.Logger
}

// CreditUsageExportData represents the joined data for one customer row in the credit usage export.
// Metadata holds merged customer and wallet metadata keyed as "<entity_type>-<field_key>".
type CreditUsageExportData struct {
	CustomerID         string
	CustomerName       string
	CustomerExternalID string
	CurrentBalance     decimal.Decimal
	RealtimeBalance    decimal.Decimal
	NumberOfWallets    int
	Metadata           types.Metadata
}

// metadataKeyDelimiter separates entity type from field key in the merged metadata map.
// e.g. "customer-account_number__c", "wallet-balance_limit"
const metadataKeyDelimiter = "-"

// staticHeaders is the fixed set of base CSV columns.
var staticHeaders = []string{
	string(wallet.CreditUsageCSVHeadersCustomerName),
	string(wallet.CreditUsageCSVHeadersCustomerExternalID),
	string(wallet.CreditUsageCSVHeadersCustomerID),
	string(wallet.CreditUsageCSVHeadersCurrentBalance),
	string(wallet.CreditUsageCSVHeadersRealtimeBalance),
	string(wallet.CreditUsageCSVHeadersNumberOfWallets),
}

// NewCreditUsageExporter creates a new credit usage exporter
func NewCreditUsageExporter(
	walletRepo wallet.Repository,
	customerRepo customer.Repository,
	walletBalanceGetter WalletBalanceGetter,
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *CreditUsageExporter {
	return &CreditUsageExporter{
		walletRepo:          walletRepo,
		customerRepo:        customerRepo,
		walletBalanceGetter: walletBalanceGetter,
		integrationFactory:  integrationFactory,
		logger:              logger,
	}
}

// PrepareData fetches credit usage data and streams each row directly into the CSV buffer.
func (e *CreditUsageExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	e.logger.Infow("starting credit usage data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime)

	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	customers, err := e.customerRepo.ListAll(ctx, &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to list customers").
			Mark(ierr.ErrDatabase)
	}

	e.logger.Infow("found customers to process",
		"customer_count", len(customers),
		"tenant_id", request.TenantID,
		"env_id", request.EnvID)

	metadataFields := request.JobConfig.GetExportMetadataFields()

	var buf bytes.Buffer
	csvWriter := csv.NewWriter(&buf)

	if err := csvWriter.Write(e.resolveHeaders(metadataFields)); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to write CSV headers").
			Mark(ierr.ErrInternal)
	}

	recordCount := 0
	for _, c := range customers {
		record := e.buildRecord(ctx, c)

		if err := csvWriter.Write(e.buildRow(record, metadataFields)); err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to write CSV row").
				Mark(ierr.ErrInternal)
		}
		recordCount++
	}

	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to flush CSV writer").
			Mark(ierr.ErrInternal)
	}

	csvBytes := buf.Bytes()

	if recordCount == 0 {
		e.logger.Infow("no credit usage data found for export - will upload empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID,
			"csv_size_bytes", len(csvBytes))
	} else {
		e.logger.Infow("completed data fetch and CSV conversion",
			"total_records", recordCount,
			"csv_size_bytes", len(csvBytes))
	}

	return csvBytes, recordCount, nil
}

// buildRecord fetches wallets and balances for one customer and returns a CreditUsageExportData.
// Metadata keys are stored as "<entity_type>-<field_key>" so keys from different entity types
// never collide even when they share the same underlying field name.
func (e *CreditUsageExporter) buildRecord(ctx context.Context, c *customer.Customer) *CreditUsageExportData {
	merged := make(types.Metadata)

	wallets, err := e.walletRepo.GetWalletsByCustomerID(ctx, c.ID)
	if err != nil {
		e.logger.Debugw("failed to get wallets for customer", "customer_id", c.ID, "error", err)
	}

	var currentBalance, realtimeBalance decimal.Decimal

	for _, w := range wallets {
		balanceResp, err := e.walletBalanceGetter.GetWalletBalanceV2(ctx, w.ID)
		if err != nil {
			e.logger.Debugw("failed to get wallet balance", "wallet_id", w.ID, "error", err)
			continue
		}
		if balanceResp.Wallet != nil {
			currentBalance = currentBalance.Add(balanceResp.Wallet.CreditBalance)
		}
		if balanceResp.RealTimeCreditBalance != nil {
			realtimeBalance = realtimeBalance.Add(*balanceResp.RealTimeCreditBalance)
		}
		for k, v := range w.Metadata {
			merged[string(types.ExportMetadataEntityTypeWallet)+metadataKeyDelimiter+k] = v
		}
	}

	for k, v := range c.Metadata {
		merged[string(types.ExportMetadataEntityTypeCustomer)+metadataKeyDelimiter+k] = v
	}

	return &CreditUsageExportData{
		CustomerID:         c.ID,
		CustomerName:       c.Name,
		CustomerExternalID: c.ExternalID,
		CurrentBalance:     currentBalance,
		RealtimeBalance:    realtimeBalance,
		NumberOfWallets:    len(wallets),
		Metadata:           merged,
	}
}

// resolveHeaders returns the full CSV header row: static columns followed by one column per
// requested metadata field. f.ColumnName is the user-defined display label and is completely
// independent of the lookup key used in buildRow.
func (e *CreditUsageExporter) resolveHeaders(metadataFields types.ExportMetadataFields) []string {
	headers := make([]string, len(staticHeaders), len(staticHeaders)+len(metadataFields))
	copy(headers, staticHeaders)
	for _, f := range metadataFields {
		colName := f.ColumnName
		if colName == "" {
			colName = f.FieldKey
		}
		headers = append(headers, colName)
	}
	return headers
}

// buildRow converts one CreditUsageExportData into a CSV row aligned with resolveHeaders.
// Static columns come first; dynamic metadata columns follow in the exact same iteration
// order as metadataFields — so every record places its value under the correct header.
// Lookup key = "<entity_type>-<field_key>"; absent keys produce an empty string cell.
func (e *CreditUsageExporter) buildRow(record *CreditUsageExportData, metadataFields types.ExportMetadataFields) []string {
	row := make([]string, 0, len(staticHeaders)+len(metadataFields))
	row = append(row,
		record.CustomerName,
		record.CustomerExternalID,
		record.CustomerID,
		record.CurrentBalance.String(),
		record.RealtimeBalance.String(),
		strconv.Itoa(record.NumberOfWallets),
	)
	for _, f := range metadataFields {
		row = append(row, record.Metadata[string(f.EntityType)+metadataKeyDelimiter+f.FieldKey])
	}
	return row
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *CreditUsageExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeCreditUsage)
}
