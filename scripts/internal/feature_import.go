package internal

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// FeatureRow represents a row in the feature CSV file
type FeatureRow struct {
	FeatureName      string          `json:"feature_name" csv:"feature_name"`
	EventName        string          `json:"event_name" csv:"event_name"`
	AggregationType  string          `json:"aggregation_type" csv:"aggregation_type"`
	AggregationField string          `json:"aggregation_field" csv:"aggregation_field"`
	PricePerUnit     decimal.Decimal `json:"price_per_unit" csv:"price_per_unit" swaggertype:"string"`
}

// FeatureImportSummary contains statistics about the import process
type FeatureImportSummary struct {
	TotalRows       int
	FeaturesCreated int
	FeaturesSkipped int
	PricesCreated   int
	PricesSkipped   int
	Errors          []string
}

type featureImportScript struct {
	cfg            *config.Configuration
	log            *logger.Logger
	featureService service.FeatureService
	priceService   service.PriceService
	summary        FeatureImportSummary
	summaryMu      sync.Mutex
	tenantID       string
	environmentID  string
	planID         string
}

func newFeatureImportScript(tenantID, environmentID, planID string) (*featureImportScript, error) {
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

	// Initialize the database client
	entClients, err := postgres.NewEntClients(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
		return nil, err
	}

	// Create postgres client
	pgClient := postgres.NewClient(entClients, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Initialize repositories
	featureRepo := entRepo.NewFeatureRepository(pgClient, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(pgClient, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(pgClient, log, cacheClient)

	// Create service params
	serviceParams := service.ServiceParams{
		Logger:           log,
		Config:           cfg,
		DB:               pgClient,
		FeatureRepo:      featureRepo,
		MeterRepo:        meterRepo,
		PriceRepo:        priceRepo,
		PlanRepo:         planRepo,
		WebhookPublisher: &mockWebhookPublisher{},
	}

	// Create services
	featureService := service.NewFeatureService(serviceParams)
	priceService := service.NewPriceService(serviceParams)

	return &featureImportScript{
		cfg:            cfg,
		log:            log,
		featureService: featureService,
		priceService:   priceService,
		summary:        FeatureImportSummary{},
		tenantID:       tenantID,
		environmentID:  environmentID,
		planID:         planID,
	}, nil
}

// parseFeatureCSV parses the feature CSV file
func (s *featureImportScript) parseFeatureCSV(filePath string) ([]FeatureRow, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	// Read header
	_, err = csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	var featureRows []FeatureRow

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.log.Errorw("Failed to read CSV record", "error", err)
			continue
		}

		// Ensure we have at least 5 columns
		if len(record) < 5 {
			s.log.Warnw("Skipping row with insufficient columns", "columns", len(record))
			continue
		}

		// Parse price per unit from string to decimal
		pricePerUnit, err := decimal.NewFromString(record[4])
		if err != nil {
			s.log.Warnw("Failed to parse price per unit, skipping row", "price_string", record[4], "error", err)
			continue
		}

		featureRow := FeatureRow{
			FeatureName:      record[0],
			EventName:        record[1],
			AggregationType:  record[2],
			AggregationField: record[3],
			PricePerUnit:     pricePerUnit,
		}

		featureRows = append(featureRows, featureRow)
	}

	s.summary.TotalRows = len(featureRows)
	s.log.Infow("Parsed feature CSV", "total_rows", len(featureRows))
	return featureRows, nil
}

// getFeatureByLookupKey gets a feature by lookup key
func (s *featureImportScript) getFeatureByLookupKey(ctx context.Context, lookupKey string) (*dto.FeatureResponse, error) {
	filter := types.NewDefaultFeatureFilter()
	filter.LookupKey = lookupKey
	filter.QueryFilter.Limit = lo.ToPtr(1)

	features, err := s.featureService.GetFeatures(ctx, filter)
	if err != nil {
		return nil, err
	}

	if len(features.Items) == 0 {
		return nil, nil
	}

	return features.Items[0], nil
}

// checkPriceExists checks if a price with the given meter_id already exists for the plan
func (s *featureImportScript) checkPriceExists(ctx context.Context, planID, meterID string) (bool, error) {
	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{planID}).
		WithStatus(types.StatusPublished).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN)
	priceFilter.MeterIDs = []string{meterID}

	prices, err := s.priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return false, err
	}

	return len(prices.Items) > 0, nil
}

