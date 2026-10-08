package webhookDto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInvoiceSyncWebhookPayload_WireShape(t *testing.T) {
	inv := &dto.InvoiceResponse{Invoice: invoice.Invoice{ID: "inv_1"}}
	details := &ProviderDetails{Provider: types.SecretProviderStripe}

	raw, err := json.Marshal(NewInvoiceSyncWebhookPayload(inv, details, "card declined", types.WebhookEventInvoiceSyncFailed))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "card declined", got["error"])
	assert.Equal(t, map[string]any{"provider": "stripe", "invoice_id": nil, "hosted_invoice_url": nil}, got["provider_details"])
	assert.NotContains(t, got, "integration_mapping")
}
