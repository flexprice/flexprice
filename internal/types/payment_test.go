package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentMethodType_UPI_Validates(t *testing.T) {
	t.Parallel()
	require.NoError(t, PaymentMethodTypeUPI.Validate())
	require.Equal(t, "UPI", PaymentMethodTypeUPI.String())
}

func TestPaymentMethodStatus_Pending_Validates(t *testing.T) {
	t.Parallel()
	require.NoError(t, PaymentMethodStatusPending.Validate())
	require.Equal(t, "PENDING", PaymentMethodStatusPending.String())
}
