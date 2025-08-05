package internal

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	entRepo "github.com/flexprice/flexprice/internal/repository/ent"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// PricingRow represents a row in the pricing CSV file
type PricingRow struct {
	FeatureName  string  `json:"feature_name" csv:"feature_name"`
	EventName    string  `json:"event_name" csv:"event_name"`
	Aggregation  string  `json:"aggregation" csv:"aggregation"`
	Filters      string  `json:"filters" csv:"filters"`
	PerUnitPrice float64 `json:"per_unit_price" csv:"per_unit_price"`
	PlanName     string  `json:"plan_name" csv:"plan_name"`
	FeatureID    string  `json:"feature_id" csv:"feature_id"`
	MeterID      string  `json:"meter_id" csv:"meter_id"`
	PriceID      string  `json:"price_id" csv:"price_id"`
	PlanID       string  `json:"plan_id" csv:"plan_id"`
	Delete       string  `json:"delete" csv:"delete"`
}

// PricingImportSummary contains statistics about the import process
type PricingImportSummary struct {
	TotalRows       int
	MetersUpdated   int
	MetersDeleted   int
	FeaturesUpdated int
	FeaturesDeleted int
	PricesCreated   int
	PricesUpdated   int
	PricesDeleted   int
	Errors          []string
}

type pricingImportScript struct {
	cfg           *config.Configuration
	log           *logger.Logger
	featureRepo   feature.Repository
	meterRepo     meter.Repository
	priceRepo     price.Repository
	planRepo      plan.Repository
	entClient     *ent.Client
	pgClient      postgres.IClient
	summary       PricingImportSummary
	tenantID      string
	environmentID string
}

func newPricingImportScript(tenantID, environmentID string) (*pricingImportScript, error) {
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
	entClient, err := postgres.NewEntClient(cfg, log)
	if err != nil {
		log.Fatalf("Failed to connect to postgres: %v", err)
		return nil, err
	}

	// Create postgres client
	pgClient := postgres.NewClient(entClient, log, sentry.NewSentryService(cfg, log))
	cacheClient := cache.NewInMemoryCache()

	// Initialize repositories
	featureRepo := entRepo.NewFeatureRepository(pgClient, log, cacheClient)
	meterRepo := entRepo.NewMeterRepository(pgClient, log, cacheClient)
	priceRepo := entRepo.NewPriceRepository(pgClient, log, cacheClient)
	planRepo := entRepo.NewPlanRepository(pgClient, log, cacheClient)

	return &pricingImportScript{
		cfg:           cfg,
		log:           log,
		featureRepo:   featureRepo,
		meterRepo:     meterRepo,
		priceRepo:     priceRepo,
		planRepo:      planRepo,
		entClient:     entClient,
		pgClient:      pgClient,
		summary:       PricingImportSummary{},
		tenantID:      tenantID,
		environmentID: environmentID,
	}, nil
}

// parsePricingCSV parses the pricing CSV file
func (s *pricingImportScript) parsePricingCSV(filePath string) ([]PricingRow, error) {
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

	var pricingRows []PricingRow

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.log.Errorw("Failed to read CSV record", "error", err)
			continue
		}

		// Parse per_unit_price
		var perUnitPrice float64
		if record[4] != "" {
			_, err = fmt.Sscanf(record[4], "%f", &perUnitPrice)
			if err != nil {
				s.log.Warnw("Failed to parse per_unit_price, setting to 0", "value", record[4], "error", err)
			}
		}

		pricingRow := PricingRow{
			FeatureName:  record[0],
			EventName:    record[1],
			Aggregation:  record[2],
			Filters:      record[3],
			PerUnitPrice: perUnitPrice,
			PlanName:     record[5],
			FeatureID:    record[6],
			MeterID:      record[7],
			PriceID:      record[8],
			PlanID:       record[9],
			Delete:       record[10],
		}

		pricingRows = append(pricingRows, pricingRow)
	}

	s.summary.TotalRows = len(pricingRows)
	s.log.Infow("Parsed pricing CSV", "total_rows", len(pricingRows))
	return pricingRows, nil
}

