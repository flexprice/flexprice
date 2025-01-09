package types

import "context"

// ContextKey is a type for the keys of values stored in the context
type ContextKey string

const (
	CtxRequestID     ContextKey = "ctx_request_id"
	CtxTenantID      ContextKey = "ctx_tenant_id"
	CtxUserID        ContextKey = "ctx_user_id"
	CtxJWT           ContextKey = "ctx_jwt"
	CtxEnvironmentID ContextKey = "ctx_environment_id"
	CtxDBTransaction ContextKey = "ctx_db_transaction"

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
