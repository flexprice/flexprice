package razorpay

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRazorpayToken_ConfirmedUPI(t *testing.T) {
	t.Parallel()
	raw := map[string]interface{}{
		"id":         "token_abc",
		"method":     "upi",
		"max_amount": float64(1500000), // paise
		"created_at": float64(1751328000),
		"recurring_details": map[string]interface{}{
			"status": "confirmed",
		},
	}
	pm, err := normalizeRazorpayToken(raw)
	require.NoError(t, err)
	require.Equal(t, "token_abc", pm.GatewayMethodID)
	require.Equal(t, types.PaymentMethodTypeUPI, pm.Method)
	require.Equal(t, types.PaymentMethodStatusActive, pm.Status)
	require.True(t, pm.MaxAmount.Equal(decimal.NewFromInt(15000))) // paise → rupees
	require.Nil(t, pm.ExpiresAt)
}

func TestNormalizeRazorpayToken_RejectedStatus(t *testing.T) {
	t.Parallel()
	raw := map[string]interface{}{
		"id":     "token_xyz",
		"method": "upi",
		"recurring_details": map[string]interface{}{
			"status": "rejected",
		},
	}
	pm, err := normalizeRazorpayToken(raw)
	require.NoError(t, err)
	require.Equal(t, types.PaymentMethodStatusInactive, pm.Status)
}

func newTestProviderPaymentMethod(id string, status types.PaymentMethodStatus, createdAt time.Time) *interfaces.ProviderPaymentMethod {
	maxAmount := decimal.NewFromInt(1000)
	return &interfaces.ProviderPaymentMethod{
		GatewayMethodID: id,
		Method:          types.PaymentMethodTypeUPI,
		Status:          status,
		MaxAmount:       &maxAmount,
		CreatedAt:       createdAt,
	}
}

func TestSelectUsableToken_FiltersAndSortsByCreatedAtDesc(t *testing.T) {
	t.Parallel()
	older := newTestProviderPaymentMethod("token_old", types.PaymentMethodStatusActive, time.Unix(1000, 0))
	newer := newTestProviderPaymentMethod("token_new", types.PaymentMethodStatusActive, time.Unix(2000, 0))
	inactive := newTestProviderPaymentMethod("token_inactive", types.PaymentMethodStatusInactive, time.Unix(3000, 0))

	selected, ok := SelectUsableToken([]*interfaces.ProviderPaymentMethod{older, newer, inactive}, types.PaymentMethodTypeUPI, decimal.NewFromInt(100))
	require.True(t, ok)
	require.Equal(t, "token_new", selected.GatewayMethodID)
}

func TestSelectUsableToken_NoneUsable(t *testing.T) {
	t.Parallel()
	_, ok := SelectUsableToken(nil, types.PaymentMethodTypeUPI, decimal.NewFromInt(100))
	require.False(t, ok)
}

func TestSelectUsableToken_FiltersOutOverCeiling(t *testing.T) {
	t.Parallel()
	pm := newTestProviderPaymentMethod("token_low_ceiling", types.PaymentMethodStatusActive, time.Now())
	_, ok := SelectUsableToken([]*interfaces.ProviderPaymentMethod{pm}, types.PaymentMethodTypeUPI, decimal.NewFromInt(999999))
	require.False(t, ok, "invoice total exceeds token's MaxAmount (1000), must not be selected")
}
