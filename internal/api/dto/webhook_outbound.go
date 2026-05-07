package dto

// RetryOutboundWebhookRequest is the body for POST /v1/webhooks/retry.
type RetryOutboundWebhookRequest struct {
	SystemEventID string `json:"system_event_id" binding:"required" example:"sev_abc123"`
}

// RetryOutboundWebhookResponse is returned when outbound webhook delivery completes (HTTP 202).
type RetryOutboundWebhookResponse struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	SystemEventID string `json:"system_event_id"`
}
