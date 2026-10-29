package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingTermEnums(t *testing.T) {
	tests := []struct {
		name    string
		valid   interface{ Validate() error }
		empty   interface{ Validate() error }
		invalid interface{ Validate() error }
	}{
		{"kind", BillingTermOnce, BillingTermKind(""), BillingTermKind("custom")},
		{"value", BillingTermFixed, BillingTermValueType(""), BillingTermValueType("percent")},
		{"timing", BillingTermArrear, BillingTermTiming(""), BillingTermTiming("later")},
		{"split", BillingTermEven, BillingTermSplit(""), BillingTermSplit("prices")},
		{"delivery", BillingTermSeparateInvoice, BillingTermDelivery(""), BillingTermDelivery("email")},
		{"item", BillingScheduleCharge, BillingScheduleItemKind(""), BillingScheduleItemKind("instalment")},
		{"trigger", BillingScheduleManual, BillingScheduleTrigger(""), BillingScheduleTrigger("event")},
		{"status", BillingScheduleInvoiced, BillingScheduleStatus(""), BillingScheduleStatus("paid")},
		{"reason", BillingScheduleInvoiceVoided, BillingScheduleCancellationReason(""), BillingScheduleCancellationReason("deleted")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, test.valid.Validate())
			require.Error(t, test.empty.Validate())
			require.Error(t, test.invalid.Validate())
		})
	}
}

func TestBillingOwnerTypesRemainExtensible(t *testing.T) {
	require.NoError(t, BillingTermOwnerCommitment.Validate())
	require.NoError(t, BillingTermOwnerType("standalone_fee").Validate())
	require.Error(t, BillingTermOwnerType("").Validate())
	require.Error(t, BillingTermOwnerType("  ").Validate())
}