// updateMeterDefinition updates a meter definition to match the CSV data using direct SQL
func (s *pricingImportScript) updateMeterDefinition(ctx context.Context, row PricingRow) error {
	if row.MeterID == "" {
		s.log.Warnw("Skipping meter update - no meter ID", "feature_name", row.FeatureName)
		return nil
	}

	// Fetch the existing meter to check if update is needed
	meterObj, err := s.meterRepo.GetMeter(ctx, row.MeterID)
	if err != nil {
		return fmt.Errorf("failed to get meter by ID: %w", err)
	}

	// Parse aggregation JSON
	var aggregation meter.Aggregation
	if err := json.Unmarshal([]byte(row.Aggregation), &aggregation); err != nil {
		s.log.Errorw("Failed to parse aggregation JSON", "aggregation", row.Aggregation, "error", err)
		return err
	}

	// Check if meter should be deleted
	if strings.ToUpper(row.Delete) == "Y" {
		if meterObj.Status != types.StatusDeleted {
			// Use direct SQL to mark as deleted
			query := `
				UPDATE meters 
				SET status = $1, updated_at = $2 
				WHERE id = $3 AND tenant_id = $4 AND environment_id = $5
			`
			_, err = s.entClient.ExecContext(ctx, query,
				types.StatusDeleted,
				time.Now().UTC(),
				row.MeterID,
				s.tenantID,
				s.environmentID)

			if err != nil {
				s.log.Errorw("Failed to mark meter as deleted", "meter_id", row.MeterID, "error", err)
				return err
			}
			s.summary.MetersDeleted++
			s.log.Infow("Marked meter as deleted", "meter_id", row.MeterID, "name", meterObj.Name)
		}
		return nil
	}

	// Check if update is needed
	needsUpdate := false
	if meterObj.EventName != row.EventName {
		needsUpdate = true
		s.log.Infow("Meter name mismatch", "meter_id", row.MeterID, "db_name", meterObj.EventName, "csv_name", row.EventName)
	}

	// Compare aggregation (simple string comparison of JSON)
	dbAggregation, _ := json.Marshal(meterObj.Aggregation)
	csvAggregation, _ := json.Marshal(aggregation)
	if string(dbAggregation) != string(csvAggregation) {
		needsUpdate = true
		s.log.Infow("Meter aggregation mismatch",
			"meter_id", row.MeterID,
			"db_aggregation", string(dbAggregation),
			"csv_aggregation", string(csvAggregation))
	}

	// Update meter if needed using direct SQL
	if needsUpdate {
		// Convert aggregation to JSON string for database
		aggregationJSON, err := json.Marshal(aggregation)
		if err != nil {
			s.log.Errorw("Failed to marshal aggregation", "error", err)
			return err
		}

		// Use direct SQL to update all fields including "immutable" ones
		query := `
			UPDATE meters 
			SET event_name = $1, aggregation = $2, updated_at = $3 
			WHERE id = $4 AND tenant_id = $5 AND environment_id = $6
		`
		_, err = s.entClient.ExecContext(ctx, query,
			row.EventName,
			aggregationJSON,
			time.Now().UTC(),
			row.MeterID,
			s.tenantID,
			s.environmentID)

		if err != nil {
			s.log.Errorw("Failed to update meter via SQL", "meter_id", row.MeterID, "error", err)
			return err
		}
		s.summary.MetersUpdated++
		s.log.Infow("Updated meter via SQL", "meter_id", row.MeterID, "name", row.EventName)
	}

	return nil
}

// updateFeatureMapping updates a feature's meter mapping and status using direct SQL
func (s *pricingImportScript) updateFeatureMapping(ctx context.Context, row PricingRow) error {
	if row.FeatureID == "" {
		s.log.Warnw("Skipping feature update - no feature ID", "feature_name", row.FeatureName)
		return nil
	}

	// Fetch the existing feature to check if update is needed
	featureObj, err := s.featureRepo.Get(ctx, row.FeatureID)
	if err != nil {
		return fmt.Errorf("failed to get feature by ID: %w", err)
	}

	// Check if feature should be deleted
	if strings.ToUpper(row.Delete) == "Y" {
		if featureObj.Status != types.StatusDeleted {
			// Use direct SQL to mark as deleted
			query := `
				UPDATE features 
				SET status = $1, updated_at = $2 
				WHERE id = $3 AND tenant_id = $4 AND environment_id = $5
			`
			_, err = s.entClient.ExecContext(ctx, query,
				types.StatusDeleted,
				time.Now().UTC(),
				row.FeatureID,
				s.tenantID,
				s.environmentID)

			if err != nil {
				s.log.Errorw("Failed to mark feature as deleted", "feature_id", row.FeatureID, "error", err)
				return err
			}
			s.summary.FeaturesDeleted++
			s.log.Infow("Marked feature as deleted", "feature_id", row.FeatureID, "name", featureObj.Name)
		}
		return nil
	}

	// Check if update is needed
	needsUpdate := false
	if featureObj.Name != row.FeatureName {
		needsUpdate = true
		s.log.Infow("Feature name mismatch", "feature_id", row.FeatureID, "db_name", featureObj.Name, "csv_name", row.FeatureName)
	}

	if featureObj.MeterID != row.MeterID {
		needsUpdate = true
		s.log.Infow("Feature meter ID mismatch", "feature_id", row.FeatureID, "db_meter_id", featureObj.MeterID, "csv_meter_id", row.MeterID)
	}

	// Update feature if needed
	if needsUpdate {
		// Use direct SQL to update
		query := `
			UPDATE features 
			SET name = $1, meter_id = $2, updated_at = $3 
			WHERE id = $4 AND tenant_id = $5 AND environment_id = $6
		`
		_, err = s.entClient.ExecContext(ctx, query,
			row.FeatureName,
			row.MeterID,
			time.Now().UTC(),
			row.FeatureID,
			s.tenantID,
			s.environmentID)

		if err != nil {
			s.log.Errorw("Failed to update feature via SQL", "feature_id", row.FeatureID, "error", err)
			return err
		}
		s.summary.FeaturesUpdated++
		s.log.Infow("Updated feature via SQL", "feature_id", row.FeatureID, "name", row.FeatureName, "meter_id", row.MeterID)
	}

	return nil
}

