package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
)

func AssignTenantToUser() {
	// Parse command line flags
	userID := flag.String("user", "", "User ID to assign tenant to")
	tenantID := flag.String("tenant", "", "Tenant ID to assign")
	flag.Parse()

	// Validate flags
	if *userID == "" || *tenantID == "" {
		fmt.Println("Usage: go run scripts/local/assign_tenant.go -user=<user_id> -tenant=<tenant_id>")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create auth provider
	authProvider := auth.NewProvider(cfg)

	// Assign tenant to user
	err = authProvider.AssignUserToTenant(context.Background(), *userID, *tenantID)
	if err != nil {
		log.Fatalf("Failed to assign tenant to user: %v", err)
	}

	fmt.Printf("Successfully assigned tenant %s to user %s\n", *tenantID, *userID)
}
