package internal

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	chRepo "github.com/flexprice/flexprice/internal/repository/clickhouse"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/s3"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/typst"
)

// noopEventPublisher satisfies publisher.EventPublisher for scripts that should not publish to Kafka.
type noopEventPublisher struct{}

func (noopEventPublisher) Publish(ctx context.Context, event *events.Event) error {
	return nil
}

// migrateCalendarBillingRow is built from a CSV whose headers match subscription Ent fields
// (see ent/schema/subscription.go + BaseMixin + EnvironmentMixin). Primary key column is `id`.
type migrateCalendarBillingRow struct {
	SubscriptionID string // from column id (or alias subscription_id)
	CustomerID     string
	PlanID         string
	Currency       string
	TenantID       string // from CSV; required on each row
	EnvironmentID  string // from CSV; required on each row
}

type migrateCalendarBillingScript struct {
	db              postgres.IClient
	subscriptionSvc service.SubscriptionService
	subRepo         subscription.Repository
}

// MigrateCalendarBillingCSV schedules cancellation of existing subscriptions and creates new
// calendar-billing subscriptions from a CSV file, one DB transaction per row.
//
// Environment (set via scripts/main.go flags):
//   - FILE_PATH (required): comma-separated CSV path
//   - EFFECTIVE_DATE (required): RFC3339 or YYYY-MM-DD — cancel_at / new subscription start
//   - TENANT_ID, ENVIRONMENT_ID (optional): when set, only rows whose tenant_id / environment_id match are processed
//   - Each row must include tenant_id and environment_id (from the export); per-row context uses those values.
//   - FAILED_OUTPUT_PATH (optional): defaults to failed_calendar_billing_migration.csv in cwd
//   - DRY_RUN (optional): true / 1 to validate only
//   - WORKER_COUNT (optional): concurrent workers
//   - POSITIONAL_CSV (optional): true / 1 — no header row; columns follow public.subscriptions export order (see postgresSubscriptionExportCol*).
//   - POSITIONAL_SKIP_HEADER (optional): with POSITIONAL_CSV, skip the first line (e.g. header row from a tool).
func MigrateCalendarBillingCSV() error {
	filterTenantID := os.Getenv("TENANT_ID")
	filterEnvironmentID := os.Getenv("ENVIRONMENT_ID")
	filePath := os.Getenv("FILE_PATH")
	effectiveDateStr := os.Getenv("EFFECTIVE_DATE")
	failedOut := os.Getenv("FAILED_OUTPUT_PATH")
	dryRun := os.Getenv("DRY_RUN") == "true" || os.Getenv("DRY_RUN") == "1"
	positional := os.Getenv("POSITIONAL_CSV") == "true" || os.Getenv("POSITIONAL_CSV") == "1"
	positionalSkipHeader := os.Getenv("POSITIONAL_SKIP_HEADER") == "true" || os.Getenv("POSITIONAL_SKIP_HEADER") == "1"
	workerCount := 5
	if w := os.Getenv("WORKER_COUNT"); w != "" {
		if n, err := strconv.Atoi(w); err == nil && n > 0 {
			workerCount = n
		}
	}

	if filePath == "" || effectiveDateStr == "" {
		return fmt.Errorf("FILE_PATH and EFFECTIVE_DATE are required")
	}
	if failedOut == "" {
		failedOut = "failed_calendar_billing_migration.csv"
	}

	effectiveDate, err := parseEffectiveDate(effectiveDateStr)
	if err != nil {
		return err
	}
	if !effectiveDate.After(time.Now().UTC()) {
		return fmt.Errorf("EFFECTIVE_DATE must be in the future (required by CancelSubscription validation)")
	}

	rows, err := readCalendarBillingCSV(filePath, filterTenantID, filterEnvironmentID, positional, positionalSkipHeader)
	if err != nil {
		return err
	}

	script, err := newMigrateCalendarBillingScript()
	if err != nil {
		return fmt.Errorf("failed to initialize script: %w", err)
	}

	baseCtx := context.Background()
	if uid := os.Getenv("USER_ID"); uid != "" {
		baseCtx = context.WithValue(baseCtx, types.CtxUserID, uid)
	}

	log.Printf("migrate-calendar-billing-csv: %d rows, effective=%s, workers=%d, dry_run=%v, positional=%v, skip_header=%v, failed_out=%s\n",
		len(rows), effectiveDate.UTC().Format(time.RFC3339), workerCount, dryRun, positional, positionalSkipHeader, failedOut)

	failedWriter, failedFile, err := openFailedCSV(failedOut)
	if err != nil {
		return err
	}
	defer failedFile.Close()

	var (
		mu            sync.Mutex
		seenRowKey    = make(map[string]struct{})
		ok, skipped   int
		failed        int
		failedWriteMu sync.Mutex
	)

	jobs := make(chan migrateCalendarBillingRow)
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for row := range jobs {
			reason, err := script.processRow(baseCtx, row, effectiveDate, dryRun)
			mu.Lock()
			switch {
			case err != nil:
				failed++
				mu.Unlock()
				failedWriteMu.Lock()
				_ = failedWriter.Write([]string{
					row.TenantID, row.EnvironmentID,
					row.SubscriptionID, row.CustomerID, row.PlanID, row.Currency, err.Error(),
				})
				failedWriter.Flush()
				failedWriteMu.Unlock()
				log.Printf("FAILED sub=%s: %v\n", row.SubscriptionID, err)
			case reason != "":
				skipped++
				mu.Unlock()
				log.Printf("SKIPPED sub=%s: %s\n", row.SubscriptionID, reason)
			default:
				ok++
				mu.Unlock()
				log.Printf("SUCCESS sub=%s\n", row.SubscriptionID)
			}
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

	for _, row := range rows {
		if row.SubscriptionID == "" {
			continue
		}
		dupKey := row.TenantID + "\x00" + row.EnvironmentID + "\x00" + row.SubscriptionID
		mu.Lock()
		if _, dup := seenRowKey[dupKey]; dup {
			mu.Unlock()
			log.Printf("SKIPPED duplicate row (tenant/env/sub): %s / %s / %s\n", row.TenantID, row.EnvironmentID, row.SubscriptionID)
			continue
		}
		seenRowKey[dupKey] = struct{}{}
		mu.Unlock()
		jobs <- row
	}
	close(jobs)
	wg.Wait()

	log.Printf("Summary: success=%d skipped=%d failed=%d (failed rows appended to %s)\n", ok, skipped, failed, failedOut)
	if failed > 0 {
		return fmt.Errorf("completed with %d failed rows", failed)
	}
	return nil
}

func parseEffectiveDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t.UTC(), nil
	}
	t, err = time.Parse("2006-01-02", s)
	if err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("invalid EFFECTIVE_DATE (use RFC3339 or YYYY-MM-DD): %w", err)
}

func utcDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func csvCell(rec []string, colIdx map[string]int, col string) string {
	i, ok := colIdx[col]
	if !ok || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

// postgresSubscriptionExportCol* are 0-based column indices for a typical
// SELECT * FROM subscriptions (or pg_dump) row order:
//
//	id, lookup_key, customer_id, plan_id, subscription_status, status, currency,
//	billing_anchor, start_date, end_date, current_period_start, current_period_end,
//	cancelled_at, cancel_at, cancel_at_period_end, trial_start, trial_end,
//	invoice_cadence, billing_cadence, billing_period, billing_period_count,
//	tenant_id, created_at, updated_at, created_by, updated_by, version, metadata,
//	environment_id, pause_status, active_pause_id, billing_cycle, commitment_amount,
//	overage_factor, payment_behavior, collection_method, gateway_payment_method_id,
//	customer_timezone, proration_behavior, enable_true_up, invoicing_customer_id,
//	commitment_duration, parent_subscription_id, payment_terms
const (
	pgSubColID            = 0
	pgSubColCustomerID    = 2
	pgSubColPlanID        = 3
	pgSubColCurrency      = 6
	pgSubColTenantID      = 21
	pgSubColEnvironmentID = 28
)

// readCalendarBillingCSV parses a subscription export. Default mode: first row is a header
// with Ent / DB column names. With positional, every row is data in postgresSubscriptionExportCol* order.
func readCalendarBillingCSV(path, wantTenantID, wantEnvironmentID string, positional, positionalSkipHeader bool) ([]migrateCalendarBillingRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	if positional {
		return readPositionalSubscriptionCSV(r, filepath.Base(path), wantTenantID, wantEnvironmentID, positionalSkipHeader)
	}

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	idx := make(map[string]int)
	for i, col := range header {
		key := strings.ToLower(strings.TrimSpace(col))
		if _, exists := idx[key]; !exists {
			idx[key] = i
		}
	}

	subIDCol, ok := firstColumnIndex(idx, "id", "subscription_id")
	if !ok {
		return nil, fmt.Errorf("csv missing required column %q or %q (Ent subscription primary key)", "id", "subscription_id")
	}
	for _, req := range []string{"customer_id", "plan_id", "currency", "tenant_id", "environment_id"} {
		if _, ok := idx[req]; !ok {
			return nil, fmt.Errorf("csv missing required column %q (expected Ent field name)", req)
		}
	}

	var out []migrateCalendarBillingRow
	lineNum := 1
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		lineNum++

		subID := ""
		if subIDCol < len(rec) {
			subID = strings.TrimSpace(rec[subIDCol])
		}
		row := migrateCalendarBillingRow{
			SubscriptionID: subID,
			CustomerID:     csvCell(rec, idx, "customer_id"),
			PlanID:         csvCell(rec, idx, "plan_id"),
			Currency:       strings.ToLower(csvCell(rec, idx, "currency")),
			TenantID:       csvCell(rec, idx, "tenant_id"),
			EnvironmentID:  csvCell(rec, idx, "environment_id"),
		}

		if wantTenantID != "" && row.TenantID != wantTenantID {
			return nil, fmt.Errorf("row %d: tenant_id %q does not match -tenant-id %q", lineNum, row.TenantID, wantTenantID)
		}
		if wantEnvironmentID != "" && row.EnvironmentID != wantEnvironmentID {
			return nil, fmt.Errorf("row %d: environment_id %q does not match -environment-id %q", lineNum, row.EnvironmentID, wantEnvironmentID)
		}
		if row.TenantID == "" || row.EnvironmentID == "" {
			return nil, fmt.Errorf("row %d: empty tenant_id or environment_id", lineNum)
		}

		out = append(out, row)
	}
	return out, nil
}

