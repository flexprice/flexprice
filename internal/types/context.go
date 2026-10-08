package types

import (
	"context"
	"fmt"
	"sync"
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
	CtxPostCommit    ContextKey = "ctx_post_commit"  // Mutable post-commit hook list, installed per transaction
	CtxRoles         ContextKey = "ctx_roles"        // RBAC roles array for permission checks

	// Default values
	DefaultTenantID = "00000000-0000-0000-0000-000000000000"
	DefaultUserID   = "00000000-0000-0000-0000-000000000000"

	// Dashboard context keys
	CtxCustomerID         ContextKey = "ctx_customer_id"
	CtxExternalCustomerID ContextKey = "ctx_external_customer_id"

	// Tenant internal state
	CtxTenantInternalStatus ContextKey = "ctx_tenant_internal_status"

	// Caller type set by auth middleware; empty for JWT users and config API keys.
	CtxUserType ContextKey = "ctx_user_type"
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

// GetRoles returns the RBAC roles array from the context. A caller with no
// roles holds no grant and is refused every permission check, so an absent or
// unexpected value fails closed rather than widening access.
func GetRoles(ctx context.Context) []string {
	if roles, ok := ctx.Value(CtxRoles).([]string); ok {
		return roles
	}
	return []string{}
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

// IsServiceAccount reports whether the request was made by a service account API key.
// Returns false for JWT users and config API keys. RBAC applies to every caller
// type regardless, so this reports the caller's kind and never grants a bypass.
func IsServiceAccount(ctx context.Context) bool {
	t, _ := ctx.Value(CtxUserType).(string)
	return t == string(UserTypeServiceAccount)
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

// postCommitHooks collects work deferred until the current transaction
// commits. The transaction wrapper installs it; callers queue through
// RegisterPostCommit.
type postCommitHooks struct {
	mu     sync.Mutex
	funcs  []func()
	closed bool
}

// WithPostCommitHooks installs an empty hook list for one transaction.
func WithPostCommitHooks(ctx context.Context) context.Context {
	return context.WithValue(ctx, CtxPostCommit, &postCommitHooks{})
}

// RegisterPostCommit queues fn to run once the current transaction commits,
// reporting false when there is nothing to wait for — no transaction, or one
// that has already finished — so the caller runs fn itself. Work that reads
// what the transaction wrote must go through here: uncommitted rows are
// invisible on any other connection.
func RegisterPostCommit(ctx context.Context, fn func()) bool {
	hooks, ok := ctx.Value(CtxPostCommit).(*postCommitHooks)
	if !ok || hooks == nil {
		return false
	}
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if hooks.closed {
		return false
	}
	hooks.funcs = append(hooks.funcs, fn)
	return true
}

// RunPostCommitHooks closes registration and runs what was queued.
func RunPostCommitHooks(ctx context.Context) {
	for _, fn := range closePostCommitHooks(ctx) {
		fn()
	}
}

// DiscardPostCommitHooks closes registration and drops what was queued — the
// rollback path, where the writes the work would read never landed.
func DiscardPostCommitHooks(ctx context.Context) {
	_ = closePostCommitHooks(ctx)
}

// closePostCommitHooks takes the queued hooks and closes the list, so a
// registration arriving afterwards is told to run inline rather than queueing
// onto a list nobody will drain again.
func closePostCommitHooks(ctx context.Context) []func() {
	hooks, ok := ctx.Value(CtxPostCommit).(*postCommitHooks)
	if !ok || hooks == nil {
		return nil
	}
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	queued := hooks.funcs
	hooks.funcs = nil
	hooks.closed = true
	return queued
}

// WithoutDBTransaction strips any open transaction from ctx.
//
// Work that outlives the request — a detached goroutine, anything queued —
// must never inherit the caller's transaction. The request commits and hands
// that connection back to the pool while the detached work is still issuing
// statements on it, which desynchronizes the Postgres wire protocol
// ("unexpected Parse response") and poisons the connection for whoever picks
// it up next, so failures surface in unrelated requests.
func WithoutDBTransaction(ctx context.Context) context.Context {
	if ctx.Value(CtxDBTransaction) == nil {
		return ctx
	}
	return context.WithValue(ctx, CtxDBTransaction, nil)
}
