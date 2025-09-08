package client

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
)

// WaitForHealthy waits for the client to become healthy with a timeout
func WaitForHealthy(ctx context.Context, client TemporalClient, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for temporal client to become healthy: %w", ctx.Err())
		case <-ticker.C:
			if client.IsHealthy(ctx) {
				return nil
			}
		}
	}
}

// WithTenantContext adds tenant context to the context for temporal operations
func WithTenantContext(ctx context.Context, tenantID, userID string) context.Context {
	tc := models.NewTenantContext(tenantID, userID, "", "")
	return models.WithTenantContext(ctx, tc)
}

// GetTenantContext extracts tenant context from the context
func GetTenantContext(ctx context.Context) (*models.TenantContext, error) {
	return models.FromContext(ctx)
}

// ValidateTenantContext validates that the required tenant context fields are present
func ValidateTenantContext(ctx context.Context) error {
	tc, err := GetTenantContext(ctx)
	if err != nil {
		return err
	}

	if tc.TenantID == "" {
		return models.ErrInvalidTenantContext
	}

	return nil
}