func readPositionalSubscriptionCSV(
	r *csv.Reader,
	fileLabel, wantTenantID, wantEnvironmentID string,
	skipFirstRow bool,
) ([]migrateCalendarBillingRow, error) {
	var out []migrateCalendarBillingRow
	lineNum := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		lineNum++
		if skipFirstRow && lineNum == 1 {
			continue
		}
		if len(rec) <= pgSubColEnvironmentID {
			return nil, fmt.Errorf("%s line %d: positional format needs at least %d columns (through environment_id), got %d",
				fileLabel, lineNum, pgSubColEnvironmentID+1, len(rec))
		}

		row := migrateCalendarBillingRow{
			SubscriptionID: strings.TrimSpace(rec[pgSubColID]),
			CustomerID:     strings.TrimSpace(rec[pgSubColCustomerID]),
			PlanID:         strings.TrimSpace(rec[pgSubColPlanID]),
			Currency:       strings.ToLower(strings.TrimSpace(rec[pgSubColCurrency])),
			TenantID:       strings.TrimSpace(rec[pgSubColTenantID]),
			EnvironmentID:  strings.TrimSpace(rec[pgSubColEnvironmentID]),
		}
		if wantTenantID != "" && row.TenantID != wantTenantID {
			return nil, fmt.Errorf("%s line %d: tenant_id %q does not match -tenant-id %q", fileLabel, lineNum, row.TenantID, wantTenantID)
		}
		if wantEnvironmentID != "" && row.EnvironmentID != wantEnvironmentID {
			return nil, fmt.Errorf("%s line %d: environment_id %q does not match -environment-id %q", fileLabel, lineNum, row.EnvironmentID, wantEnvironmentID)
		}
		if row.TenantID == "" || row.EnvironmentID == "" {
			return nil, fmt.Errorf("%s line %d: empty tenant_id or environment_id", fileLabel, lineNum)
		}

		out = append(out, row)
	}
	return out, nil
}

func firstColumnIndex(idx map[string]int, names ...string) (int, bool) {
	for _, n := range names {
		n = strings.ToLower(n)
		if i, ok := idx[n]; ok {
			return i, true
		}
	}
	return 0, false
}

func openFailedCSV(path string) (*csv.Writer, *os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("create failed csv: %w", err)
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"tenant_id", "environment_id", "id", "customer_id", "plan_id", "currency", "error"}); err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	w.Flush()
	return w, f, nil
}

