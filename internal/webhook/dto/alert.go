package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

// InternalAlertEvent represents the internal event structure for alert webhooks
type InternalAlertEvent struct {
	EventType     string                 `json:"event_type"`
	TenantID      string                 `json:"tenant_id"`
	EnvironmentID string                 `json:"environment_id"`
	AlertID       string                 `json:"alert_id"`
	EntityType    string                 `json:"entity_type"`
	EntityID      string                 `json:"entity_id,omitempty"`
	AlertMetric   string                 `json:"alert_metric"`
	AlertState    string                 `json:"alert_state"`
	AlertEnabled  bool                   `json:"alert_enabled"`
	AlertData     map[string]interface{} `json:"alert_data,omitempty"`
}

type AlertWebhookPayload struct {
	Alert *dto.AlertResponse `json:"alert"`
}

func NewAlertWebhookPayload(alert *dto.AlertResponse) *AlertWebhookPayload {
	return &AlertWebhookPayload{Alert: alert}
}
