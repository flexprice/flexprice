package internal

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
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

const (
	dateLayout                   = "2006-01-02"
	defaultReconciliationWorkers = 10
	envReconciliationWorkers     = "RECONCILIATION_WORKERS"
	envStartTime                 = "START_TIME"
	envEndTime                   = "END_TIME"
)

// AnalyticsInvoiceDiffRow represents one row in the reconciliation diff CSV
type AnalyticsInvoiceDiffRow struct {
	CustomerID         string
	CustomerName       string
	ExternalCustomerID string
	AnalyticsTotalCost string
	InvoiceSubtotalSum string
	Diff               string
	InvoiceIDs         string
	InvoiceCount       int
	Currency           string
}

type analyticsInvoiceReconciliationScript struct {
	log                         *logger.Logger
	customerRepo                customer.Repository
	invoiceRepo                 invoice.Repository
	featureUsageTrackingService service.FeatureUsageTrackingService
}

// RunAnalyticsInvoiceReconciliation compares analytics total cost to invoice subtotals for a period and writes diffs to CSV.
// Requires TENANT_ID and ENVIRONMENT_ID. Optional START_TIME and END_TIME (date 2006-01-02 or ISO-8601); default is start of current year through end of today UTC.
// Infrastructure (Postgres, ClickHouse, Kafka) must be running.
//
// Invoice side uses stored subtotals only: values come from invoiceRepo.List() (inv.Subtotal).
// Recalculated totals from GetInvoiceWithBreakdown()/recalculateInvoiceTotals() are not used,
// so the report may show divergence where DB-stored subtotals differ from in-memory recalculated totals.
func RunAnalyticsInvoiceReconciliation() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	startTime, endTime, err := parseReconciliationPeriod()
	if err != nil {
		return err
	}

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

	// Build list of customers to process (same tenant/env, with external_customer_id)
	var toProcess []*customer.Customer
	for _, c := range customers {
		if c.TenantID != tenantID || c.EnvironmentID != environmentID {
			continue
		}
		if c.ExternalID == "" {
			continue
		}
		toProcess = append(toProcess, c)
	}
	workers := getReconciliationWorkers()
	log.Printf("Processing %d customers with %d parallel workers, appending diff rows to CSV as each completes", len(toProcess), workers)

	// 2) Run N workers: collect one row per customer, then sort by diff ascending (most negative first) and write CSV
	outputFile := fmt.Sprintf("analytics_invoice_reconciliation_%s_%s.csv", tenantID, time.Now().Format("20060102_150405"))
	rows, completed := runReconciliationWorkers(
		ctx, script, toProcess, startTime, endTime, workers,
	)
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].diff.LessThan(rows[j].diff)
	})

	csvFile, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("create CSV: %w", err)
	}
	defer csvFile.Close()
	csvWriter := csv.NewWriter(csvFile)
	header := []string{
		"customer_id", "customer_name", "external_customer_id",
		"analytics_total_cost", "invoice_subtotal_sum", "diff",
		"invoice_ids", "invoice_count_in_period", "total_invoice_count", "currency",
	}
	if err := csvWriter.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, r := range rows {
		if err := csvWriter.Write(r.record); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("flush CSV: %w", err)
	}
	if len(rows) == 0 {
		log.Printf("Wrote CSV with header only to %s (completed %d/%d customers)", outputFile, completed, len(toProcess))
	} else {
		log.Printf("Wrote %d customer rows sorted by diff ascending to %s (completed %d/%d customers)", len(rows), outputFile, completed, len(toProcess))
	}
	return nil
}

// parseReconciliationPeriod returns start and end time for the reconciliation period.
// Uses START_TIME and END_TIME env vars (date-only 2006-01-02 or ISO-8601). If unset,
// defaults to start of current year and end of current day UTC so invoice/list and analytics match real data.
func parseReconciliationPeriod() (startTime, endTime time.Time, err error) {
	now := time.Now().UTC()
	defaultStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	defaultEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 999999999, time.UTC)

	startStr := os.Getenv(envStartTime)
	endStr := os.Getenv(envEndTime)

	if startStr == "" {
		startTime = defaultStart
	} else {
		startTime, err = parseTime(startStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid START_TIME %q: %w", startStr, err)
		}
	}
	if endStr == "" {
		endTime = defaultEnd
	} else {
		endTime, err = parseTime(endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid END_TIME %q: %w", endStr, err)
		}
	}
	if startTime.After(endTime) {
		return time.Time{}, time.Time{}, fmt.Errorf("START_TIME must be before or equal to END_TIME")
	}
	return startTime, endTime, nil
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse(dateLayout, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("use date (2006-01-02) or ISO-8601")
}

