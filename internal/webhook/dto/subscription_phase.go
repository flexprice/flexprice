package webhookDto

type InternalSubscriptionPhaseEvent struct {
	PhaseID  string `json:"phase_id"`
	TenantID string `json:"tenant_id"`
}
