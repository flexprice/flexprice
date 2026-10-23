package razorpay

import (
	"crypto/hmac"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebhookSignature(t *testing.T) {
	payload := []byte(`{"event":"payment.captured"}`)
	good := webhookSignature(payload, "secret")

	tests := []struct {
		name string
		sig  string
		want bool
	}{
		{"match", good, true},
		{"mismatch", webhookSignature(payload, "other"), false},
		{"truncated", good[:len(good)-1], false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hmac.Equal([]byte(good), []byte(tt.sig)))
		})
	}
}
