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
}

func main() {
	// Define command line flags
	var (
		listCommands bool
		cmdName      string
		email        string
		tenant       string
		metersFile   string
		plansFile    string
		tenantID     string
		userID       string
		password     string
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
