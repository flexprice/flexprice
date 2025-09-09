package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/flexprice/flexprice/scripts/internal"
	"github.com/flexprice/flexprice/scripts/local"
)

// Command represents a script that can be run
type Command struct {
	Name        string
	Description string
	Run         func() error
}

var commands = []Command{
	{
		Name:        "seed-events",
		Description: "Seed events data into Clickhouse",
		Run:         internal.SeedEventsClickhouse,
	},
	{
		Name:        "seed-events-by-meters",
		Description: "Seed events data into Clickhouse by meters",
		Run:         internal.SeedEventsFromMeters,
	},
	{
		Name:        "generate-apikey",
		Description: "Generate a new API key",
		Run:         internal.GenerateNewAPIKey,
	},
	{
		Name:        "assign-tenant",
		Description: "Assign tenant to user",
		Run:         internal.AssignTenantToUser,
	},
	{
		Name:        "onboard-tenant",
		Description: "Onboard a new tenant",
		Run:         internal.OnboardNewTenant,
	},
	{
		Name:        "migrate-subscription-line-items",
		Description: "Migrate subscription line items",
		Run:         local.MigrateSubscriptionLineItems,
	},
	{
		Name:        "migrate-environments",
		Description: "Migrate entities to use environment_id",
		Run:         internal.MigrateEnvironments,
	},
	{
		Name:        "sync-billing-customers",
		Description: "Sync billing customers",
		Run:         internal.SyncBillingCustomers,
	},
	{
		Name:        "import-pricing",
		Description: "Import pricing",
		Run:         internal.ImportPricing,
	},
	{
		Name:        "sync-plan-prices",
		Description: "Synchronize plan prices to all active subscriptions",
		Run:         internal.SyncPlanPrices,
	},
	{
		Name:        "reprocess-events",
		Description: "Reprocess events",
		Run:         internal.ReprocessEventsFromEnv,
	},
	{
		Name:        "assign-plan",
		Description: "Assign a specific plan to customers who don't already have it",
		Run:         internal.AssignPlanToCustomers,
	},
	{
		Name:        "bulk-reprocess-events",
		Description: "Bulk reprocess events for all customers in a tenant",
		Run:         runBulkReprocessEventsCommand,
	},
	{
		Name:        "add-new-user",
		Description: "Add a new user to a tenant",
		Run:         internal.AddNewUserToTenant,
	},
	{
		Name:        "migrate-invoice-sequences",
		Description: "Migrate invoice sequences to include environment isolation",
		Run:         internal.MigrateInvoiceSequences,
	},
	{
		Name:        "migrate-to-addon",
		Description: "Migrate to addon",
		Run:         internal.CopyPlanChargesToAddons,
	},
}

// runBulkReprocessEventsCommand wraps the bulk reprocess events with command line parameters
func runBulkReprocessEventsCommand() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	eventName := os.Getenv("EVENT_NAME")
	batchSizeStr := os.Getenv("BATCH_SIZE")

	if tenantID == "" || environmentID == "" {
		return fmt.Errorf("TENANT_ID and ENVIRONMENT_ID are required")
	}

	batchSize := 100 // default
	if batchSizeStr != "" {
		if _, err := fmt.Sscanf(batchSizeStr, "%d", &batchSize); err != nil {
			return fmt.Errorf("invalid BATCH_SIZE, must be an integer: %w", err)
		}
	}

	params := internal.BulkReprocessEventsParams{
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		EventName:     eventName,
		BatchSize:     batchSize,
	}

	return internal.BulkReprocessEvents(params)
}

func main() {
	// Define command line flags
	var (
		listCommands       bool
		cmdName            string
		email              string
		tenant             string
		metersFile         string
		plansFile          string
		tenantID           string
		userID             string
		password           string
		environmentID      string
		filePath           string
		planID             string
		apiKey             string
		externalCustomerID string
		eventName          string
		startTime          string
		endTime            string
		batchSize          string
	)

	flag.BoolVar(&listCommands, "list", false, "List all available commands")
	flag.StringVar(&cmdName, "cmd", "", "Command to run")
	flag.StringVar(&email, "user-email", "", "Email for tenant operations")
	flag.StringVar(&tenant, "tenant-name", "", "Tenant name for operations")
	flag.StringVar(&metersFile, "meters-file", "", "Path to meters JSON file")
	flag.StringVar(&plansFile, "plans-file", "", "Path to plans JSON file")
	flag.StringVar(&tenantID, "tenant-id", "", "Tenant ID for operations")
	flag.StringVar(&userID, "user-id", "", "User ID for operations")
	flag.StringVar(&password, "user-password", "", "password for setting up new user")
	flag.StringVar(&environmentID, "environment-id", "", "Environment ID for operations")
	flag.StringVar(&filePath, "file-path", "", "File path for operations")
	flag.StringVar(&planID, "plan-id", "", "Plan ID for operations")
	flag.StringVar(&apiKey, "api-key", "", "API key for operations")
	flag.StringVar(&externalCustomerID, "external-customer-id", "", "External customer ID for reprocessing events")
	flag.StringVar(&eventName, "event-name", "", "Event name filter for reprocessing")
	flag.StringVar(&startTime, "start-time", "", "Start time for reprocessing (ISO-8601 format)")
	flag.StringVar(&endTime, "end-time", "", "End time for reprocessing (ISO-8601 format)")
	flag.StringVar(&batchSize, "batch-size", "100", "Batch size for reprocessing")
	flag.Parse()

	if listCommands {
		fmt.Println("Available commands:")
		for _, cmd := range commands {
			fmt.Printf("  %-20s %s\n", cmd.Name, cmd.Description)
		}
		return
	}

	if cmdName == "" {
		log.Fatal("Please specify a command to run using -cmd flag. Use -list to see available commands.")
	}

	// Set command-specific environment variables
	if email != "" {
		os.Setenv("USER_EMAIL", email)
	}
	if tenant != "" {
		os.Setenv("TENANT_NAME", tenant)
	}
	if metersFile != "" {
		os.Setenv("METERS_FILE", metersFile)
	}
	if plansFile != "" {
		os.Setenv("PLANS_FILE", plansFile)
	}
	if tenantID != "" {
		os.Setenv("TENANT_ID", tenantID)
	}
	if userID != "" {
		os.Setenv("USER_ID", userID)
	}
	if password != "" {
		os.Setenv("USER_PASSWORD", password)
	}
	if environmentID != "" {
		os.Setenv("ENVIRONMENT_ID", environmentID)
	}
	if filePath != "" {
		os.Setenv("FILE_PATH", filePath)
	}
	if planID != "" {
		os.Setenv("PLAN_ID", planID)
	}
	if apiKey != "" {
		os.Setenv("SCRIPT_FLEXPRICE_API_KEY", apiKey)
	}
	if externalCustomerID != "" {
		os.Setenv("EXTERNAL_CUSTOMER_ID", externalCustomerID)
	}
	if eventName != "" {
		os.Setenv("EVENT_NAME", eventName)
	}
	if startTime != "" {
		os.Setenv("START_TIME", startTime)
	}
	if endTime != "" {
		os.Setenv("END_TIME", endTime)
	}
	if batchSize != "" {
		os.Setenv("BATCH_SIZE", batchSize)
	}

	// Find and run the command
	for _, cmd := range commands {
		if cmd.Name == cmdName {
			if err := cmd.Run(); err != nil {
				log.Fatalf("Error running command %s: %v", cmdName, err)
			}
			return
		}
	}

	log.Fatalf("Unknown command: %s. Use -list to see available commands.", cmdName)
}
