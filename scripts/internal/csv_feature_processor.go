package internal

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
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

// go run scripts/main.go -cmd process-csv-features \
//   -file-path "file_path" \
//   -tenant-id "tenant_id" \
//   -environment-id "env_id" \
//   -user-id "dde9a118-f186-45a6-969b-a9dab4590a75" \
//   -plan-id "plan_id" \
//   -addon-id "addon_id"

// mockWebhookPublisher is a no-op webhook publisher for scripts
type mockWebhookPublisher struct{}

func (m *mockWebhookPublisher) PublishEvent(ctx context.Context, eventType string, data interface{}) error {
	// No-op for scripts
	return nil
}

func (m *mockWebhookPublisher) PublishWebhook(ctx context.Context, event *types.WebhookEvent) error {
	// No-op for scripts
	return nil
}

func (m *mockWebhookPublisher) Close() error {
	// No-op for scripts
	return nil
}

// CSVFeatureRecord represents a single record from the CSV file
type CSVFeatureRecord struct {
	FeatureName string
	EventName   string
	Aggregation string
	PlanName    string // serverless or dedicated
	Amount      string
	Currency    string
}

// CSVFeatureProcessor handles the processing of CSV data for features and prices
type CSVFeatureProcessor struct {
	cfg           *config.Configuration
	log           *logger.Logger
	featureRepo   feature.Repository
	meterRepo     meter.Repository
	priceRepo     price.Repository
	planRepo      plan.Repository
	addonRepo     addon.Repository
	entClient     *postgres.EntClients
	pgClient      postgres.IClient
	serviceParams service.ServiceParams
	// Service instances for reuse
	meterService   service.MeterService
	featureService service.FeatureService
	priceService   service.PriceService
	addonService   service.AddonService
	tenantID       string
	environmentID  string
	userID         string
	summary        ProcessingSummary
}

// ProcessingSummary contains statistics about the processing
type ProcessingSummary struct {
	TotalRows       int
	MetersCreated   int
	FeaturesCreated int
	PricesCreated   int
	Errors          []string
}

func newCSVFeatureProcessor(tenantID, environmentID, userID string) (*CSVFeatureProcessor, error) {
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
		return nil, err
	}
	pgClient := postgres.NewClient(entClient, log, sentryService)
	cacheClient := cache.NewInMemoryCache()

	// Initialize repositories
	featureRepo := entRepo.NewFeatureRepository(pgClient, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(pgClient, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(pgClient, log, cacheClient)
	addonRepo := entRepo.NewAddonRepository(pgClient, log, cacheClient)
	eventRepo := chRepo.NewEventRepository(chStore, log)
	processedEventRepo := chRepo.NewProcessedEventRepository(chStore, log)

	// Create service params (following the pattern from other scripts)
	serviceParams := service.ServiceParams{
		Logger:             log,
		Config:             cfg,
		DB:                 pgClient,
		FeatureRepo:        featureRepo,
		MeterRepo:          meterRepo,
		PriceRepo:          priceRepo,
		PlanRepo:           planRepo,
		AddonRepo:          addonRepo,
		EventRepo:          eventRepo,
		ProcessedEventRepo: processedEventRepo,
		WebhookPublisher:   &mockWebhookPublisher{},
	}

	// Create service instances once for reuse
	meterService := service.NewMeterService(meterRepo)
	featureService := service.NewFeatureService(serviceParams)
	priceService := service.NewPriceService(serviceParams)
	addonService := service.NewAddonService(serviceParams)

	return &CSVFeatureProcessor{
		cfg:            cfg,
		log:            log,
		featureRepo:    featureRepo,
		meterRepo:      meterRepo,
		priceRepo:      priceRepo,
		planRepo:       planRepo,
		addonRepo:      addonRepo,
		entClient:      entClient,
		pgClient:       pgClient,
		serviceParams:  serviceParams,
		meterService:   meterService,
		featureService: featureService,
		priceService:   priceService,
		addonService:   addonService,
		tenantID:       tenantID,
		environmentID:  environmentID,
		userID:         userID,
		summary:        ProcessingSummary{},
	}, nil
}

// parseCSV parses the CSV file
func (p *CSVFeatureProcessor) parseCSV(filePath string) ([]CSVFeatureRecord, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	// Read header to get column indices
	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	// Create column index map
	columnIndex := make(map[string]int)
	for i, col := range header {
		columnIndex[strings.ToLower(strings.TrimSpace(col))] = i
	}

	// Validate required columns
	requiredColumns := []string{"feature_name", "event_name", "aggregation", "plan_name", "amount", "currency"}
	for _, col := range requiredColumns {
		if _, exists := columnIndex[col]; !exists {
			return nil, fmt.Errorf("required column '%s' not found in CSV header", col)
		}
	}

	var records []CSVFeatureRecord

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			p.log.Errorw("Failed to read CSV record", "error", err)
			continue
		}

		if len(record) < len(requiredColumns) {
			p.log.Warnw("Skipping record with insufficient columns", "record", record)
			continue
		}

		records = append(records, CSVFeatureRecord{
			FeatureName: strings.TrimSpace(record[columnIndex["feature_name"]]),
			EventName:   strings.TrimSpace(record[columnIndex["event_name"]]),
			Aggregation: strings.TrimSpace(record[columnIndex["aggregation"]]),
			PlanName:    strings.TrimSpace(record[columnIndex["plan_name"]]),
			Amount:      strings.TrimSpace(record[columnIndex["amount"]]),
			Currency:    strings.TrimSpace(record[columnIndex["currency"]]),
		})
	}

	p.summary.TotalRows = len(records)
	p.log.Infow("Parsed CSV file", "total_rows", len(records))
	return records, nil
}

