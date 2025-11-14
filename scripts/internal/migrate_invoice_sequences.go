package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/environment"
	"github.com/flexprice/flexprice/ent/invoicesequence"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type DryRunResult struct {
	TenantID      string `json:"tenant_id"`
	Environment   string `json:"environment_id"`
	LastValue     int64  `json:"last_value"`
	YearMonth     string `json:"year_month"`
	WouldCreate   bool   `json:"would_create"`
	AlreadyExists bool   `json:"already_exists"`
}

func MigrateInvoiceSequences() error {
	isDryRun := os.Getenv("DRY_RUN") == "true"
	if isDryRun {
		log.Println("🔍 DRY RUN MODE - No changes will be made")
	}

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize logger
	logger, err := logger.NewLogger(cfg)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize postgres client
	entClients, err := postgres.NewEntClients(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %v", err)
	}
	entClient := entClients.Writer

	ctx := context.Background()

	// Get all unique tenant IDs from invoice_sequences
	tenants, err := entClient.InvoiceSequence.Query().
		Select(invoicesequence.FieldTenantID).
		GroupBy(invoicesequence.FieldTenantID).
		Strings(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenants: %w", err)
	}

	log.Printf("Found %d tenants to process", len(tenants))
	targetYearMonth := "202508"

	var dryRunResults []DryRunResult

	// For each tenant, process their environments
	for _, tenantID := range tenants {
		log.Printf("👉 Processing tenant: %s", tenantID)

		// Get all environments for this tenant
		environments, err := entClient.Environment.Query().
			Where(environment.TenantID(tenantID)).
			Select(environment.FieldID).
			Strings(ctx)
		if err != nil {
			log.Printf("❌ failed to get environments for tenant %s: %v", tenantID, err)
			continue
		}

		log.Printf("Found %d environments for tenant %s", len(environments), tenantID)

		// Get last value for this tenant in target month
		lastValue := int64(0)
		seq, err := entClient.InvoiceSequence.Query().
			Where(
				invoicesequence.TenantID(tenantID),
				invoicesequence.YearMonth(targetYearMonth),
			).
			Order(ent.Desc(invoicesequence.FieldLastValue)).
			First(ctx)
		if err != nil && !ent.IsNotFound(err) {
			log.Printf("❌ failed to get last value for tenant %s: %v", tenantID, err)
			continue
		}
		if seq != nil {
			lastValue = seq.LastValue
		}

		log.Printf("📊 Found last_value %d for tenant %s in %s", lastValue, tenantID, targetYearMonth)

		// For each environment, create invoice sequence
		for _, envID := range environments {
			// Check if sequence already exists
			exists, err := entClient.InvoiceSequence.Query().
				Where(
					invoicesequence.YearMonth(targetYearMonth),
					invoicesequence.TenantID(tenantID),
					invoicesequence.EnvironmentID(envID),
				).
				Exist(ctx)
			if err != nil {
				log.Printf("❌ failed to check sequence existence for tenant %s env %s: %v", tenantID, envID, err)
				continue
			}

			if isDryRun {
				result := DryRunResult{
					TenantID:      tenantID,
					Environment:   envID,
					LastValue:     lastValue,
					YearMonth:     targetYearMonth,
					WouldCreate:   !exists,
					AlreadyExists: exists,
				}
				dryRunResults = append(dryRunResults, result)

				if exists {
					log.Printf("⏩ sequence already exists for tenant %s env %s", tenantID, envID)
				} else {
					log.Printf("🔍 [DRY RUN] Would create sequence for tenant %s env %s with last value %d", tenantID, envID, lastValue)
				}
				continue
			}

			if exists {
				log.Printf("⏩ sequence already exists for tenant %s env %s", tenantID, envID)
				continue
			}

			// Create new sequence with tenant's last value
			if err := entClient.InvoiceSequence.Create().
				SetTenantID(tenantID).
				SetEnvironmentID(envID).
				SetYearMonth(targetYearMonth).
				SetLastValue(lastValue).
				Exec(ctx); err != nil {
				log.Printf("❌ failed to create sequence for tenant %s env %s: %v", tenantID, envID, err)
				continue
			}

			log.Printf("✅ created sequence for tenant %s env %s with last value %d", tenantID, envID, lastValue)
		}
	}

	// Write dry run results to file if in dry run mode
	if isDryRun && len(dryRunResults) > 0 {
		timestamp := time.Now().Format("20060102_150405")
		filename := filepath.Join("scripts", "internal", fmt.Sprintf("invoice_sequence_dry_run_%s.json", timestamp))

		file, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer file.Close()

		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(dryRunResults); err != nil {
			return fmt.Errorf("failed to write results to file: %w", err)
		}

		log.Printf("✅ Dry run results written to %s", filename)
	}

	return nil
}