func getReconciliationWorkers() int {
	s := os.Getenv(envReconciliationWorkers)
	if s == "" {
		return defaultReconciliationWorkers
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return defaultReconciliationWorkers
	}
	return n
}

type analyticsResult struct {
	customer  *customer.Customer
	totalCost decimal.Decimal
	err       error
}

// reconciliationRow holds a CSV record and its diff for sorting (ascending = most negative first).
type reconciliationRow struct {
	record []string
	diff   decimal.Decimal
}

// runReconciliationWorkers runs GetDetailedUsageAnalytics with a worker pool; as each result
// arrives, fetches invoices for that customer (by customer ID, created in period), sums subtotal,
// and collects one row per customer. Returns rows (to be sorted by diff) and completed count.
func runReconciliationWorkers(
	ctx context.Context,
	script *analyticsInvoiceReconciliationScript,
	customers []*customer.Customer,
	startTime, endTime time.Time,
	workers int,
) (rows []reconciliationRow, completed int) {
	if len(customers) == 0 {
		return nil, 0
	}
	if workers > len(customers) {
		workers = len(customers)
	}

	jobCh := make(chan *customer.Customer, len(customers))
	resultCh := make(chan analyticsResult, workers*2)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobCh {
				req := &dto.GetUsageAnalyticsRequest{
					ExternalCustomerID: c.ExternalID,
					StartTime:          startTime,
					EndTime:            endTime,
				}
				resp, err := script.featureUsageTrackingService.GetDetailedUsageAnalytics(ctx, req)
				if err != nil {
					resultCh <- analyticsResult{customer: c, err: err}
					continue
				}
				resultCh <- analyticsResult{customer: c, totalCost: resp.TotalCost}
			}
		}()
	}

	go func() {
		for _, c := range customers {
			jobCh <- c
		}
		close(jobCh)
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		completed++
		if r.err != nil {
			log.Printf("Warning: analytics for customer %s: %v", r.customer.ID, r.err)
			if completed%100 == 0 || completed == len(customers) {
				log.Printf("Progress: %d/%d customers (%d rows collected)", completed, len(customers), len(rows))
			}
			continue
		}
		c := r.customer
		analyticsCost := r.totalCost

		invFilter := types.NewNoLimitInvoiceFilter()
		invFilter.CustomerID = c.ID
		invFilter.CreatedAtGTE = &startTime
		invFilter.CreatedAtLTE = &endTime
		invFilter.SkipLineItems = true
		invFilter.InvoiceType = types.InvoiceTypeSubscription
		invoices, err := script.invoiceRepo.List(ctx, invFilter)
		if err != nil {
			log.Printf("Warning: list invoices for customer %s: %v", c.ID, err)
			if completed%100 == 0 || completed == len(customers) {
				log.Printf("Progress: %d/%d customers (%d rows collected)", completed, len(customers), len(rows))
			}
			continue
		}
		var storedSubtotal decimal.Decimal
		var ids []string
		for _, inv := range invoices {
			storedSubtotal = storedSubtotal.Add(inv.Subtotal)
			ids = append(ids, inv.ID)
		}
		inPeriodCount := len(ids)

		// Total invoice count for this customer (all time, same tenant/env)
		totalFilter := types.NewNoLimitInvoiceFilter()
		totalFilter.CustomerID = c.ID
		totalFilter.SkipLineItems = true
		totalFilter.InvoiceType = types.InvoiceTypeSubscription
		totalInvoiceCount, countErr := script.invoiceRepo.Count(ctx, totalFilter)
		if countErr != nil {
			log.Printf("Warning: count invoices for customer %s: %v", c.ID, countErr)
			totalInvoiceCount = 0
		}

		diff := analyticsCost.Sub(storedSubtotal)
		customerName := c.Name
		if customerName == "" {
			customerName = c.ExternalID
		}
		if customerName == "" {
			customerName = c.ID
		}
		idList := ""
		for _, id := range ids {
			if idList != "" {
				idList += ";"
			}
			idList += id
		}
		record := []string{
			c.ID, customerName, c.ExternalID,
			analyticsCost.StringFixed(4), storedSubtotal.StringFixed(4), diff.StringFixed(4),
			idList, fmt.Sprintf("%d", inPeriodCount), fmt.Sprintf("%d", totalInvoiceCount), "USD",
		}
		rows = append(rows, reconciliationRow{record: record, diff: diff})
		if completed%100 == 0 || completed == len(customers) {
			log.Printf("Progress: %d/%d customers (%d rows collected)", completed, len(customers), len(rows))
		}
	}
	return rows, completed
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
	settingsRepo := entRepo.NewSettingsRepository(pgClient, logger, cacheClient)
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
		SettingsRepo:             settingsRepo,
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
