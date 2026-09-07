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
		Name:        "generate-dev-token",
		Description: "Generate a short-lived JWT for internal developer testing",
		Run:         internal.GenerateDevToken,
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
		Name:        "assign-plan",
		Description: "Assign a specific plan to customers who don't already have it",
		Run:         internal.AssignPlanToCustomers,
	},
	{
		Name:        "add-new-user",
		Description: "Add a new user to a tenant",
		Run:         internal.AddNewUserToTenant,
	},
	{
		Name:        "add-environment",
		Description: "Add an environment to an existing tenant",
		Run:         internal.AddEnvironmentToTenant,
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

	{
		Name:        "process-csv-features",
		Description: "Process CSV file to create features and prices for serverless plan",
		Run:         internal.ProcessCSVFeatures,
	},
	{
		Name:        "credit-usage-report",
		Description: "Generate credit usage report for customers in a tenant/environment",
		Run:         internal.GenerateCreditUsageReport,
	},
	{
		Name:        "import-features",
		Description: "Import features from a CSV file",
		Run:         internal.ImportFeatures,
	},
	{
		Name:        "migrate-cga",
		Description: "Migrate existing Credit Grant Applications to new structure (ensure environment_id is set)",
		Run:         internal.MigrateCGA,
	},
	{
		Name:        "sync-price-to-subscriptions",
		Description: "Sync a price to all subscriptions with the same plan and start date by creating new line items",
		Run:         internal.SyncPriceToSubscriptions,
	},
	{
		Name:        "setup-draft-invoices",
		Description: "Create draft invoices for subscriptions from a JSON file and trigger processing workflows",
		Run:         internal.SetupDraftInvoices,
	},
	{
		Name:        "setup-dummy-billing-customer",
		Description: "Create CUSTOMER_COUNT demo customers (default 1), each with subscription, $100 wallet top-up, and 500 meter events (Postgres + Kafka)",
		Run:         internal.SetupDummyBillingCustomer,
	},
	{
		Name:        "replay-events-csv",
		Description: "Replay events from a CSV via POST /v1/events (preserves event_id, timestamp, event_name, source, properties)",
		Run:         internal.ReplayEventsFromCSV,
	},
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
		apiKey             string
		externalCustomerID string
		eventName          string
		startTime          string
		endTime            string
		batchSize          string
		dryRun             string
		planID             string
		meterID            string
		startDate          string
		billingCycle       string
		customerCount      string
		environmentName    string
		environmentType    string
		addonID            string
		workerCount        string
		effectiveDate      string
		failedOutput       string
		successOutput      string
		apiBaseURL         string
		expiryHours        string
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
	flag.StringVar(&environmentName, "environment-name", "", "Environment name for add-environment command")
	flag.StringVar(&environmentType, "environment-type", "", "Environment type for add-environment (development or production)")
	flag.StringVar(&filePath, "file-path", "", "File path for operations")
	flag.StringVar(&planID, "plan-id", "", "Plan ID for operations")
	flag.StringVar(&meterID, "meter-id", "", "Meter ID (for setup-dummy-billing-customer)")
	flag.StringVar(&startDate, "start-date", "", "Subscription start date RFC3339 or YYYY-MM-DD")
	flag.StringVar(&billingCycle, "billing-cycle", "", "Billing cycle: anniversary or calendar")
	flag.StringVar(&customerCount, "customer-count", "", "Number of customers to create (setup-dummy-billing-customer); default 1")
	flag.StringVar(&apiKey, "api-key", "", "API key for operations")
	flag.StringVar(&externalCustomerID, "external-customer-id", "", "External customer ID for reprocessing events")
	flag.StringVar(&eventName, "event-name", "", "Event name filter for reprocessing")
	flag.StringVar(&startTime, "start-time", "", "Start time for reprocessing (ISO-8601 format)")
	flag.StringVar(&endTime, "end-time", "", "End time for reprocessing (ISO-8601 format)")
	flag.StringVar(&batchSize, "batch-size", "100", "Batch size for reprocessing")
	flag.StringVar(&dryRun, "dry-run", "false", "Dry run mode (true/false)")
	flag.StringVar(&addonID, "addon-id", "", "Addon ID for operations")
	flag.StringVar(&workerCount, "worker-count", "", "Concurrent workers (sets WORKER_COUNT when non-empty; migrate-calendar-billing-csv defaults to 3 if unset)")
	flag.StringVar(&effectiveDate, "effective-date", "", "Effective date for calendar billing migration (RFC3339 or YYYY-MM-DD)")
	flag.StringVar(&failedOutput, "failed-output", "", "Path for failed rows CSV (migrate-calendar-billing-csv)")
	flag.StringVar(&successOutput, "success-output", "", "Path for successful rows CSV (migrate-calendar-billing-csv)")
	flag.StringVar(&apiBaseURL, "api-base-url", "", "Flexprice API base URL including /v1 (migrate-calendar-billing-csv); default https://api.cloud.flexprice.io/v1")
	flag.StringVar(&expiryHours, "expiry-hours", "", "Token expiry in hours (generate-dev-token; default 1)")
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
		os.Setenv("USER_EMAIL", email) // #nosec G104 -- seed tooling, non-prod
	}
	if tenant != "" {
		os.Setenv("TENANT_NAME", tenant) // #nosec G104 -- seed tooling, non-prod
	}
	if metersFile != "" {
		os.Setenv("METERS_FILE", metersFile) // #nosec G104 -- seed tooling, non-prod
	}
	if plansFile != "" {
		os.Setenv("PLANS_FILE", plansFile) // #nosec G104 -- seed tooling, non-prod
	}
	if tenantID != "" {
		os.Setenv("TENANT_ID", tenantID) // #nosec G104 -- seed tooling, non-prod
	}
	if userID != "" {
		os.Setenv("USER_ID", userID) // #nosec G104 -- seed tooling, non-prod
	}
	if password != "" {
		os.Setenv("USER_PASSWORD", password) // #nosec G104 -- seed tooling, non-prod
	}
	if environmentID != "" {
		os.Setenv("ENVIRONMENT_ID", environmentID) // #nosec G104 -- seed tooling, non-prod
	}
	if environmentName != "" {
		os.Setenv("ENVIRONMENT_NAME", environmentName) // #nosec G104 -- seed tooling, non-prod
	}
	if environmentType != "" {
		os.Setenv("ENVIRONMENT_TYPE", environmentType) // #nosec G104 -- seed tooling, non-prod
	}
	if filePath != "" {
		os.Setenv("FILE_PATH", filePath) // #nosec G104 -- seed tooling, non-prod
	}
	if planID != "" {
		os.Setenv("PLAN_ID", planID) // #nosec G104 -- seed tooling, non-prod
	}
	if meterID != "" {
		os.Setenv("METER_ID", meterID) // #nosec G104 -- seed tooling, non-prod
	}
	if startDate != "" {
		os.Setenv("START_DATE", startDate) // #nosec G104 -- seed tooling, non-prod
	}
	if billingCycle != "" {
		os.Setenv("BILLING_CYCLE", billingCycle) // #nosec G104 -- seed tooling, non-prod
	}
	if customerCount != "" {
		os.Setenv("CUSTOMER_COUNT", customerCount) // #nosec G104 -- seed tooling, non-prod
	}
	if addonID != "" {
		os.Setenv("ADDON_ID", addonID) // #nosec G104 -- seed tooling, non-prod
	}
	if apiKey != "" {
		os.Setenv("SCRIPT_FLEXPRICE_API_KEY", apiKey) // #nosec G104 -- seed tooling, non-prod
		os.Setenv("FLEXPRICE_API_KEY", apiKey) // #nosec G104 -- seed tooling, non-prod
	}
	if externalCustomerID != "" {
		os.Setenv("EXTERNAL_CUSTOMER_ID", externalCustomerID) // #nosec G104 -- seed tooling, non-prod
	}
	if eventName != "" {
		os.Setenv("EVENT_NAME", eventName) // #nosec G104 -- seed tooling, non-prod
	}
	if startTime != "" {
		os.Setenv("START_TIME", startTime) // #nosec G104 -- seed tooling, non-prod
	}
	if endTime != "" {
		os.Setenv("END_TIME", endTime) // #nosec G104 -- seed tooling, non-prod
	}
	if batchSize != "" {
		os.Setenv("BATCH_SIZE", batchSize) // #nosec G104 -- seed tooling, non-prod
	}
	if dryRun != "" {
		os.Setenv("DRY_RUN", dryRun) // #nosec G104 -- seed tooling, non-prod
	}
	if workerCount != "" {
		os.Setenv("WORKER_COUNT", workerCount) // #nosec G104 -- seed tooling, non-prod
	}
	if apiBaseURL != "" {
		os.Setenv("API_BASE_URL", apiBaseURL) // #nosec G104 -- seed tooling, non-prod
	}
	if effectiveDate != "" {
		os.Setenv("EFFECTIVE_DATE", effectiveDate) // #nosec G104 -- seed tooling, non-prod
	}
	if failedOutput != "" {
		os.Setenv("FAILED_OUTPUT_PATH", failedOutput) // #nosec G104 -- seed tooling, non-prod
	}
	if successOutput != "" {
		os.Setenv("SUCCESS_OUTPUT_PATH", successOutput) // #nosec G104 -- seed tooling, non-prod
	}
	if expiryHours != "" {
		os.Setenv("EXPIRY_HOURS", expiryHours) // #nosec G104 -- seed tooling, non-prod
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
