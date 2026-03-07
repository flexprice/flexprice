package internal

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

const dateLayout = "2006-01-02"

// AnalyticsInvoiceDiffRow represents one row in the reconciliation diff CSV
type AnalyticsInvoiceDiffRow struct {
	CustomerID         string
	CustomerName        string
	ExternalCustomerID  string
	AnalyticsTotalCost  string
	InvoiceSubtotalSum  string
	Diff                string
	InvoiceIDs          string
	InvoiceCount        int
	Currency            string
}

type analyticsInvoiceReconciliationScript struct {
	log                           *logger.Logger
	customerRepo                  customer.Repository
	invoiceRepo                   invoice.Repository
	featureUsageTrackingService   service.FeatureUsageTrackingService
}

// RunAnalyticsInvoiceReconciliation compares analytics total cost to invoice subtotals for a period and writes diffs to CSV.
// Period is hardcoded to 1 Feb – 1 Mar. Requires TENANT_ID and ENVIRONMENT_ID.
// Infrastructure (Postgres, ClickHouse, Kafka) must be running.
func RunAnalyticsInvoiceReconciliation() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	startTime := time.Date(2025, time.February, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC)

	// End of period for invoice filter: period_end in [start, end]
	periodEndGTE := startTime
	periodEndLTE := endTime

	log.Printf("Reconciling analytics vs invoices for period %s to %s (tenant=%s, env=%s)",
		startTime.Format(dateLayout), endTime.Format(dateLayout), tenantID, environmentID)

	script, err := newAnalyticsInvoiceReconciliationScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// 1) List all customers (published)
	customerFilter := types.NewNoLimitCustomerFilter()
	customerFilter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	customers, err := script.customerRepo.ListAll(ctx, customerFilter)
	if err != nil {
		return fmt.Errorf("list customers: %w", err)
	}

	log.Printf("Found %d customers", len(customers))

	// 2) For each customer: get analytics total cost via feature usage tracking (same period)
	analyticsByCustomer := make(map[string]decimal.Decimal)
	for i, c := range customers {
		if c.TenantID != tenantID || c.EnvironmentID != environmentID {
			continue
		}
		if c.ExternalID == "" {
			log.Printf("Skip customer %s: no external_customer_id", c.ID)
			continue
		}
		if i%50 == 0 && i > 0 {
			log.Printf("Analytics progress: %d/%d customers", i, len(customers))
		}
		req := &dto.GetUsageAnalyticsRequest{
			ExternalCustomerID: c.ExternalID,
			StartTime:          startTime,
			EndTime:            endTime,
		}
		resp, err := script.featureUsageTrackingService.GetDetailedUsageAnalyticsV2(ctx, req)
		if err != nil {
			log.Printf("Warning: analytics for customer %s (%s): %v", c.ExternalID, c.ID, err)
			continue
		}
		analyticsByCustomer[c.ID] = resp.TotalCost
	}

	// 3) List invoices in period (period_end in range), sum subtotal by customer
	invFilter := types.NewNoLimitInvoiceFilter()
	invFilter.PeriodEndGTE = &periodEndGTE
	invFilter.PeriodEndLTE = &periodEndLTE
	invFilter.InvoiceStatus = []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized}
	invFilter.SkipLineItems = true

	invoices, err := script.invoiceRepo.List(ctx, invFilter)
	if err != nil { 
		return fmt.Errorf("list invoices: %w", err)
	}

	invoiceSubtotalByCustomer := make(map[string]decimal.Decimal)
	invoiceIDsByCustomer := make(map[string][]string)
	for _, inv := range invoices {
		if inv.TenantID != tenantID || inv.EnvironmentID != environmentID {
			continue
		}
		invoiceSubtotalByCustomer[inv.CustomerID] = invoiceSubtotalByCustomer[inv.CustomerID].Add(inv.Subtotal)
		invoiceIDsByCustomer[inv.CustomerID] = append(invoiceIDsByCustomer[inv.CustomerID], inv.ID)
	}

	// 4) Build diff rows (only where there is a meaningful difference)
	var rows []AnalyticsInvoiceDiffRow
	tolerance := decimal.NewFromFloat(0.0001)

	for _, c := range customers {
		if c.TenantID != tenantID || c.EnvironmentID != environmentID {
			continue
		}
		analyticsCost := analyticsByCustomer[c.ID]
		invoiceSubtotal := invoiceSubtotalByCustomer[c.ID]
		diff := analyticsCost.Sub(invoiceSubtotal)
		if diff.Abs().LessThanOrEqual(tolerance) {
			continue
		}
		customerName := c.Name
		if customerName == "" {
			customerName = c.ExternalID
		}
		if customerName == "" {
			customerName = c.ID
		}
		ids := invoiceIDsByCustomer[c.ID]
		idList := ""
		for _, id := range ids {
			if idList != "" {
				idList += ";"
			}
			idList += id
		}
		rows = append(rows, AnalyticsInvoiceDiffRow{
			CustomerID:        c.ID,
			CustomerName:      customerName,
			ExternalCustomerID: c.ExternalID,
			AnalyticsTotalCost: analyticsCost.StringFixed(4),
			InvoiceSubtotalSum: invoiceSubtotal.StringFixed(4),
			Diff:              diff.StringFixed(4),
			InvoiceIDs:        idList,
			InvoiceCount:      len(ids),
			Currency:          "USD",
		})
	}

	if len(rows) == 0 {
		log.Printf("No differences found; no CSV written.")
		return nil
	}

	outputFile := fmt.Sprintf("analytics_invoice_reconciliation_%s_%s.csv", tenantID, time.Now().Format("20060102_150405"))
	if err := writeReconciliationCSV(rows, outputFile); err != nil {
		return fmt.Errorf("write CSV: %w", err)
	}

	log.Printf("Wrote %d diff rows to %s", len(rows), outputFile)
	return nil
}