func (s *migrateCalendarBillingScript) processRow(
	baseCtx context.Context,
	row migrateCalendarBillingRow,
	effectiveDate time.Time,
	dryRun bool,
) (skipReason string, err error) {
	if row.SubscriptionID == "" || row.CustomerID == "" || row.PlanID == "" || row.Currency == "" {
		return "", fmt.Errorf("missing required field in row")
	}
	ctx := context.WithValue(
		context.WithValue(baseCtx, types.CtxTenantID, row.TenantID),
		types.CtxEnvironmentID, row.EnvironmentID,
	)
	if err := types.ValidateCurrencyCode(row.Currency); err != nil {
		return "", err
	}

	sub, err := s.subRepo.Get(ctx, row.SubscriptionID)
	if err != nil {
		return "", err
	}
	if sub.TenantID != row.TenantID || sub.EnvironmentID != row.EnvironmentID {
		return "", fmt.Errorf("csv tenant_id/environment_id does not match subscription (sub tenant=%s env=%s)", sub.TenantID, sub.EnvironmentID)
	}
	if sub.CustomerID != row.CustomerID || sub.PlanID != row.PlanID {
		return "", fmt.Errorf("csv customer_id/plan_id does not match subscription (got customer=%s plan=%s)", sub.CustomerID, sub.PlanID)
	}

	if sub.SubscriptionStatus == types.SubscriptionStatusCancelled {
		return "already cancelled", nil
	}

	skipDup, dupReason, err := s.idempotentAlreadyMigrated(ctx, row, effectiveDate)
	if err != nil {
		return "", err
	}
	if skipDup {
		return dupReason, nil
	}

	cancelReq := &dto.CancelSubscriptionRequest{
		CancellationType:  types.CancellationTypeScheduledDate,
		CancelAt:          &effectiveDate,
		ProrationBehavior: types.ProrationBehaviorNone,
		Reason:            "migrating-to-calendar-billing",
		SuppressWebhook:   true,
	}

	monthly := types.BILLING_PERIOD_MONTHLY
	createReq := dto.CreateSubscriptionRequest{
		CustomerID:         row.CustomerID,
		PlanID:             row.PlanID,
		Currency:           row.Currency,
		StartDate:          &effectiveDate,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleCalendar,
		ProrationBehavior:  types.ProrationBehaviorNone,
		CommitmentDuration: &monthly,
		EnableTrueUp:       false,
	}

	if dryRun {
		return "", nil
	}

	skipCancel := sub.CancelAt != nil && utcDay(*sub.CancelAt).Equal(utcDay(effectiveDate))

	err = s.db.WithTx(ctx, func(txCtx context.Context) error {
		if !skipCancel {
			if _, err := s.subscriptionSvc.CancelSubscription(txCtx, row.SubscriptionID, cancelReq); err != nil {
				if ierr.IsValidation(err) && strings.Contains(strings.ToLower(err.Error()), "already scheduled") {
					return fmt.Errorf("subscription already has a scheduled cancellation; resolve manually: %w", err)
				}
				return err
			}
		}
		_, err := s.subscriptionSvc.CreateSubscription(txCtx, createReq)
		return err
	})
	if err != nil {
		return "", err
	}
	return "", nil
}

func (s *migrateCalendarBillingScript) idempotentAlreadyMigrated(
	ctx context.Context,
	row migrateCalendarBillingRow,
	effectiveDate time.Time,
) (bool, string, error) {
	f := types.NewNoLimitSubscriptionFilter()
	f.CustomerID = row.CustomerID
	f.PlanID = row.PlanID
	f.SubscriptionStatus = []types.SubscriptionStatus{
		types.SubscriptionStatusActive,
		types.SubscriptionStatusTrialing,
		types.SubscriptionStatusPaused,
	}
	subs, err := s.subRepo.List(ctx, f)
	if err != nil {
		return false, "", err
	}
	wantDay := utcDay(effectiveDate)
	for _, other := range subs {
		if other.ID == row.SubscriptionID {
			continue
		}
		if other.BillingCycle == types.BillingCycleCalendar && utcDay(other.StartDate).Equal(wantDay) {
			return true, "active calendar subscription already exists for customer+plan with same start date (idempotent skip)", nil
		}
	}
	return false, "", nil
}

