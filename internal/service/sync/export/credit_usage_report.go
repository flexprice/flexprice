package export

import (
	"bytes"
	"context"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gocarina/gocsv"
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

// CreditUsageCSV represents the CSV structure for credit usage export
type CreditUsageCSV struct {
	CustomerName       string `csv:"name"`
	CustomerExternalID string `csv:"customer_external_id"`
	CustomerID         string `csv:"customer_id"`
	CurrentBalance     string `csv:"current_balance"`
	RealtimeBalance    string `csv:"real_time_balance"`
	NumberOfWallets    int    `csv:"number_of_wallets"`
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

// PrepareData fetches credit usage data and converts it to CSV format
func (e *CreditUsageExporter) PrepareData(ctx context.Context, request *dto.ExportRequest) ([]byte, int, error) {
	e.logger.Infow("starting credit usage data fetch",
		"tenant_id", request.TenantID,
		"env_id", request.EnvID,
		"start_time", request.StartTime,
		"end_time", request.EndTime)

	// Ensure tenant and environment are set in context for proper filtering
	ctx = types.SetTenantID(ctx, request.TenantID)
	ctx = types.SetEnvironmentID(ctx, request.EnvID)

	// Get all customers for this tenant/environment (no limit, filtered by context)
	customerFilter := &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}
	customers, err := e.customerRepo.ListAll(ctx, customerFilter)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to list customers").
			Mark(ierr.ErrDatabase)
	}

	e.logger.Infow("found customers to process",
		"customer_count", len(customers),
		"tenant_id", request.TenantID,
		"env_id", request.EnvID)

	// Process all customers
	var usageData []*wallet.CreditUsageExportData

	for _, customer := range customers {
		// Get all wallets for this customer
		wallets, err := e.walletRepo.GetWalletsByCustomerID(ctx, customer.ID)
		if err != nil {
			e.logger.Debugw("Failed to get wallets for customer", "customer_id", customer.ID, "error", err)
			// Add customer with zero values if no wallets
			usageData = append(usageData, &wallet.CreditUsageExportData{
				CustomerID:         customer.ID,
				CustomerName:       customer.Name,
				CustomerExternalID: customer.ExternalID,
				CurrentBalance:     decimal.Zero,
				RealtimeBalance:    decimal.Zero,
				NumberOfWallets:    0,
			})
			continue
		}

		// Aggregate balances across all wallets for this customer
		var currentBalance, realtimeBalance decimal.Decimal

		for _, wallet := range wallets {
			// Get wallet balance
			balanceResp, err := e.walletBalanceGetter.GetWalletBalanceV2(ctx, wallet.ID)
			if err != nil {
				e.logger.Debugw("Failed to get wallet balance for wallet", "wallet_id", wallet.ID, "error", err)
				continue
			}

			// Accumulate static and real-time credit balances
			if balanceResp.Wallet != nil {
				currentBalance = currentBalance.Add(balanceResp.Wallet.CreditBalance)
			}
			if balanceResp.RealTimeCreditBalance != nil {
				realtimeBalance = realtimeBalance.Add(*balanceResp.RealTimeCreditBalance)
			}
		}

		usageData = append(usageData, &wallet.CreditUsageExportData{
			CustomerName:       customer.Name,
			CustomerExternalID: customer.ExternalID,
			CustomerID:         customer.ID,
			CurrentBalance:     currentBalance,
			RealtimeBalance:    realtimeBalance,
			NumberOfWallets:    len(wallets),
		})
	}

	totalRecords := len(usageData)

	// Convert to CSV records
	csvRecords := e.convertToCSVRecords(usageData)

	// Marshal to CSV using gocsv
	var buf bytes.Buffer
	if err := gocsv.Marshal(csvRecords, &buf); err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to marshal data to CSV").
			Mark(ierr.ErrInternal)
	}

	csvBytes := buf.Bytes()

	if totalRecords == 0 {
		e.logger.Infow("no credit usage data found for export - will upload empty CSV with headers only",
			"tenant_id", request.TenantID,
			"env_id", request.EnvID,
			"csv_size_bytes", len(csvBytes))
	} else {
		e.logger.Infow("completed data fetch and CSV conversion",
			"total_records", totalRecords,
			"csv_size_bytes", len(csvBytes))
	}

	return csvBytes, totalRecords, nil
}

// convertToCSVRecords converts CreditUsageExportData to CSV records
func (e *CreditUsageExporter) convertToCSVRecords(usageData []*wallet.CreditUsageExportData) []*CreditUsageCSV {
	records := make([]*CreditUsageCSV, 0, len(usageData))

	for _, usage := range usageData {
		record := &CreditUsageCSV{
			CustomerName:       usage.CustomerName,
			CustomerExternalID: usage.CustomerExternalID,
			CustomerID:         usage.CustomerID,
			CurrentBalance:     usage.CurrentBalance.String(),
			RealtimeBalance:    usage.RealtimeBalance.String(),
			NumberOfWallets:    usage.NumberOfWallets,
		}

		records = append(records, record)
	}

	return records
}

// GetFilenamePrefix returns the prefix for the exported file
func (e *CreditUsageExporter) GetFilenamePrefix() string {
	return string(types.ScheduledTaskEntityTypeCreditUsage)
}
