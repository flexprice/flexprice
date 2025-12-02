package internal

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type creditUsageReportScript struct {
	log              *logger.Logger
	customerRepo     customer.Repository
	walletRepo       wallet.Repository
	walletService    service.WalletService
	subscriptionRepo subscription.Repository
	subscriptionSvc  service.SubscriptionService
}

// CreditUsageReportData represents the aggregated credit data for a customer
type CreditUsageReportData struct {
	CustomerName          string
	CustomerExternalID    string
	CustomerID            string
	CreditsAddedFree      decimal.Decimal
	CreditsAddedPurchased decimal.Decimal
	CreditsUsed           decimal.Decimal
}

// mockEventPublisher is a no-op event publisher for scripts
type mockEventPublisher struct{}

func (m *mockEventPublisher) Publish(ctx context.Context, event *events.Event) error {
	// No-op for scripts
	return nil
}

// GenerateCreditUsageReport generates a credit usage report for all customers in a tenant/environment
func GenerateCreditUsageReport() error {
	// Get environment variables for the script
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	startTimeStr := os.Getenv("START_TIME")
	endTimeStr := os.Getenv("END_TIME")

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	if startTimeStr == "" || endTimeStr == "" {
		return fmt.Errorf("START_TIME and END_TIME are required (ISO-8601 format: 2006-01-02T15:04:05Z)")
	}

	// Parse time parameters
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		return fmt.Errorf("invalid START_TIME format, use ISO-8601 (2006-01-02T15:04:05Z): %w", err)
	}

	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		return fmt.Errorf("invalid END_TIME format, use ISO-8601 (2006-01-02T15:04:05Z): %w", err)
	}

	if startTime.After(endTime) {
		return fmt.Errorf("START_TIME must be before END_TIME")
	}

	log.Printf("Starting credit usage report for tenant: %s, environment: %s\n", tenantID, environmentID)
	log.Printf("Time range: %s to %s\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Initialize script
	script, err := newCreditUsageReportScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// Get all customers for this tenant/environment
	customerFilter := &types.CustomerFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	}
	customers, err := script.customerRepo.ListAll(ctx, customerFilter)
	if err != nil {
		return fmt.Errorf("failed to list customers: %w", err)
	}

	log.Printf("Found %d customers to process\n", len(customers))

	// Process each customer and collect report data
	reportData := make([]CreditUsageReportData, 0)

	for i, cust := range customers {
		if i%10 == 0 {
			log.Printf("Processing customer %d/%d: %s\n", i+1, len(customers), cust.ID)
		}

		if cust.TenantID != tenantID || cust.EnvironmentID != environmentID {
			continue
		}

		if cust.Status != types.StatusPublished {
			continue
		}

		// Get customer data
		customerName := cust.Name
		if customerName == "" {
			customerName = cust.ExternalID
		}
		if customerName == "" {
			customerName = cust.ID
		}

		// Get all wallets for this customer
		wallets, err := script.walletRepo.GetWalletsByCustomerID(ctx, cust.ID)
		if err != nil {
			log.Printf("Warning: Failed to get wallets for customer %s: %v\n", cust.ID, err)
			// Add customer with zero values if no wallets
			reportData = append(reportData, CreditUsageReportData{
				CustomerName:          customerName,
				CustomerExternalID:    cust.ExternalID,
				CustomerID:            cust.ID,
				CreditsAddedFree:      decimal.Zero,
				CreditsAddedPurchased: decimal.Zero,
				CreditsUsed:           decimal.Zero,
			})
			continue
		}

		// Aggregate data across all wallets for this customer
		var creditsAddedFree, creditsAddedPurchased decimal.Decimal
		var staticCreditBalance, realTimeCreditBalance decimal.Decimal
		var invoicePaymentDebits decimal.Decimal

		for _, w := range wallets {
			// Query credit transactions (added) during the period
			creditFilter := &types.WalletTransactionFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				TimeRangeFilter: &types.TimeRangeFilter{
					StartTime: &startTime,
					EndTime:   &endTime,
				},
				WalletID:          &w.ID,
				Type:              lo.ToPtr(types.TransactionTypeCredit),
				TransactionStatus: lo.ToPtr(types.TransactionStatusCompleted),
			}

			creditTransactions, err := script.walletRepo.ListAllWalletTransactions(ctx, creditFilter)
			if err != nil {
				log.Printf("Warning: Failed to get credit transactions for wallet %s: %v\n", w.ID, err)
			} else {
				// Process credit transactions
				for _, tx := range creditTransactions {
					amount := tx.CreditAmount

					// Categorize by transaction reason
					switch tx.TransactionReason {
					case types.TransactionReasonFreeCredit, types.TransactionReasonSubscriptionCredit, types.TransactionReasonCreditNote:
						creditsAddedFree = creditsAddedFree.Add(amount)
					case types.TransactionReasonPurchasedCreditInvoiced, types.TransactionReasonPurchasedCreditDirect:
						creditsAddedPurchased = creditsAddedPurchased.Add(amount)
					}
				}
			}

			// Query debit transactions with invoice payment reason during the period
			debitFilter := &types.WalletTransactionFilter{
				QueryFilter: types.NewNoLimitQueryFilter(),
				TimeRangeFilter: &types.TimeRangeFilter{
					StartTime: &startTime,
					EndTime:   &endTime,
				},
				WalletID:          &w.ID,
				Type:              lo.ToPtr(types.TransactionTypeDebit),
				TransactionStatus: lo.ToPtr(types.TransactionStatusCompleted),
			}

			debitTransactions, err := script.walletRepo.ListAllWalletTransactions(ctx, debitFilter)
			if err != nil {
				log.Printf("Warning: Failed to get debit transactions for wallet %s: %v\n", w.ID, err)
			} else {
				// Process debit transactions - only count invoice payment debits
				for _, tx := range debitTransactions {
					if tx.TransactionReason == types.TransactionReasonInvoicePayment {
						invoicePaymentDebits = invoicePaymentDebits.Add(tx.CreditAmount)
					}
				}
			}

			// Get wallet balance to calculate credits used
			balanceResp, err := script.walletService.GetWalletBalance(ctx, w.ID)
			if err != nil {
				log.Printf("Warning: Failed to get wallet balance for wallet %s: %v\n", w.ID, err)
				continue
			}

			// Accumulate static and real-time credit balances
			if balanceResp.Wallet != nil {
				staticCreditBalance = staticCreditBalance.Add(balanceResp.Wallet.CreditBalance)
			}
			if balanceResp.RealTimeCreditBalance != nil {
				realTimeCreditBalance = realTimeCreditBalance.Add(*balanceResp.RealTimeCreditBalance)
			}
		}

		// Calculate credits used: static balance - real-time balance + invoice payment debits
		creditsUsed := staticCreditBalance.Sub(realTimeCreditBalance).Add(invoicePaymentDebits)

		// Add customer data to report
		reportData = append(reportData, CreditUsageReportData{
			CustomerName:          customerName,
			CustomerExternalID:    cust.ExternalID,
			CustomerID:            cust.ID,
			CreditsAddedFree:      creditsAddedFree,
			CreditsAddedPurchased: creditsAddedPurchased,
			CreditsUsed:           creditsUsed,
		})
	}

	// Generate CSV output
	outputFile := fmt.Sprintf("credit_usage_report_%s_%s.csv", tenantID, time.Now().Format("20060102_150405"))
	if err := generateCSVReport(reportData, outputFile); err != nil {
		return fmt.Errorf("failed to generate CSV report: %w", err)
	}

	log.Printf("Credit usage report generated successfully: %s\n", outputFile)
	log.Printf("Total customers processed: %d\n", len(reportData))

	return nil
}

