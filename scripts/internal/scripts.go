package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
)

// GenerateNewAPIKey generates a new API key
func GenerateNewAPIKey() error {
	// Generate a new API key
	rawKey := auth.GenerateAPIKey()
	hashedKey := auth.HashAPIKey(rawKey)

	userID := os.Getenv("USER_ID")
	tenantID := os.Getenv("TENANT_ID")

	// Create API key details (customize these values)
	details := config.APIKeyDetails{
		TenantID: tenantID,
		UserID:   userID,
		Name:     "Dev API Keys",
		IsActive: true,
	}

	// Create the configuration map
	keysMap := map[string]config.APIKeyDetails{
		hashedKey: details,
	}

	// Convert to JSON
	jsonBytes, err := json.Marshal(keysMap)
	if err != nil {
		return err
	}

	fmt.Printf("\nNew API Key Generated:\n")
	fmt.Printf("Raw Key (give this to your customer): %s\n", rawKey)
	fmt.Printf("\nConfiguration:\n")
	fmt.Printf("Add this to your config.yaml under auth.api_key.keys:\n")
	fmt.Printf("%s:\n", hashedKey)
	fmt.Printf("  tenant_id: %s\n", details.TenantID)
	fmt.Printf("  user_id: %s\n", details.UserID)
	fmt.Printf("  name: %s\n", details.Name)
	fmt.Printf("  is_active: %v\n", details.IsActive)
	fmt.Printf("\nOr set this environment variable:\n")
	fmt.Printf("FLEXPRICE_AUTH_API_KEY_KEYS='%s'\n", string(jsonBytes))

	return nil
}

// AssignTenantToUser assigns a tenant to a user
func AssignTenantToUser() error {
	userID := os.Getenv("USER_ID")
	tenantID := os.Getenv("TENANT_ID")
	// Load configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
		return err
	}

	// Create auth provider
	authProvider := auth.NewProvider(cfg)

	// Assign tenant to user
	err = authProvider.AssignUserToTenant(context.Background(), userID, tenantID)
	if err != nil {
		log.Fatalf("Failed to assign tenant to user: %v", err)
		return err
	}

	fmt.Printf("Successfully assigned tenant %s to user %s\n", tenantID, userID)
	return nil
}
