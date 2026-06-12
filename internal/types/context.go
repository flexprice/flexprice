package types

import (
	"context"
	"fmt"
	"sync/atomic"
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
	CtxWriterPin     ContextKey = "ctx_writer_pin"   // Mutable read-your-writes pin, installed per unit of work
	CtxRoles         ContextKey = "ctx_roles"        // RBAC roles array for permission checks

	// Default values
	DefaultTenantID = "00000000-0000-0000-0000-000000000000"
	DefaultUserID   = "00000000-0000-0000-0000-000000000000"

	// Dashboard context keys
	CtxCustomerID         ContextKey = "ctx_customer_id"
	CtxExternalCustomerID ContextKey = "ctx_external_customer_id"

	// Tenant internal state
	CtxTenantInternalStatus ContextKey = "ctx_tenant_internal_status"
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

// GetCustomerID returns the customer ID from the context
func GetCustomerID(ctx context.Context) string {
	if customerID, ok := ctx.Value(CtxCustomerID).(string); ok {
		return customerID
	}
	return ""
}

// GetExternalCustomerID returns the external customer ID from the context
func GetExternalCustomerID(ctx context.Context) string {
	if externalCustomerID, ok := ctx.Value(CtxExternalCustomerID).(string); ok {
		return externalCustomerID
	}
	return ""
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

// SetExternalCustomerID sets the external customer ID in context
func SetExternalCustomerID(ctx context.Context, externalCustomerID string) context.Context {
	return context.WithValue(ctx, CtxExternalCustomerID, externalCustomerID)
}

// SetCustomerID sets the customer ID in context
func SetCustomerID(ctx context.Context, customerID string) context.Context {
	return context.WithValue(ctx, CtxCustomerID, customerID)
}

// GetTenantInternalStatus returns the tenant's internal status from the context.
func GetTenantInternalStatus(ctx context.Context) TenantInternalStatus {
	if s, ok := ctx.Value(CtxTenantInternalStatus).(TenantInternalStatus); ok {
		return s
	}
	return ""
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

// writerPin is a mutable flag shared by every context derived from the one it
// was installed on. Unlike a plain context value, flipping it inside a nested
// call is visible to all later reads in the same unit of work, which is what
// makes automatic read-your-writes routing possible: the first write anywhere
// in a request pins every subsequent read to the writer endpoint, so replica
// lag can never make a just-written row invisible.
type writerPin struct {
	pinned atomic.Bool
}

// WithWriterPinning installs a writer pin on the context. Call this once at
// the root of each unit of work (HTTP request, Kafka message, Temporal
// activity, background job). If a pin is already installed, the context is
// returned unchanged so nested scopes share the outer pin.
func WithWriterPinning(ctx context.Context) context.Context {
	if _, ok := ctx.Value(CtxWriterPin).(*writerPin); ok {
		return ctx
	}
	return context.WithValue(ctx, CtxWriterPin, &writerPin{})
}

// PinWriter flips the writer pin for the current unit of work, routing all
// subsequent reads on this context (and its descendants) to the writer.
// No-op when no pin is installed.
func PinWriter(ctx context.Context) {
	if pin, ok := ctx.Value(CtxWriterPin).(*writerPin); ok {
		pin.pinned.Store(true)
	}
}

// IsWriterPinned reports whether a write has occurred in the current unit of
// work, meaning reads must go to the writer for read-after-write consistency.
func IsWriterPinned(ctx context.Context) bool {
	if pin, ok := ctx.Value(CtxWriterPin).(*writerPin); ok {
		return pin.pinned.Load()
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