func newMigrateCalendarBillingScript() (*migrateCalendarBillingScript, error) {
	cfg, err := config.NewConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	log, err := logger.NewLogger(cfg)
	if err != nil {
		return nil, fmt.Errorf("logger: %w", err)
	}

	sentrySvc := sentry.NewSentryService(cfg, log)
	entClient, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	pgClient := postgres.NewClient(entClient, log, sentrySvc)
	cacheClient := cache.NewInMemoryCache()

	chStore, err := clickhouse.NewClickHouseStore(cfg, sentrySvc)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: %w", err)
	}

	eventRepo := chRepo.NewEventRepository(chStore, log)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, log)
	featureUsageRepo := chRepo.NewFeatureUsageRepository(chStore, log)
	costSheetUsageRepo := chRepo.NewCostSheetUsageRepository(chStore, log)
	rawEventRepo := chRepo.NewRawEventRepository(chStore, log)

	customerRepo := entRepo.NewCustomerRepository(pgClient, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(pgClient, log, cacheClient)
	subscriptionRepo := entRepo.NewSubscriptionRepository(pgClient, log, cacheClient)
	subscriptionLineItemRepo := entRepo.NewSubscriptionLineItemRepository(pgClient, log, cacheClient)
	subscriptionPhaseRepo := entRepo.NewSubscriptionPhaseRepository(pgClient, log, cacheClient)
	subScheduleRepo := entRepo.NewSubscriptionScheduleRepository(pgClient, log)
	priceRepo := entRepo.NewPriceRepository(pgClient, log, cacheClient)
	priceUnitRepo := entRepo.NewPriceUnitRepository(pgClient, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, log, cacheClient)
	invoiceRepo := entRepo.NewInvoiceRepository(pgClient, log, cacheClient)
	invoiceLineItemRepo := entRepo.NewInvoiceLineItemRepository(pgClient, log, cacheClient)
	featureRepo := entRepo.NewFeatureRepository(pgClient, log, cacheClient)
	entitlementRepo := entRepo.NewEntitlementRepository(pgClient, log, cacheClient)
	walletRepo := entRepo.NewWalletRepository(pgClient, log, cacheClient)
	tenantRepo := entRepo.NewTenantRepository(pgClient, log, cacheClient)
	environmentRepo := entRepo.NewEnvironmentRepository(pgClient, log)
	paymentRepo := entRepo.NewPaymentRepository(pgClient, log, cacheClient)
	secretRepo := entRepo.NewSecretRepository(pgClient, log, cacheClient)
	taskRepo := entRepo.NewTaskRepository(pgClient, log)
	creditGrantRepo := entRepo.NewCreditGrantRepository(pgClient, log, cacheClient)
	creditGrantApplicationRepo := entRepo.NewCreditGrantApplicationRepository(pgClient, log, cacheClient)
	costSheetRepo := entRepo.NewCostsheetRepository(pgClient, log, cacheClient)
	creditNoteRepo := entRepo.NewCreditNoteRepository(pgClient, log, cacheClient)
	creditNoteLineItemRepo := entRepo.NewCreditNoteLineItemRepository(pgClient, log, cacheClient)
	taxRateRepo := entRepo.NewTaxRateRepository(pgClient, log, cacheClient)
	taxAssociationRepo := entRepo.NewTaxAssociationRepository(pgClient, log, cacheClient)
	taxAppliedRepo := entRepo.NewTaxAppliedRepository(pgClient, log, cacheClient)
	couponRepo := entRepo.NewCouponRepository(pgClient, log, cacheClient)
	couponAssociationRepo := entRepo.NewCouponAssociationRepository(pgClient, log, cacheClient)
	couponApplicationRepo := entRepo.NewCouponApplicationRepository(pgClient, log, cacheClient)
	addonRepo := entRepo.NewAddonRepository(pgClient, log, cacheClient)
	addonAssociationRepo := entRepo.NewAddonAssociationRepository(pgClient, log, cacheClient)
	connectionRepo := entRepo.NewConnectionRepository(pgClient, log, cacheClient)
	entityIntegrationMappingRepo := entRepo.NewEntityIntegrationMappingRepository(pgClient, log, cacheClient)
	settingsRepo := entRepo.NewSettingsRepository(pgClient, log, cacheClient)
	alertLogsRepo := entRepo.NewAlertLogsRepository(pgClient, log, cacheClient)
	groupRepo := entRepo.NewGroupRepository(pgClient, log, cacheClient)
	scheduledTaskRepo := entRepo.NewScheduledTaskRepository(pgClient, log)
	planPriceSyncRepo := entRepo.NewPlanPriceSyncRepository(pgClient, log)
	workflowExecutionRepo := entRepo.NewWorkflowExecutionRepository(pgClient, log, cacheClient)
	authRepo := entRepo.NewAuthRepository(pgClient, log)
	userRepo := entRepo.NewUserRepository(pgClient, log)

	enc, err := security.NewEncryptionService(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("encryption: %w", err)
	}
	integrationFactory := integration.NewFactory(
		cfg,
		log,
		connectionRepo,
		customerRepo,
		subscriptionRepo,
		invoiceRepo,
		paymentRepo,
		priceRepo,
		entityIntegrationMappingRepo,
		meterRepo,
		featureRepo,
		enc,
	)

	typstCompiler := typst.DefaultCompiler(log)
	pdfGen := pdf.NewGenerator(cfg, typstCompiler)
	s3svc, err := s3.NewService(cfg)
	if err != nil {
		return nil, fmt.Errorf("s3: %w", err)
	}

	params := service.ServiceParams{
		Logger:                       log,
		Config:                       cfg,
		DB:                           pgClient,
		PDFGenerator:                 pdfGen,
		S3:                           s3svc,
		AuthRepo:                     authRepo,
		UserRepo:                     userRepo,
		EventRepo:                    eventRepo,
		CostSheetUsageRepo:           costSheetUsageRepo,
		ProcessedEventRepo:           processedEventRepo,
		FeatureUsageRepo:             featureUsageRepo,
		RawEventRepo:                 rawEventRepo,
		MeterRepo:                    meterRepo,
		PriceRepo:                    priceRepo,
		PriceUnitRepo:                priceUnitRepo,
		CustomerRepo:                 customerRepo,
		PlanRepo:                     planRepo,
		SubRepo:                      subscriptionRepo,
		SubscriptionLineItemRepo:     subscriptionLineItemRepo,
		SubscriptionPhaseRepo:        subscriptionPhaseRepo,
		SubScheduleRepo:              subScheduleRepo,
		WalletRepo:                   walletRepo,
		TenantRepo:                   tenantRepo,
		InvoiceRepo:                  invoiceRepo,
		InvoiceLineItemRepo:          invoiceLineItemRepo,
		FeatureRepo:                  featureRepo,
		EntitlementRepo:              entitlementRepo,
		PaymentRepo:                  paymentRepo,
		SecretRepo:                   secretRepo,
		EnvironmentRepo:              environmentRepo,
		CreditGrantRepo:              creditGrantRepo,
		CreditGrantApplicationRepo:   creditGrantApplicationRepo,
		TaskRepo:                     taskRepo,
		CostSheetRepo:                costSheetRepo,
		CreditNoteRepo:               creditNoteRepo,
		CreditNoteLineItemRepo:       creditNoteLineItemRepo,
		TaxRateRepo:                  taxRateRepo,
		TaxAssociationRepo:           taxAssociationRepo,
		TaxAppliedRepo:               taxAppliedRepo,
		CouponRepo:                   couponRepo,
		CouponAssociationRepo:        couponAssociationRepo,
		CouponApplicationRepo:        couponApplicationRepo,
		AddonRepo:                    addonRepo,
		AddonAssociationRepo:         addonAssociationRepo,
		ConnectionRepo:               connectionRepo,
		EntityIntegrationMappingRepo: entityIntegrationMappingRepo,
		SettingsRepo:                 settingsRepo,
		AlertLogsRepo:                alertLogsRepo,
		GroupRepo:                    groupRepo,
		ScheduledTaskRepo:            scheduledTaskRepo,
		PlanPriceSyncRepo:            planPriceSyncRepo,
		WorkflowExecutionRepo:        workflowExecutionRepo,
		EventPublisher:               noopEventPublisher{},
		WebhookPublisher:             &mockWebhookPublisher{},
		Client:                       httpclient.NewDefaultClient(),
		ProrationCalculator:          proration.NewCalculator(log),
		IntegrationFactory:           integrationFactory,
	}

	subscriptionSvc := service.NewSubscriptionService(params)

	return &migrateCalendarBillingScript{
		db:              pgClient,
		subscriptionSvc: subscriptionSvc,
		subRepo:         subscriptionRepo,
	}, nil
}
