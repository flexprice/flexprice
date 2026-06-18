package webhookrequest

import (
	"time"

	"github.com/flexprice/flexprice/ent"
)

// WebhookRequest is the domain model for an inbound webhook request audit log entry.
type WebhookRequest struct {
	ID            string              `json:"id"`
	TenantID      string              `json:"tenant_id"`
	EnvironmentID string              `json:"environment_id"`
	Provider      string              `json:"provider"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	RequestID     string              `json:"request_id"`
	Headers       map[string][]string `json:"headers"`
	Body          string              `json:"body"`
	CreatedAt     time.Time           `json:"created_at"`
}

// FromEnt converts an Ent WebhookRequest to the domain model.
func FromEnt(e *ent.WebhookRequest) *WebhookRequest {
	if e == nil {
		return nil
	}
	return &WebhookRequest{
		ID:            e.ID,
		TenantID:      e.TenantID,
		EnvironmentID: e.EnvironmentID,
		Provider:      e.Provider,
		Method:        e.Method,
		Path:          e.Path,
		RequestID:     e.RequestID,
		Headers:       e.Headers,
		Body:          e.Body,
		CreatedAt:     e.CreatedAt,
	}
}