// createMeter creates a meter for the given record
func (p *CSVFeatureProcessor) createMeter(ctx context.Context, record CSVFeatureRecord) (*meter.Meter, error) {
	// Parse aggregation (supports both JSON and simple string format)
	aggregation, err := p.parseAggregation(record.Aggregation)
	if err != nil {
		return nil, err
	}

	// Create meter request
	meterReq := &dto.CreateMeterRequest{
		Name:        record.FeatureName,
		EventName:   record.EventName,
		Aggregation: aggregation,
		Filters:     []meter.Filter{}, // No filters for now
		ResetUsage:  types.ResetUsageBillingPeriod,
	}

	// Create meter using service
	return p.meterService.CreateMeter(ctx, meterReq)
}

// createFeature creates a feature for the given record
func (p *CSVFeatureProcessor) createFeature(ctx context.Context, record CSVFeatureRecord, meterID string) (*dto.FeatureResponse, error) {
	// Create feature request
	featureReq := dto.CreateFeatureRequest{
		Name:         record.FeatureName,
		Description:  fmt.Sprintf("Feature for %s", record.FeatureName),
		LookupKey:    "", // Keep lookup key empty to avoid duplicates
		Type:         types.FeatureTypeMetered,
		MeterID:      meterID,
		UnitSingular: "unit",
		UnitPlural:   "units",
		Metadata:     types.Metadata{},
	}

	// Create feature using service
	return p.featureService.CreateFeature(ctx, featureReq)
}

// createPrice creates a price for the given record
func (p *CSVFeatureProcessor) createPrice(ctx context.Context, record CSVFeatureRecord, meterID, entityID string) error {
	// Parse amount
	amount, err := decimal.NewFromString(record.Amount)
	if err != nil {
		return fmt.Errorf("invalid amount format: %w", err)
	}

	// Determine entity type and ID based on plan_name
	var entityType types.PriceEntityType

	if record.PlanName == "serverless" {
		entityType = types.PRICE_ENTITY_TYPE_PLAN
	} else if record.PlanName == "dedicated" || record.PlanName == "private" {
		entityType = types.PRICE_ENTITY_TYPE_ADDON
	} else {
		return fmt.Errorf("unsupported plan name: %s (expected 'serverless', 'dedicated', or 'private')", record.PlanName)
	}

	// Create price request
	priceReq := dto.CreatePriceRequest{
		Amount:             lo.ToPtr(amount),
		Currency:           record.Currency,
		EntityType:         entityType,
		EntityID:           entityID,
		Type:               types.PRICE_TYPE_USAGE,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		MeterID:            meterID,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		Description:        fmt.Sprintf("Price for %s", record.FeatureName),
		Metadata:           types.Metadata{},
	}

	// Create price using service
	_, err = p.priceService.CreatePrice(ctx, priceReq)
	return err
}

// parseAggregation parses the aggregation JSON string to meter.Aggregation
func (p *CSVFeatureProcessor) parseAggregation(agg string) (meter.Aggregation, error) {
	// Try to parse as JSON first
	var aggData struct {
		Type       string  `json:"type"`
		Field      string  `json:"field"`
		Multiplier *string `json:"multiplier,omitempty"`
		BucketSize *string `json:"bucket_size,omitempty"`
	}

	if err := json.Unmarshal([]byte(agg), &aggData); err == nil {
		// Successfully parsed as JSON
		aggregationType, err := p.parseAggregationType(aggData.Type)
		if err != nil {
			return meter.Aggregation{}, err
		}

		aggregation := meter.Aggregation{
			Type:  aggregationType,
			Field: aggData.Field,
		}

		// Parse multiplier if provided
		if aggData.Multiplier != nil {
			multiplier, err := decimal.NewFromString(*aggData.Multiplier)
			if err != nil {
				return meter.Aggregation{}, fmt.Errorf("invalid multiplier value: %w", err)
			}
			aggregation.Multiplier = &multiplier
		}

		// Parse bucket size if provided
		if aggData.BucketSize != nil {
			aggregation.BucketSize = types.WindowSize(*aggData.BucketSize)
		}

		return aggregation, nil
	}

	// Fallback to simple string parsing for backward compatibility
	aggregationType, err := p.parseAggregationType(agg)
	if err != nil {
		return meter.Aggregation{}, err
	}

	return meter.Aggregation{
		Type:  aggregationType,
		Field: "value", // Default field for simple aggregation
	}, nil
}

