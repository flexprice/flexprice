package main

import (
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
)

// GenerateNewAPIKey generates a new API key and prints both the raw key and its configuration
func GenerateNewAPIKey() {
	// Generate a new API key
	rawKey := auth.GenerateAPIKey()
	hashedKey := auth.HashAPIKey(rawKey)

	// Create API key details (customize these values)
	details := config.APIKeyDetails{
		TenantID: "00000000-0000-0000-0000-000000000000",
		UserID:   "00000000-0000-0000-0000-000000000000",
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
		panic(err)
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
}
