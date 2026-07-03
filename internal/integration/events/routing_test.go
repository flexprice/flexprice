package events

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
)

func TestResolveInvoiceSyncTarget(t *testing.T) {
	tests := []struct {
		name           string
		allowed        []string
		enabledInOrder []types.SecretProvider
		wantTarget     types.SecretProvider
		wantOK         bool
	}{
		{
			name:           "empty allow-list, one enabled ⇒ that one",
			allowed:        nil,
			enabledInOrder: []types.SecretProvider{types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "empty allow-list, several enabled ⇒ first by fixed order",
			allowed:        []string{},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe, types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderStripe,
			wantOK:         true,
		},
		{
			name:           "allow-list [razorpay], Stripe+Razorpay enabled ⇒ Razorpay",
			allowed:        []string{string(types.SecretProviderRazorpay)},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe, types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "allow-list [quickbooks, razorpay], only Razorpay enabled ⇒ Razorpay (fallback)",
			allowed:        []string{string(types.SecretProviderQuickBooks), string(types.SecretProviderRazorpay)},
			enabledInOrder: []types.SecretProvider{types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "allow-list respects list priority over fixed order",
			allowed:        []string{string(types.SecretProviderRazorpay), string(types.SecretProviderStripe)},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe, types.SecretProviderRazorpay},
			wantTarget:     types.SecretProviderRazorpay,
			wantOK:         true,
		},
		{
			name:           "allow-list [stripe], Stripe not enabled ⇒ (\"\", false)",
			allowed:        []string{string(types.SecretProviderStripe)},
			enabledInOrder: []types.SecretProvider{types.SecretProviderRazorpay},
			wantTarget:     "",
			wantOK:         false,
		},
		{
			name:           "allow-list with unknown provider string ⇒ ignored, falls through",
			allowed:        []string{"not-a-provider", string(types.SecretProviderStripe)},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe},
			wantTarget:     types.SecretProviderStripe,
			wantOK:         true,
		},
		{
			name:           "allow-list with only unknown provider ⇒ (\"\", false)",
			allowed:        []string{"not-a-provider"},
			enabledInOrder: []types.SecretProvider{types.SecretProviderStripe},
			wantTarget:     "",
			wantOK:         false,
		},
		{
			name:           "nothing enabled, empty allow-list ⇒ (\"\", false)",
			allowed:        nil,
			enabledInOrder: nil,
			wantTarget:     "",
			wantOK:         false,
		},
		{
			name:           "nothing enabled, non-empty allow-list ⇒ (\"\", false)",
			allowed:        []string{string(types.SecretProviderStripe)},
			enabledInOrder: nil,
			wantTarget:     "",
			wantOK:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTarget, gotOK := ResolveInvoiceSyncTarget(tt.allowed, tt.enabledInOrder)
			if gotTarget != tt.wantTarget || gotOK != tt.wantOK {
				t.Errorf("ResolveInvoiceSyncTarget(%v, %v) = (%q, %v), want (%q, %v)",
					tt.allowed, tt.enabledInOrder, gotTarget, gotOK, tt.wantTarget, tt.wantOK)
			}
		})
	}
}