func writeReconciliationCSV(rows []AnalyticsInvoiceDiffRow, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"customer_id", "customer_name", "external_customer_id",
		"analytics_total_cost", "invoice_subtotal_sum", "diff",
		"invoice_ids", "invoice_count", "currency",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		record := []string{
			r.CustomerID, r.CustomerName, r.ExternalCustomerID,
			r.AnalyticsTotalCost, r.InvoiceSubtotalSum, r.Diff,
			r.InvoiceIDs, fmt.Sprintf("%d", r.InvoiceCount), r.Currency,
		}
		if err := w.Write(record); err != nil {
			return err
		}
	}
	return nil
}

func newAnalyticsInvoiceReconciliationScript() (*analyticsInvoiceReconciliationScript, error) {
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("logger: %w", err)
	}
	sentrySvc := sentry.NewSentryService(cfg, logger)
	chStore, err := clickhouse.NewClickHouseStore(cfg, sentrySvc)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: %w", err)
	}
	entClient, err := postgres.NewEntClients(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	pgClient := postgres.NewClient(entClient, logger, sentrySvc)
	cacheClient := cache.NewInMemoryCache()

	customerRepo := entRepo.NewCustomerRepository(pgClient, logger, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(pgClient, logger, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(pgClient, logger, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, logger, cacheClient)
	priceRepo := entRepo.NewPriceRepository(pgClient, logger, cacheClient)
	planRepo := entRepo.NewPlanRepository(pgClient, logger, cacheClient)
	subRepo := entRepo.NewSubscriptionRepository(pgClient, logger, cacheClient)
	subLineItemRepo := entRepo.NewSubscriptionLineItemRepository(pgClient, logger, cacheClient)
	addonRepo := entRepo.NewAddonRepository(pgClient, logger, cacheClient)
	eventRepo := chRepo.NewEventRepository(chStore, logger)
	featureUsageRepo := chRepo.NewFeatureUsageRepository(chStore, logger)

	serviceParams := service.ServiceParams{
		Logger:                   logger,
		Config:                   cfg,
		DB:                       pgClient,
		CustomerRepo:             customerRepo,
		InvoiceRepo:              invoiceRepo,
		FeatureRepo:              featureRepo,
		MeterRepo:                meterRepo,
		PriceRepo:                priceRepo,
		PlanRepo:                 planRepo,
		SubRepo:                  subRepo,
		SubscriptionLineItemRepo: subLineItemRepo,
		AddonRepo:                addonRepo,
		EventRepo:                eventRepo,
		FeatureUsageRepo:         featureUsageRepo,
	}
	featureUsageTrackingService := service.NewFeatureUsageTrackingService(serviceParams, eventRepo, featureUsageRepo)

	return &analyticsInvoiceReconciliationScript{
		log:                         logger,
		customerRepo:                customerRepo,
		invoiceRepo:                 invoiceRepo,
		featureUsageTrackingService: featureUsageTrackingService,
	}, nil
}