// updatePrice updates or creates a price based on the CSV data
func (s *pricingImportScript) updatePrice(ctx context.Context, row PricingRow) error {
	// Skip if feature is marked for deletion
	if strings.ToUpper(row.Delete) == "Y" {
		// If price ID exists, mark it as deleted
		if row.PriceID != "" {
			// Use direct SQL to mark price as deleted
			query := `
				UPDATE prices 
				SET status = $1, updated_at = $2 
				WHERE id = $3 AND tenant_id = $4 AND environment_id = $5
			`
			_, err := s.entClient.ExecContext(ctx, query,
				types.StatusDeleted,
				time.Now().UTC(),
				row.PriceID,
				s.tenantID,
				s.environmentID)

			if err != nil {
				s.log.Errorw("Failed to mark price as deleted", "price_id", row.PriceID, "error", err)
				return err
			}
			s.summary.PricesDeleted++
			s.log.Infow("Marked price as deleted", "price_id", row.PriceID)
		}
		return nil
	}

	// Check if plan exists
	if row.PlanID == "" {
		s.log.Warnw("Skipping price update - no plan ID", "feature_name", row.FeatureName)
		return nil
	}

	// Try to update existing price if price ID is provided
	if row.PriceID != "" {
		priceObj, err := s.priceRepo.Get(ctx, row.PriceID)
		if err == nil && priceObj != nil {
			// Check if update is needed
			decimalAmount := decimal.NewFromFloat(row.PerUnitPrice)
			if !priceObj.Amount.Equal(decimalAmount) {
				// Use direct SQL to update price
				query := `
					UPDATE prices 
					SET amount = $1, display_amount = $2, updated_at = $3 
					WHERE id = $4 AND tenant_id = $5 AND environment_id = $6
				`
				displayAmount := decimalAmount.String()
				_, err = s.entClient.ExecContext(ctx, query,
					decimalAmount,
					displayAmount,
					time.Now().UTC(),
					row.PriceID,
					s.tenantID,
					s.environmentID)

				if err != nil {
					s.log.Errorw("Failed to update price via SQL", "price_id", row.PriceID, "error", err)
					return err
				}
				s.summary.PricesUpdated++
				s.log.Infow("Updated price via SQL", "price_id", row.PriceID, "amount", row.PerUnitPrice)
			}
			return nil
		}
	}

	// Price doesn't exist, create a new one
	now := time.Now().UTC()
	priceObj := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		EntityType:         types.PRICE_ENTITY_TYPE_PLAN,
		EntityID:           row.PlanID,
		MeterID:            row.MeterID,
		Amount:             decimal.NewFromFloat(row.PerUnitPrice),
		Currency:           "usd", // Default to USD
		Type:               types.PRICE_TYPE_USAGE,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel: types.BaseModel{
			TenantID:  s.tenantID,
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: types.DefaultUserID,
			UpdatedBy: types.DefaultUserID,
		},
	}

	// Set the display amount
	priceObj.DisplayAmount = priceObj.GetDisplayAmount()

	// Use SQL to insert new price
	query := `
		INSERT INTO prices (
			id, plan_id, meter_id, amount, display_amount, currency, type, 
			billing_model, billing_cadence, billing_period, billing_period_count, invoice_cadence,
			tenant_id, environment_id, status, created_at, updated_at, created_by, updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		)
	`
	_, err := s.entClient.ExecContext(ctx, query,
		priceObj.ID, priceObj.EntityID, priceObj.MeterID,
		priceObj.Amount, priceObj.DisplayAmount, priceObj.Currency, priceObj.Type,
		priceObj.BillingModel, priceObj.BillingCadence, priceObj.BillingPeriod, priceObj.BillingPeriodCount, priceObj.InvoiceCadence,
		s.tenantID, s.environmentID, priceObj.Status,
		priceObj.CreatedAt, priceObj.UpdatedAt, priceObj.CreatedBy, priceObj.UpdatedBy,
	)

	if err != nil {
		s.log.Errorw("Failed to create new price via SQL", "plan_id", row.PlanID, "meter_id", row.MeterID, "error", err)
		return err
	}

	s.summary.PricesCreated++
	s.log.Infow("Created new price via SQL", "price_id", priceObj.ID, "feature_id", row.FeatureID, "plan_id", row.PlanID, "amount", row.PerUnitPrice)

	return nil
}