// processRow processes a single row from the CSV and creates a feature and price
func (s *featureImportScript) processRow(ctx context.Context, row FeatureRow) error {
	// Validate aggregation type
	aggregationType := types.AggregationType(row.AggregationType)
	if !aggregationType.Validate() {
		return fmt.Errorf("invalid aggregation type: %s", row.AggregationType)
	}

	// Check if feature already exists by lookup key
	existingFeature, err := s.getFeatureByLookupKey(ctx, row.FeatureName)
	if err != nil {
		return fmt.Errorf("failed to check if feature exists: %w", err)
	}

	var feature *dto.FeatureResponse
	var meterID string

	if existingFeature != nil {
		// Feature exists, use it
		feature = existingFeature
		meterID = feature.Feature.MeterID
		s.summaryMu.Lock()
		s.summary.FeaturesSkipped++
		s.summaryMu.Unlock()
		s.log.Infow("Feature already exists, skipping creation", "feature_name", row.FeatureName, "feature_id", feature.Feature.ID, "meter_id", meterID)
	} else {
		// Create aggregation object
		aggregation := meter.Aggregation{
			Type:  aggregationType,
			Field: row.AggregationField,
		}

		// Create meter request with filters
		meterReq := &dto.CreateMeterRequest{
			Name:        row.FeatureName,
			EventName:   row.EventName,
			Aggregation: aggregation,
			ResetUsage:  types.ResetUsageBillingPeriod,
			Filters: []meter.Filter{
				{
					Key:    "byok",
					Values: []string{"false"},
				},
			},
		}

		// Create feature request
		featureReq := dto.CreateFeatureRequest{
			Name:      row.FeatureName,
			LookupKey: row.FeatureName,
			Type:      types.FeatureTypeMetered,
			Meter:     meterReq,
		}

		// Create feature using service
		feature, err = s.featureService.CreateFeature(ctx, featureReq)
		if err != nil {
			return fmt.Errorf("failed to create feature: %w", err)
		}

		meterID = feature.Feature.MeterID
		s.summaryMu.Lock()
		s.summary.FeaturesCreated++
		s.summaryMu.Unlock()
		s.log.Infow("Created feature", "feature_name", row.FeatureName, "event_name", row.EventName, "meter_id", meterID)
	}

	// Check if price already exists for this meter and plan
	priceExists, err := s.checkPriceExists(ctx, s.planID, meterID)
	if err != nil {
		return fmt.Errorf("failed to check if price exists: %w", err)
	}

	if priceExists {
		s.summaryMu.Lock()
		s.summary.PricesSkipped++
		s.summaryMu.Unlock()
		s.log.Infow("Price already exists, skipping creation", "feature_name", row.FeatureName, "meter_id", meterID, "plan_id", s.planID)
		return nil
	}

	startDate := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Create price
	priceReq := dto.CreatePriceRequest{
		Amount:               &row.PricePerUnit,
		Currency:             "USD",
		EntityType:           types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:             s.planID,
		Type:                 types.PRICE_TYPE_USAGE,
		PriceUnitType:        types.PRICE_UNIT_TYPE_FIAT,
		BillingPeriod:        types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount:   1,
		BillingModel:         types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:       types.BILLING_CADENCE_RECURRING,
		MeterID:              meterID,
		InvoiceCadence:       types.InvoiceCadenceArrear,
		DisplayName:          row.FeatureName,
		SkipEntityValidation: false,
		StartDate:            &startDate,
	}

	_, err = s.priceService.CreatePrice(ctx, priceReq)
	if err != nil {
		return fmt.Errorf("failed to create price: %w", err)
	}

	s.summaryMu.Lock()
	s.summary.PricesCreated++
	s.summaryMu.Unlock()
	s.log.Infow("Created price", "feature_name", row.FeatureName, "meter_id", meterID, "plan_id", s.planID, "amount", row.PricePerUnit)
	return nil
}

