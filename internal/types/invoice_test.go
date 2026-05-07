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
