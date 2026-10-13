package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceBillingReason_TrialStart_Validate(t *testing.T) {
	err := InvoiceBillingReasonSubscriptionTrialStart.Validate()
	require.NoError(t, err, "SUBSCRIPTION_TRIAL_START must be a valid billing reason")
}

func TestInvoiceBillingReason_TrialStart_NotFirstOpenInvoiceReason(t *testing.T) {
	// Trial start invoices must NOT trigger subscription activation when paid.
	assert.False(t,
		InvoiceBillingReasonSubscriptionTrialStart.IsFirstSubscriptionOpenInvoiceReason(),
		"SUBSCRIPTION_TRIAL_START must not activate subscription on payment",
	)
}

func TestInvoiceBillingReason_TrialStart_StringValue(t *testing.T) {
	assert.Equal(t, "SUBSCRIPTION_TRIAL_START", string(InvoiceBillingReasonSubscriptionTrialStart))
}

func TestWithCollapsedInvoiceDisplayName(t *testing.T) {
	tests := []struct {
		name string
		md   Metadata
		in   string
		want string
	}{
		{name: "sets on nil map", md: nil, in: "Upgrade: Team → Starter", want: "Upgrade: Team → Starter"},
		{name: "preserves other keys", md: Metadata{"foo": "bar"}, in: "Quantity change", want: "Quantity change"},
		{name: "trims space", md: nil, in: "  Plan change  ", want: "Plan change"},
		{name: "ignores empty", md: Metadata{"foo": "bar"}, in: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WithCollapsedInvoiceDisplayName(tt.md, tt.in)
			assert.Equal(t, tt.want, CollapsedInvoiceDisplayName(got))
			if tt.md != nil {
				assert.Equal(t, "bar", got["foo"])
			}
		})
	}
}
