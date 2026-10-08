package models

import "github.com/flexprice/flexprice/internal/types"

// InvoiceSyncWebhookInput carries a provider invoice sync's final outcome; an empty Error means success.
type InvoiceSyncWebhookInput struct {
	InvoiceID     string               `json:"invoice_id"`
	TenantID      string               `json:"tenant_id"`
	EnvironmentID string               `json:"environment_id"`
	Provider      types.SecretProvider `json:"provider"`
	Error         string               `json:"error,omitempty"`
}
