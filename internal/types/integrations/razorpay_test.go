package integrations

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A terminal outcome must never be reported as pending: gateway sync makes no
// transition on an empty status, so a refunded payment read as pending sits untouched
// until the sweeper expires its session.
func TestRazorpayPaymentStatusMapping(t *testing.T) {
	cases := []struct {
		raw     RazorpayPaymentStatus
		want    types.PaymentStatus
		wantErr bool
	}{
		{RazorpayPaymentStatusCaptured, types.PaymentStatusSucceeded, false},
		{RazorpayPaymentStatusRefunded, types.PaymentStatusRefunded, false},
		{RazorpayPaymentStatusFailed, types.PaymentStatusFailed, false},
		// In flight, not an outcome — empty status, no error.
		{RazorpayPaymentStatusCreated, "", false},
		{RazorpayPaymentStatusAuthorized, "", false},
		// A status Razorpay adds later must surface rather than read as pending.
		{RazorpayPaymentStatus("some_future_status"), "", true},
	}

	for _, c := range cases {
		t.Run(string(c.raw), func(t *testing.T) {
			got, err := c.raw.ToFlexpricePaymentStatus()
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}
