package types

import (
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingTermEnums_Validate(t *testing.T) {
	type validator interface{ Validate() error }

	tests := []struct {
		name    string
		valid   []validator
		invalid validator
	}{
		{
			name:    "owner type",
			valid:   []validator{BillingTermOwnerCommitment, BillingTermOwnerType("")},
			invalid: BillingTermOwnerType("x"),
		},
		{
			name:    "kind",
			valid:   []validator{BillingTermKindOnce, BillingTermKindRecurring, BillingTermKind("")},
			invalid: BillingTermKind("x"),
		},
		{
			name: "value type",
			valid: []validator{
				BillingTermValueFixed,
				BillingTermValuePercentage,
				BillingTermValueRemainder,
				BillingTermValueType(""),
			},
			invalid: BillingTermValueType("x"),
		},
		{
			name:    "split",
			valid:   []validator{BillingTermSplitEven, BillingTermSplitProportional, BillingTermSplit("")},
			invalid: BillingTermSplit("x"),
		},
		{
			name:    "instalment bill trigger",
			valid:   []validator{InstalmentBillTriggerDate, InstalmentBillTriggerManual, InstalmentBillTrigger("")},
			invalid: InstalmentBillTrigger("x"),
		},
		{
			name:    "instalment kind",
			valid:   []validator{InstalmentKindInstalment, InstalmentKindSettlement, InstalmentKind("")},
			invalid: InstalmentKind("x"),
		},
		{
			name: "instalment status",
			valid: []validator{
				InstalmentStatusScheduled,
				InstalmentStatusInvoiced,
				InstalmentStatusCancelled,
				InstalmentStatus(""),
			},
			invalid: InstalmentStatus("x"),
		},
		{
			name: "instalment delivery",
			valid: []validator{
				InstalmentDeliverySubscriptionInvoice,
				InstalmentDeliverySeparateInvoice,
				InstalmentDelivery(""),
			},
			invalid: InstalmentDelivery("x"),
		},
		{
			name:    "cancellation fee type",
			valid:   []validator{CancellationFeeFixed, CancellationFeePercentage, CancellationFeeType("")},
			invalid: CancellationFeeType("x"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, v := range tc.valid {
				assert.NoError(t, v.Validate())
			}

			err := tc.invalid.Validate()
			require.Error(t, err)
			assert.True(t, ierr.IsValidation(err))
		})
	}

	assert.NoError(t, InvoiceBillingReasonBillingTerms.Validate())
}

func TestBillingTermConstants(t *testing.T) {
	assert.Equal(t, "bterm", UUID_PREFIX_BILLING_TERM)
	assert.Equal(t, "binst", UUID_PREFIX_BILLING_INSTALMENT)
	assert.Equal(t, "BILLING_TERMS", string(InvoiceBillingReasonBillingTerms))
	assert.Equal(t, "billing_term", string(SystemEntityTypeBillingTerm))
	assert.Equal(t, "billing_instalment", string(SystemEntityTypeBillingInstalment))
}
