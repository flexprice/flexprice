package models

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/types"
)

type contextKey string

const contextKeyTenantContext = contextKey("tenant-context")

// TenantContext represents the tenant context for Temporal operations
type TenantContext struct {
	TenantID      string
	UserID        string
	RequestID     string
	CorrelationID string
}

// NewTenantContext creates a new TenantContext
func NewTenantContext(tenantID, userID, requestID, correlationID string) *TenantContext {
	return &TenantContext{
		TenantID:      tenantID,
		UserID:        userID,
		RequestID:     requestID,
		CorrelationID: correlationID,
	}
}

// ToHeaders converts TenantContext to Temporal headers
func (tc *TenantContext) ToHeaders() map[string]string {
	return map[string]string{
		HeaderTenantID:      tc.TenantID,
		HeaderUserID:        tc.UserID,
		HeaderRequestID:     tc.RequestID,
		HeaderCorrelationID: tc.CorrelationID,
	}
}

// FromContext extracts TenantContext from a context.Context using existing utilities
func FromContext(ctx context.Context) (*TenantContext, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}

	// Try to get from custom context key first
	if tc, ok := ctx.Value(contextKeyTenantContext).(*TenantContext); ok {
		return tc, nil
	}

	// Fallback to existing context utilities
	tenantID := types.GetTenantID(ctx)
	userID := types.GetUserID(ctx)
	requestID := types.GetRequestID(ctx)
	correlationID := getStringFromContext(ctx, "correlation_id")

	if tenantID == "" {
		return nil, fmt.Errorf("no tenant context found in context")
	}

	return &TenantContext{
		TenantID:      tenantID,
		UserID:        userID,
		RequestID:     requestID,
		CorrelationID: correlationID,
	}, nil
}

// WithTenantContext adds TenantContext to a context.Context
func WithTenantContext(ctx context.Context, tc *TenantContext) context.Context {
	if tc == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKeyTenantContext, tc)
}

// getStringFromContext extracts a string value from context
func getStringFromContext(ctx context.Context, key string) string {
	if value := ctx.Value(key); value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}
