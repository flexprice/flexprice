package webhookDto

// InternalCheckoutSessionEvent is the internal payload stored in system_events.
// The builder re-fetches the session by ID to build the full outbound payload.
type InternalCheckoutSessionEvent struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
}
