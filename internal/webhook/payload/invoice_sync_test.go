package payload

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestBuildProviderDetails(t *testing.T) {
	stripeInvoice := &dto.InvoiceResponse{Invoice: invoice.Invoice{
		ID:       "inv_1",
		Metadata: types.Metadata{types.InvoiceMetadataKeyStripeHostedInvoiceURL: "https://invoice.stripe.com/i/1"},
	}}
	mapping := &dto.EntityIntegrationMappingResponse{ProviderEntityID: "in_1"}

	tests := []struct {
		name          string
		provider      types.SecretProvider
		mapping       *dto.EntityIntegrationMappingResponse
		invoice       *dto.InvoiceResponse
		wantInvoiceID *string
		wantHostedURL *string
	}{
		{"stripe with mapping and hosted url", types.SecretProviderStripe, mapping, stripeInvoice, lo.ToPtr("in_1"), lo.ToPtr("https://invoice.stripe.com/i/1")},
		{"stripe failed before mapping", types.SecretProviderStripe, nil, &dto.InvoiceResponse{}, nil, nil},
		{"other provider ignores the stripe key", types.SecretProviderRazorpay, mapping, stripeInvoice, lo.ToPtr("in_1"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildProviderDetails(tt.provider, tt.mapping, tt.invoice)
			assert.Equal(t, tt.provider, got.Provider)
			assert.Equal(t, tt.wantInvoiceID, got.InvoiceID)
			assert.Equal(t, tt.wantHostedURL, got.HostedInvoiceURL)
		})
	}
}
