package webhookDto

type InternalEntitlementEvent struct {
	EntitlementID string `json:"entitlement_id"`
	TenantID      string `json:"tenant_id"`
}
