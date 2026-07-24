package internal

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	internalAuth "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
)

// GenerateDevToken generates a short-lived JWT for internal developer testing.
//
// Environment variables:
//
//	TENANT_ID       required
//	ENVIRONMENT_ID  optional (embedded as environment_id when provided)
//	USER_ID         optional, defaults to types.DefaultUserID
//	USER_EMAIL      required when auth.provider=supabase, defaults to "dev@flexprice.io"
//	EXPIRY_HOURS    optional, defaults to 1
//
// The claim schema is chosen automatically based on auth.provider in config:
//
//	flexprice → { user_id, tenant_id, environment_id, exp, iat }
//	supabase  → { sub, email, app_metadata.tenant_id, environment_id, exp, iat }
func GenerateDevToken() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	userID := os.Getenv("USER_ID")
	userEmail := os.Getenv("USER_EMAIL")
	expiryHoursStr := os.Getenv("EXPIRY_HOURS")

	if tenantID == "" {
		return fmt.Errorf("TENANT_ID is required")
	}
	if userID == "" {
		userID = types.DefaultUserID
	}
	if userEmail == "" {
		userEmail = "dev@flexprice.io"
	}

	expiryHours := 1
	if expiryHoursStr != "" {
		v, err := strconv.Atoi(expiryHoursStr)
		if err != nil || v <= 0 {
			return fmt.Errorf("invalid -expiry-hours value %q: must be a positive integer", expiryHoursStr)
		}
		expiryHours = v
	}

	cfg, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	provider := internalAuth.NewProvider(cfg)
	token, expiresAt, err := provider.GenerateDevToken(tenantID, environmentID, userID, userEmail, expiryHours)
	if err != nil {
		return fmt.Errorf("failed to generate dev token: %w", err)
	}

	separator := strings.Repeat("─", 60)
	fmt.Println("\nDev Token Generated")
	fmt.Println(separator)
	fmt.Printf("Provider:       %s\n", cfg.Auth.Provider)
	fmt.Printf("Token:          %s\n", token)
	fmt.Printf("Tenant ID:      %s\n", tenantID)
	if cfg.Auth.Provider == types.AuthProviderSupabase {
		fmt.Printf("User ID (sub):  %s\n", userID)
		fmt.Printf("Email:          %s\n", userEmail)
		if environmentID != "" {
			fmt.Printf("Environment ID: %s\n", environmentID)
		} else {
			fmt.Printf("Environment ID: (not set — use X-Environment-ID header)\n")
		}
	} else {
		fmt.Printf("User ID:        %s\n", userID)
		if environmentID != "" {
			fmt.Printf("Environment ID: %s\n", environmentID)
		} else {
			fmt.Printf("Environment ID: (not set — use X-Environment-ID header)\n")
		}
	}
	fmt.Printf("Expires At:     %s\n", expiresAt.UTC().Format(time.RFC3339))
	fmt.Println(separator)

	return nil
}
