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
	"github.com/flexprice/flexprice/internal/domain/wallet"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type creditUsageReportScript struct {
	log           *logger.Logger
	customerRepo  customer.Repository
	walletRepo    wallet.Repository
	walletService service.WalletService
}

// CreditUsageReportData represents the credit balance data for a customer
type CreditUsageReportData struct {
	CustomerName       string
	CustomerExternalID string
	CustomerID         string
	CurrentBalance     decimal.Decimal
	RealtimeBalance    decimal.Decimal
}

// GenerateCreditUsageReport generates a credit balance report for all customers in a tenant/environment
func GenerateCreditUsageReport() error {
	// Get environment variables for the script
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	log.Printf("Starting credit balance report for tenant: %s, environment: %s\n", tenantID, environmentID)

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
				CustomerName:       customerName,
				CustomerExternalID: cust.ExternalID,
				CustomerID:         cust.ID,
				CurrentBalance:     decimal.Zero,
				RealtimeBalance:    decimal.Zero,
			})
			continue
		}

		// Aggregate balances across all wallets for this customer
		var currentBalance, realtimeBalance decimal.Decimal

		for _, w := range wallets {
			// Get wallet balance
			balanceResp, err := script.walletService.GetWalletBalance(ctx, w.ID)
			if err != nil {
				log.Printf("Warning: Failed to get wallet balance for wallet %s: %v\n", w.ID, err)
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

		// Add customer data to report
		reportData = append(reportData, CreditUsageReportData{
			CustomerName:       customerName,
			CustomerExternalID: cust.ExternalID,
			CustomerID:         cust.ID,
			CurrentBalance:     currentBalance,
			RealtimeBalance:    realtimeBalance,
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
		"Current Balance",
		"Realtime Balance",
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
			row.CurrentBalance.String(),
			row.RealtimeBalance.String(),
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
	client := postgres.NewClient(entClient, log, sentryService)
	cacheClient := cache.NewInMemoryCache()

	// Create repositories
	customerRepo := entRepo.NewCustomerRepository(client, log, cacheClient)
	walletRepo := entRepo.NewWalletRepository(client, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(client, log, cacheClient)
	subscriptionLineItemRepo := entRepo.NewSubscriptionLineItemRepository(client, log, cacheClient)
	subscriptionPhaseRepo := entRepo.NewSubscriptionPhaseRepository(client, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(client, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(client, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(client, log, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(client, log, cacheClient)
	entitlementRepo := entRepo.NewEntitlementRepository(client, log, cacheClient)
	addonRepo := entRepo.NewAddonRepository(client, log, cacheClient)
	addonAssociationRepo := entRepo.NewAddonAssociationRepository(client, log, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(client, log, cacheClient)
	eventRepo := chRepo.NewEventRepository(chStore, log)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, log)
	featureUsageRepo := chRepo.NewFeatureUsageRepository(chStore, log)

	// Create service params (required for wallet service which needs subscription and billing services)
	serviceParams := service.ServiceParams{
		Logger:                   log,
		Config:                   cfg,
		DB:                       client,
		CustomerRepo:             customerRepo,
		WalletRepo:               walletRepo,
		SubRepo:                  subscriptionRepo,
		SubscriptionLineItemRepo: subscriptionLineItemRepo,
		SubscriptionPhaseRepo:    subscriptionPhaseRepo,
		PlanRepo:                 planRepo,
		PriceRepo:                priceRepo,
		MeterRepo:                meterRepo,
		FeatureRepo:              featureRepo,
		EntitlementRepo:          entitlementRepo,
		AddonRepo:                addonRepo,
		AddonAssociationRepo:     addonAssociationRepo,
		InvoiceRepo:              invoiceRepo,
		EventRepo:                eventRepo,
		ProcessedEventRepo:       processedEventRepo,
		FeatureUsageRepo:         featureUsageRepo,
	}

	// Create services
	walletService := service.NewWalletService(serviceParams)

	return &creditUsageReportScript{
		log:           log,
		customerRepo:  customerRepo,
		walletRepo:    walletRepo,
		walletService: walletService,
	}, nil
}
