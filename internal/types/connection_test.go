package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebhookSecretSet(t *testing.T) {
	tests := []struct {
		name        string
		provider    SecretProvider
		metadata    ConnectionMetadata
		wantSet     bool
		wantApplies bool
	}{
		{name: "stripe_with_secret", provider: SecretProviderStripe, metadata: ConnectionMetadata{Stripe: &StripeConnectionMetadata{WebhookSecret: "enc"}}, wantSet: true, wantApplies: true},
		{name: "stripe_without_secret", provider: SecretProviderStripe, metadata: ConnectionMetadata{Stripe: &StripeConnectionMetadata{}}, wantApplies: true},
		{name: "stripe_no_metadata", provider: SecretProviderStripe, wantApplies: true},
		{name: "chargebee_username_only", provider: SecretProviderChargebee, metadata: ConnectionMetadata{Chargebee: &ChargebeeConnectionMetadata{WebhookUsername: "u"}}, wantApplies: true},
		{name: "chargebee_basic_auth", provider: SecretProviderChargebee, metadata: ConnectionMetadata{Chargebee: &ChargebeeConnectionMetadata{WebhookUsername: "u", WebhookPassword: "p"}}, wantSet: true, wantApplies: true},
		{name: "quickbooks_verifier_token", provider: SecretProviderQuickBooks, metadata: ConnectionMetadata{QuickBooks: &QuickBooksConnectionMetadata{WebhookVerifierToken: "enc"}}, wantSet: true, wantApplies: true},
		{name: "hubspot_client_secret", provider: SecretProviderHubSpot, metadata: ConnectionMetadata{HubSpot: &HubSpotConnectionMetadata{ClientSecret: "enc"}}, wantSet: true, wantApplies: true},
		{name: "zoho_without_secret", provider: SecretProviderZohoBooks, metadata: ConnectionMetadata{ZohoBooks: &ZohoBooksConnectionMetadata{}}, wantApplies: true},
		{name: "s3_has_no_webhook", provider: SecretProviderS3},
		{name: "tabs_has_no_webhook", provider: SecretProviderTabs},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, applies := tt.metadata.WebhookSecretSet(tt.provider)
			require.Equal(t, tt.wantSet, set)
			require.Equal(t, tt.wantApplies, applies)
		})
	}
}
