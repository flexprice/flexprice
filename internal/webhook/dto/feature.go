package webhookDto

type InternalFeatureEvent struct {
	FeatureID string `json:"feature_id"`
	TenantID  string `json:"tenant_id"`
}