// parseAggregationType parses the aggregation string to AggregationType
func (p *CSVFeatureProcessor) parseAggregationType(agg string) (types.AggregationType, error) {
	switch strings.ToUpper(agg) {
	case "COUNT":
		return types.AggregationCount, nil
	case "SUM":
		return types.AggregationSum, nil
	case "AVG":
		return types.AggregationAvg, nil
	case "COUNT_UNIQUE":
		return types.AggregationCountUnique, nil
	case "LATEST":
		return types.AggregationLatest, nil
	case "SUM_WITH_MULTIPLIER":
		return types.AggregationSumWithMultiplier, nil
	case "MAX":
		return types.AggregationMax, nil
	case "WEIGHTED_SUM":
		return types.AggregationWeightedSum, nil
	default:
		return "", fmt.Errorf("unsupported aggregation type: %s", agg)
	}
}

// processRecord processes a single CSV record
func (p *CSVFeatureProcessor) processRecord(ctx context.Context, record CSVFeatureRecord, planID, addonID string) error {
	// Create meter first
	meter, err := p.createMeter(ctx, record)
	if err != nil {
		return fmt.Errorf("failed to create meter: %w", err)
	}
	p.summary.MetersCreated++

	// Create feature with the meter
	feature, err := p.createFeature(ctx, record, meter.ID)
	if err != nil {
		return fmt.Errorf("failed to create feature: %w", err)
	}
	p.summary.FeaturesCreated++

	// Determine entity ID based on plan_name
	var entityID string
	if record.PlanName == "serverless" {
		entityID = planID
	} else if record.PlanName == "dedicated" || record.PlanName == "private" {
		entityID = addonID
	} else {
		return fmt.Errorf("unsupported plan name: %s", record.PlanName)
	}

	// Create price for the feature
	if err := p.createPrice(ctx, record, meter.ID, entityID); err != nil {
		return fmt.Errorf("failed to create price: %w", err)
	}
	p.summary.PricesCreated++

	p.log.Infow("Successfully processed record",
		"feature_name", record.FeatureName,
		"plan_name", record.PlanName,
		"meter_id", meter.ID,
		"feature_id", feature.ID,
		"entity_id", entityID)

	return nil
}

// printSummary prints a summary of the processing
func (p *CSVFeatureProcessor) printSummary() {
	p.log.Infow("CSV Feature Processing Summary",
		"total_rows", p.summary.TotalRows,
		"meters_created", p.summary.MetersCreated,
		"features_created", p.summary.FeaturesCreated,
		"prices_created", p.summary.PricesCreated,
		"errors", len(p.summary.Errors),
	)

	if len(p.summary.Errors) > 0 {
		p.log.Infow("Errors encountered during processing", "errors", p.summary.Errors)
	}
}

// ProcessCSVFeatures is the main function to process CSV data and create features and prices
func ProcessCSVFeatures() error {
	var filePath, tenantID, environmentID, userID, planID, addonID string
	filePath = os.Getenv("FILE_PATH")
	tenantID = os.Getenv("TENANT_ID")
	environmentID = os.Getenv("ENVIRONMENT_ID")
	userID = os.Getenv("USER_ID")
	planID = os.Getenv("PLAN_ID")
	addonID = os.Getenv("ADDON_ID")

	if filePath == "" {
		return fmt.Errorf("file path is required")
	}

	if tenantID == "" {
		return fmt.Errorf("tenant ID is required")
	}

	if environmentID == "" {
		return fmt.Errorf("environment ID is required")
	}

	if planID == "" {
		return fmt.Errorf("plan ID is required")
	}

	if addonID == "" {
		return fmt.Errorf("addon ID is required")
	}

	if userID == "" {
		userID = "user_id"
	}

	processor, err := newCSVFeatureProcessor(tenantID, environmentID, userID)
	if err != nil {
		return fmt.Errorf("failed to initialize CSV feature processor: %w", err)
	}

	// Create a context with tenant ID and environment ID
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, userID)

	// Parse the CSV file
	records, err := processor.parseCSV(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse CSV file: %w", err)
	}

	processor.log.Infow("Starting CSV feature processing",
		"file", filePath,
		"row_count", len(records),
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"plan_id", planID,
		"addon_id", addonID)

	// Process each record
	successCount := 0
	for i, record := range records {
		processor.log.Infow("Processing record", "index", i+1, "feature_name", record.FeatureName, "plan_name", record.PlanName)

		err := processor.processRecord(ctx, record, planID, addonID)
		if err != nil {
			processor.log.Errorw("Failed to process record", "index", i+1, "feature_name", record.FeatureName, "error", err)
			processor.summary.Errors = append(processor.summary.Errors, fmt.Sprintf("Record %d (%s): %v", i+1, record.FeatureName, err))
			continue
		}

		successCount++
		processor.log.Infow("Successfully processed record", "index", i+1, "feature_name", record.FeatureName)
	}

	// Print summary
	processor.printSummary()

	processor.log.Infow("CSV feature processing completed",
		"successful_records", successCount,
		"total_records", len(records),
		"success_rate", fmt.Sprintf("%.2f%%", float64(successCount)/float64(len(records))*100))

	return nil
}
