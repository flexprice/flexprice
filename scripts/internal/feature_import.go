package internal

import (
	"context"
	"encoding/csv"
	"encoding/json"
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
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
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
	PricesUpdated   int
	Errors          []string
}

type featureImportScript struct {
	cfg            *config.Configuration
	log            *logger.Logger
	featureService service.FeatureService
	priceService   service.PriceService
	pgClient       postgres.IClient
	summary        FeatureImportSummary
	summaryMu      sync.Mutex
	tenantID       string
	environmentID  string
	planID         string

	// In-memory caches populated by bulk-load before processing rows.
	// These are read-only during concurrent processing (writes only happen
	// during single-threaded bulk-load or under mutex for newly created items).
	featureCacheMu sync.RWMutex
	featureCache   map[string]*dto.FeatureResponse // lookupKey -> FeatureResponse

	priceCacheMu sync.RWMutex
	priceCache   map[string]*price.Price // meterID -> Price (for the plan)
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
		pgClient:       pgClient,
		summary:        FeatureImportSummary{},
		tenantID:       tenantID,
		environmentID:  environmentID,
		planID:         planID,
		featureCache:   make(map[string]*dto.FeatureResponse),
		priceCache:     make(map[string]*price.Price),
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

// bulkLoadFeatures fetches all features in bulk and populates the featureCache map (lookupKey -> FeatureResponse).
// This replaces per-row getFeatureByLookupKey calls with a single bulk query.
func (s *featureImportScript) bulkLoadFeatures(ctx context.Context) error {
	filter := types.NewNoLimitFeatureFilter()

	features, err := s.featureService.GetFeatures(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to bulk load features: %w", err)
	}

	for _, f := range features.Items {
		if f.Feature != nil && f.Feature.LookupKey != "" {
			s.featureCache[f.Feature.LookupKey] = f
		}
	}

	s.log.Infow("Bulk loaded features into cache", "count", len(s.featureCache))
	return nil
}

// bulkLoadPrices fetches all published prices for the plan in bulk and populates the priceCache map (meterID -> Price).
// This replaces per-row getExistingPrice calls with a single bulk query.
func (s *featureImportScript) bulkLoadPrices(ctx context.Context) error {
	priceFilter := types.NewNoLimitPriceFilter().
		WithEntityIDs([]string{s.planID}).
		WithStatus(types.StatusPublished).
		WithEntityType(types.PRICE_ENTITY_TYPE_PLAN)

	prices, err := s.priceService.GetPrices(ctx, priceFilter)
	if err != nil {
		return fmt.Errorf("failed to bulk load prices: %w", err)
	}

	for _, p := range prices.Items {
		if p.Price == nil || p.Price.MeterID == "" {
			continue
		}
		// Skip prices with end_date (expired/superseded)
		if p.Price.EndDate != nil {
			s.log.Debugw("Skipping price with end date during bulk load",
				"price_id", p.Price.ID,
				"meter_id", p.Price.MeterID,
				"end_date", p.Price.EndDate,
			)
			continue
		}
		// First active price wins per meter_id (same as the old per-row logic)
		if _, exists := s.priceCache[p.Price.MeterID]; !exists {
			s.priceCache[p.Price.MeterID] = p.Price
		}
	}

	s.log.Infow("Bulk loaded prices into cache", "count", len(s.priceCache))
	return nil
}

// getCachedFeature returns the cached feature for a lookup key, or nil if not found
func (s *featureImportScript) getCachedFeature(lookupKey string) *dto.FeatureResponse {
	s.featureCacheMu.RLock()
	defer s.featureCacheMu.RUnlock()
	return s.featureCache[lookupKey]
}

// getCachedPrice returns the cached price for a meter ID, or nil if not found
func (s *featureImportScript) getCachedPrice(meterID string) *price.Price {
	s.priceCacheMu.RLock()
	defer s.priceCacheMu.RUnlock()
	return s.priceCache[meterID]
}

// addFeatureToCache adds a newly created feature to the in-memory cache (thread-safe)
func (s *featureImportScript) addFeatureToCache(lookupKey string, f *dto.FeatureResponse) {
	s.featureCacheMu.Lock()
	defer s.featureCacheMu.Unlock()
	s.featureCache[lookupKey] = f
}

// addPriceToCache adds a newly created price to the in-memory cache (thread-safe)
func (s *featureImportScript) addPriceToCache(meterID string, p *price.Price) {
	s.priceCacheMu.Lock()
	defer s.priceCacheMu.Unlock()
	s.priceCache[meterID] = p
}

// updatePriceAmount updates the amount and display_amount of an existing price using raw SQL
// (bypassing Ent's immutable field constraint). It also stores the old pricing in metadata
// with a timestamped key for audit trail: price_YYYYMMDDHHMMSS = old_amount
func (s *featureImportScript) updatePriceAmount(ctx context.Context, existingPrice *price.Price, newAmount decimal.Decimal) error {
	now := time.Now().UTC()
	timestampKey := fmt.Sprintf("price_%s", now.Format("20060102150405"))

	// Build updated metadata: merge existing metadata with the old price record
	metadata := make(map[string]string)
	for k, v := range existingPrice.Metadata {
		metadata[k] = v
	}
	metadata[timestampKey] = existingPrice.Amount.String()

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Compute new display_amount
	newDisplayAmount := fmt.Sprintf("%s%s", existingPrice.GetCurrencySymbol(), newAmount.String())

	query := `UPDATE prices SET amount = $1, display_amount = $2, metadata = $3, updated_at = $4 WHERE id = $5`
	_, err = s.pgClient.Writer(ctx).ExecContext(ctx, query, newAmount, newDisplayAmount, string(metadataJSON), now, existingPrice.ID)
	if err != nil {
		return fmt.Errorf("failed to update price via raw SQL: %w", err)
	}

	return nil
}

// processRow processes a single row from the CSV and creates a feature and price
func (s *featureImportScript) processRow(ctx context.Context, row FeatureRow) error {
	// Validate aggregation type
	aggregationType := types.AggregationType(row.AggregationType)
	if !aggregationType.Validate() {
		return fmt.Errorf("invalid aggregation type: %s", row.AggregationType)
	}

	// Check if feature already exists in cache (populated by bulkLoadFeatures)
	existingFeature := s.getCachedFeature(row.FeatureName)

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
		var err error
		feature, err = s.featureService.CreateFeature(ctx, featureReq)
		if err != nil {
			return fmt.Errorf("failed to create feature: %w", err)
		}

		meterID = feature.Feature.MeterID

		// Add newly created feature to cache so parallel workers see it
		s.addFeatureToCache(row.FeatureName, feature)

		s.summaryMu.Lock()
		s.summary.FeaturesCreated++
		s.summaryMu.Unlock()
		s.log.Infow("Created feature", "feature_name", row.FeatureName, "event_name", row.EventName, "meter_id", meterID)
	}

	// Check if price already exists in cache (populated by bulkLoadPrices)
	existingPrice := s.getCachedPrice(meterID)

	if existingPrice != nil {
		// Price exists - check if the per-unit amount has changed
		if existingPrice.Amount.Equal(row.PricePerUnit) {
			// Same price, skip
			s.summaryMu.Lock()
			s.summary.PricesSkipped++
			s.summaryMu.Unlock()
			s.log.Infow("Price already exists with same amount, skipping",
				"feature_name", row.FeatureName,
				"meter_id", meterID,
				"plan_id", s.planID,
				"amount", row.PricePerUnit,
			)
			return nil
		}

		// Skip if new price is zero as it means the price is not available in new sheet but we have the data
		if row.PricePerUnit.Equal(decimal.Zero) {
			s.summaryMu.Lock()
			s.summary.PricesSkipped++
			s.summaryMu.Unlock()
			s.log.Infow("Price is zero, skipping", "feature_name", row.FeatureName, "meter_id", meterID, "plan_id", s.planID, "amount", row.PricePerUnit)
			return nil
		}

		// Price has changed - update via raw SQL
		oldAmount := existingPrice.Amount
		if err := s.updatePriceAmount(ctx, existingPrice, row.PricePerUnit); err != nil {
			return fmt.Errorf("failed to update price amount: %w", err)
		}

		s.summaryMu.Lock()
		s.summary.PricesUpdated++
		s.summaryMu.Unlock()
		s.log.Infow("Price amount updated",
			"feature_name", row.FeatureName,
			"meter_id", meterID,
			"plan_id", s.planID,
			"price_id", existingPrice.ID,
			"old_amount", oldAmount,
			"new_amount", row.PricePerUnit,
		)
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
		MeterID:              meterID,
		InvoiceCadence:       types.InvoiceCadenceArrear,
		DisplayName:          row.FeatureName,
		SkipEntityValidation: false,
		StartDate:            &startDate,
	}

	createdPrice, err := s.priceService.CreatePrice(ctx, priceReq)
	if err != nil {
		return fmt.Errorf("failed to create price: %w", err)
	}

	// Add newly created price to cache so parallel workers see it
	if createdPrice != nil && createdPrice.Price != nil {
		s.addPriceToCache(meterID, createdPrice.Price)
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
		"prices_updated", s.summary.PricesUpdated,
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

	// Bulk-load all existing features and prices into in-memory caches.
	// This eliminates per-row DB lookups and dramatically speeds up the import.
	script.log.Infow("Bulk loading existing features and prices into cache...")
	if err := script.bulkLoadFeatures(ctx); err != nil {
		return fmt.Errorf("failed to bulk load features: %w", err)
	}
	if err := script.bulkLoadPrices(ctx); err != nil {
		return fmt.Errorf("failed to bulk load prices: %w", err)
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
