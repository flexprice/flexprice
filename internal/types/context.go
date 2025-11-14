package types

import (
	"context"
	"fmt"
)

// ContextKey is a type for the keys of values stored in the context
type ContextKey string

const (
	CtxRequestID     ContextKey = "ctx_request_id"
	CtxTenantID      ContextKey = "ctx_tenant_id"
	CtxUserID        ContextKey = "ctx_user_id"
	CtxJWT           ContextKey = "ctx_jwt"
	CtxEnvironmentID ContextKey = "ctx_environment_id"
	CtxDBTransaction ContextKey = "ctx_db_transaction"
	CtxForceWriter   ContextKey = "ctx_force_writer" // Force DB operations to use writer connection
	CtxRoles         ContextKey = "ctx_roles"        // RBAC roles array for permission checks

	// Default values
	DefaultTenantID = "00000000-0000-0000-0000-000000000000"
	DefaultUserID   = "00000000-0000-0000-0000-000000000000"
)

func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(CtxUserID).(string); ok {
		return userID
	}
	return ""
}

func GetTenantID(ctx context.Context) string {
	if tenantID, ok := ctx.Value(CtxTenantID).(string); ok {
		return tenantID
	}
	return ""
}

func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(CtxRequestID).(string); ok {
		return requestID
	}
	return ""
}

func GetJWT(ctx context.Context) string {
	if jwt, ok := ctx.Value(CtxJWT).(string); ok {
		return jwt
	}
	return ""
}

func GetEnvironmentID(ctx context.Context) string {
	if environmentID, ok := ctx.Value(CtxEnvironmentID).(string); ok {
		return environmentID
	}
	return ""
}

// GetRoles returns the RBAC roles array from the context
func GetRoles(ctx context.Context) []string {
	if roles, ok := ctx.Value(CtxRoles).([]string); ok {
		return roles
	}
	return []string{} // Empty roles = full access
}

// SetTenantID sets the tenant ID in the context
func SetTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, CtxTenantID, tenantID)
}

// SetEnvironmentID sets the environment ID in the context
func SetEnvironmentID(ctx context.Context, environmentID string) context.Context {
	return context.WithValue(ctx, CtxEnvironmentID, environmentID)
}

// SetUserID sets the user ID in the context
func SetUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, CtxUserID, userID)
}

// WithForceWriter returns a context that forces database operations to use the writer connection.
// This is useful when you need to ensure read-after-write consistency or when you know
// the operation might need to write even if it starts as a read.
func WithForceWriter(ctx context.Context) context.Context {
	return context.WithValue(ctx, CtxForceWriter, true)
}

// ShouldForceWriter returns true if the context is marked to force writer connection
func ShouldForceWriter(ctx context.Context) bool {
	if forceWriter, ok := ctx.Value(CtxForceWriter).(bool); ok {
		return forceWriter
	}
	return false
}

// ValidateTenantContext validates that the required tenant context fields are present
func ValidateTenantContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	tenantID := GetTenantID(ctx)
	if tenantID == "" {
		return fmt.Errorf("no tenant context found in context")
	}

	return nil
}
