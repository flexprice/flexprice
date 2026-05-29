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

// uses USER_ID, TENANT_ID, ENVIRONMENT_ID, EXPIRY_HOURS environment variables
// if EXPIRY_HOURS is not set, it defaults to 1 hour

// GenerateDevToken generates a short-lived JWT for internal developer testing.
func GenerateDevToken() error {
	tenantID := os.Getenv("TENANT_ID")
	environmentID := os.Getenv("ENVIRONMENT_ID")
	userID := os.Getenv("USER_ID")
	expiryHoursStr := os.Getenv("EXPIRY_HOURS")

	if tenantID == "" {
		return fmt.Errorf("TENANT_ID is required (pass -tenant-id flag or set TENANT_ID env var)")
	}
	if userID == "" {
		userID = types.DefaultUserID
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

	authProvider := internalAuth.NewFlexpriceAuth(cfg)
	token, expiresAt, err := authProvider.GenerateDevToken(tenantID, environmentID, userID, expiryHours)
	if err != nil {
		return fmt.Errorf("failed to generate dev token: %w", err)
	}

	separator := strings.Repeat("─", 60)
	fmt.Println("\nDev Token Generated")
	fmt.Println(separator)
	fmt.Printf("Token:          %s\n", token)
	fmt.Printf("Tenant ID:      %s\n", tenantID)
	if environmentID != "" {
		fmt.Printf("Environment ID: %s\n", environmentID)
	} else {
		fmt.Printf("Environment ID: (not set — use X-Environment-ID header)\n")
	}
	fmt.Printf("User ID:        %s\n", userID)
	fmt.Printf("Expires At:     %s\n", expiresAt.UTC().Format(time.RFC3339))
	fmt.Println(separator)

	return nil
}