// processRow processes a single row from the CSV
func (s *pricingImportScript) processRow(ctx context.Context, row PricingRow) error {
	// Update meter definition
	if row.MeterID != "" {
		err := s.updateMeterDefinition(ctx, row)
		if err != nil {
			s.log.Errorw("Failed to update meter definition", "meter_id", row.MeterID, "error", err)
			s.summary.Errors = append(s.summary.Errors, fmt.Sprintf("Error updating meter %s: %v", row.MeterID, err))
		}
	}

	// Update feature mapping
	if row.FeatureID != "" {
		err := s.updateFeatureMapping(ctx, row)
		if err != nil {
			s.log.Errorw("Failed to update feature mapping", "feature_id", row.FeatureID, "error", err)
			s.summary.Errors = append(s.summary.Errors, fmt.Sprintf("Error updating feature %s: %v", row.FeatureID, err))
		}
	}

	// Update price
	if row.FeatureID != "" && row.PlanID != "" {
		err := s.updatePrice(ctx, row)
		if err != nil {
			s.log.Errorw("Failed to update price", "feature_id", row.FeatureID, "plan_id", row.PlanID, "error", err)
			s.summary.Errors = append(s.summary.Errors, fmt.Sprintf("Error updating price for feature %s and plan %s: %v", row.FeatureID, row.PlanID, err))
		}
	}

	return nil
}

// printSummary prints a summary of the import process
func (s *pricingImportScript) printSummary() {
	s.log.Infow("Pricing import summary",
		"total_rows", s.summary.TotalRows,
		"meters_updated", s.summary.MetersUpdated,
		"meters_deleted", s.summary.MetersDeleted,
		"features_updated", s.summary.FeaturesUpdated,
		"features_deleted", s.summary.FeaturesDeleted,
		"prices_created", s.summary.PricesCreated,
		"prices_updated", s.summary.PricesUpdated,
		"prices_deleted", s.summary.PricesDeleted,
		"errors", len(s.summary.Errors),
	)

	if len(s.summary.Errors) > 0 {
		s.log.Infow("Errors encountered during import", "errors", s.summary.Errors)
	}
}

// ImportPricing is the main function to import pricing data from a CSV file
func ImportPricing() error {
	var filePath, tenantID, environmentID string
	filePath = os.Getenv("FILE_PATH")
	tenantID = os.Getenv("TENANT_ID")
	environmentID = os.Getenv("ENVIRONMENT_ID")

	if filePath == "" {
		return fmt.Errorf("file path is required")
	}

	if tenantID == "" {
		return fmt.Errorf("tenant ID is required")
	}

	if environmentID == "" {
		return fmt.Errorf("environment ID is required")
	}

	script, err := newPricingImportScript(tenantID, environmentID)
	if err != nil {
		return fmt.Errorf("failed to initialize pricing import script: %w", err)
	}

	// Create a context with tenant ID and environment ID
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)

	// Parse the CSV file
	pricingRows, err := script.parsePricingCSV(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse pricing CSV: %w", err)
	}

	script.log.Infow("Starting pricing import",
		"file", filePath,
		"row_count", len(pricingRows),
		"tenant_id", tenantID,
		"environment_id", environmentID)

	// Process each row
	for i, row := range pricingRows {
		script.log.Infow("Processing row", "index", i, "feature_name", row.FeatureName, "feature_id", row.FeatureID)
		err := script.processRow(ctx, row)
		if err != nil {
			script.log.Errorw("Failed to process row", "index", i, "feature_name", row.FeatureName, "error", err)
			// Continue with the next row
		}
	}

	// Print summary
	script.printSummary()

	return nil
}