// printSummary prints a summary of the import process
func (s *featureImportScript) printSummary() {
	s.log.Infow("Feature import summary",
		"total_rows", s.summary.TotalRows,
		"features_created", s.summary.FeaturesCreated,
		"features_skipped", s.summary.FeaturesSkipped,
		"prices_created", s.summary.PricesCreated,
		"prices_skipped", s.summary.PricesSkipped,
		"errors", len(s.summary.Errors),
	)

	if len(s.summary.Errors) > 0 {
		s.log.Infow("Errors encountered during import", "errors", s.summary.Errors)
	}
}

// ImportFeatures is the main function to import features from a CSV file
func ImportFeatures() error {
	var filePath, tenantID, environmentID, planID string
	filePath = os.Getenv("FILE_PATH")
	tenantID = os.Getenv("TENANT_ID")
	environmentID = os.Getenv("ENVIRONMENT_ID")
	planID = os.Getenv("PLAN_ID")

	if filePath == "" {
		return fmt.Errorf("file path is required (set FILE_PATH environment variable)")
	}

	if tenantID == "" {
		return fmt.Errorf("tenant ID is required (set TENANT_ID environment variable)")
	}

	if environmentID == "" {
		return fmt.Errorf("environment ID is required (set ENVIRONMENT_ID environment variable)")
	}

	if planID == "" {
		return fmt.Errorf("plan ID is required (set PLAN_ID environment variable)")
	}

	script, err := newFeatureImportScript(tenantID, environmentID, planID)
	if err != nil {
		return fmt.Errorf("failed to initialize feature import script: %w", err)
	}

	// Create a context with tenant ID and environment ID
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// Parse the CSV file
	featureRows, err := script.parseFeatureCSV(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse feature CSV: %w", err)
	}

	// Get worker count from environment variable (default: 10)
	workerCount := 10
	if workerCountStr := os.Getenv("WORKER_COUNT"); workerCountStr != "" {
		if count, err := strconv.Atoi(workerCountStr); err == nil && count > 0 {
			workerCount = count
		}
	}

	script.log.Infow("Starting feature import",
		"file", filePath,
		"row_count", len(featureRows),
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"plan_id", planID,
		"worker_count", workerCount)

	// Create channels for job distribution
	type job struct {
		index int
		row   FeatureRow
	}

	jobs := make(chan job, len(featureRows))
	var wg sync.WaitGroup

	// Start worker goroutines
	for w := 1; w <= workerCount; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := range jobs {
				script.log.Infow("Processing row", "worker", workerID, "index", j.index, "feature_name", j.row.FeatureName, "event_name", j.row.EventName)
				err := script.processRow(ctx, j.row)
				if err != nil {
					script.log.Errorw("Failed to process row", "worker", workerID, "index", j.index, "feature_name", j.row.FeatureName, "error", err)
					script.summaryMu.Lock()
					script.summary.Errors = append(script.summary.Errors, fmt.Sprintf("Row %d (%s): %v", j.index, j.row.FeatureName, err))
					script.summaryMu.Unlock()
				}
			}
		}(w)
	}

	// Send jobs to workers
	for i, row := range featureRows {
		jobs <- job{index: i, row: row}
	}
	close(jobs)

	// Wait for all workers to complete
	wg.Wait()

	// Print summary
	script.printSummary()

	return nil
}
