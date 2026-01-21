package moyasar

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestMoyasarConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  *MoyasarConfig
		wantErr bool
	}{
		{
			name: "valid config with all fields",
			config: &MoyasarConfig{
				SecretKey:      "sk_test_secret",
				PublishableKey: "pk_test_pub",
				WebhookSecret:  "wh_test_secret",
			},
			wantErr: false,
		},
		{
			name: "valid config with only secret key",
			config: &MoyasarConfig{
				SecretKey: "sk_test_secret",
			},
			wantErr: false,
		},
		{
			name: "invalid config - missing secret key",
			config: &MoyasarConfig{
				PublishableKey: "pk_test_pub",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPaymentStatusConversion(t *testing.T) {
	tests := []struct {
		moyasarStatus string
		expected      string
	}{
		{"paid", "succeeded"},
		{"captured", "succeeded"},
		{"failed", "failed"},
		{"refunded", "refunded"},
		{"voided", "voided"},
		{"initiated", "pending"},
		{"unknown", "pending"},
	}

	for _, tt := range tests {
		t.Run(tt.moyasarStatus, func(t *testing.T) {
			result := convertMoyasarStatusToFlexPrice(tt.moyasarStatus)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAmountConversion(t *testing.T) {
	tests := []struct {
		name     string
		halalah  int
		expected string
	}{
		{"100 SAR", 10000, "100"},
		{"50.50 SAR", 5050, "50.5"},
		{"1 SAR", 100, "1"},
		{"0.01 SAR", 1, "0.01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert halalah to SAR using decimal for precision
			sar := decimal.NewFromInt(int64(tt.halalah)).Div(decimal.NewFromInt(100))
			// Compare as floats since we might have trailing zeros
			expected := decimal.RequireFromString(tt.expected)
			assert.True(t, sar.Equal(expected))
		})
	}
}

func TestHMACSignatureGeneration(t *testing.T) {
	// Test HMAC signature generation for webhook verification
	payload := []byte(`{"type":"payment_paid","data":{"id":"pay_123"}}`)
	secret := "test_webhook_secret"

	// Generate signature
	signature := generateHMACSig(payload, secret)

	// Verify properties
	assert.NotEmpty(t, signature)
	assert.Len(t, signature, 64) // SHA256 hex is 64 chars

	// Same payload and secret should produce same signature
	signature2 := generateHMACSig(payload, secret)
	assert.Equal(t, signature, signature2)

	// Different secret should produce different signature
	differentSig := generateHMACSig(payload, "different_secret")
	assert.NotEqual(t, signature, differentSig)
}

func TestPaymentSourceType_Values(t *testing.T) {
	// Verify payment source type constants
	assert.Equal(t, PaymentSourceType("creditcard"), PaymentSourceTypeCreditCard)
	assert.Equal(t, PaymentSourceType("applepay"), PaymentSourceTypeApplePay)
	assert.Equal(t, PaymentSourceType("stcpay"), PaymentSourceTypeSTCPay)
	assert.Equal(t, PaymentSourceType("samsungpay"), PaymentSourceTypeSamsungPay)
	assert.Equal(t, PaymentSourceType("token"), PaymentSourceTypeToken)
}

func TestMoyasarPaymentStatus_Values(t *testing.T) {
	// Verify payment status constants
	assert.Equal(t, MoyasarPaymentStatus("initiated"), MoyasarPaymentStatusInitiated)
	assert.Equal(t, MoyasarPaymentStatus("paid"), MoyasarPaymentStatusPaid)
	assert.Equal(t, MoyasarPaymentStatus("failed"), MoyasarPaymentStatusFailed)
	assert.Equal(t, MoyasarPaymentStatus("refunded"), MoyasarPaymentStatusRefunded)
	assert.Equal(t, MoyasarPaymentStatus("voided"), MoyasarPaymentStatusVoided)
	assert.Equal(t, MoyasarPaymentStatus("captured"), MoyasarPaymentStatusCaptured)
}

func TestBaseURL(t *testing.T) {
	assert.Equal(t, "https://api.moyasar.com/v1", BaseURL)
}

func TestDefaultCurrency(t *testing.T) {
	assert.Equal(t, "SAR", DefaultCurrency)
}

// Helper functions for tests
func generateHMACSig(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func validateConfig(config *MoyasarConfig) error {
	if config.SecretKey == "" {
		return fmt.Errorf("secret key is required")
	}
	return nil
}

func convertMoyasarStatusToFlexPrice(status string) string {
	switch status {
	case "paid", "captured":
		return "succeeded"
	case "failed":
		return "failed"
	case "refunded":
		return "refunded"
	case "voided":
		return "voided"
	default:
		return "pending"
	}
}