// generateCSVReport generates a CSV file from the report data
func generateCSVReport(data []CreditUsageReportData, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Customer Name",
		"Customer External ID",
		"Customer ID",
		"Credits Added (Free)",
		"Credits Added (Purchased)",
		"Credits Used",
	}

	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Write data rows
	for _, row := range data {
		record := []string{
			row.CustomerName,
			row.CustomerExternalID,
			row.CustomerID,
			row.CreditsAddedFree.String(),
			row.CreditsAddedPurchased.String(),
			row.CreditsUsed.String(),
		}

		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
		}
	}

	return nil
}

func newCreditUsageReportScript() (*creditUsageReportScript, error) {
	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	log, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize ClickHouse client for event repositories
	sentryService := sentry.NewSentryService(cfg, log)
	chStore, err := clickhouse.NewClickHouseStore(cfg, sentryService)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to clickhouse: %w", err)
	}

	// Initialize postgres client
	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
	}
	client := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Create repositories
	customerRepo := entRepo.NewCustomerRepository(client, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(client, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, log, cacheClient)
	subscriptionLineItemRepo := entRepo.NewSubscriptionLineItemRepository(client, log, cacheClient)
	subscriptionPhaseRepo := entRepo.NewSubscriptionPhaseRepository(client, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(client, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(client, log, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(client, log, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(client, log, cacheClient)
	entitlementRepo := entRepo.NewEntitlementRepository(client, log, cacheClient)
	walletRepo := entRepo.NewWalletRepository(client, log, cacheClient)
	addonRepo := entRepo.NewAddonRepository(client, log, cacheClient)
	addonAssociationRepo := entRepo.NewAddonAssociationRepository(client, log, cacheClient)
	eventRepo := chRepo.NewEventRepository(chStore, log)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, log)

	// Create service params
	serviceParams := service.ServiceParams{
		Logger:                   log,
		Config:                   cfg,
		DB:                       client,
		CustomerRepo:             customerRepo,
		PlanRepo:                 planRepo,
		SubRepo:                  subscriptionRepo,
		SubscriptionLineItemRepo: subscriptionLineItemRepo,
		SubscriptionPhaseRepo:    subscriptionPhaseRepo,
		PriceRepo:                priceRepo,
		MeterRepo:                meterRepo,
		EntitlementRepo:          entitlementRepo,
		InvoiceRepo:              invoiceRepo,
		FeatureRepo:              featureRepo,
		WalletRepo:               walletRepo,
		AddonRepo:                addonRepo,
		AddonAssociationRepo:     addonAssociationRepo,
		EventRepo:                eventRepo,
		ProcessedEventRepo:       processedEventRepo,
	}

	// Create services
	walletService := service.NewWalletService(serviceParams)
	subscriptionSvc := service.NewSubscriptionService(serviceParams)

	return &creditUsageReportScript{
		log:              log,
		customerRepo:     customerRepo,
		walletRepo:       walletRepo,
		subscriptionRepo: subscriptionRepo,
		walletService:    walletService,
		subscriptionSvc:  subscriptionSvc,
	}, nil
}
