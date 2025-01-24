package workflow

type BillingPeriodUpdatePayload struct {
	EventID  string                 `json:"event_id"`
	TenantID string                 `json:"tenant_id"`
	Metadata map[string]interface{} `json:"metadata"`
}
