package razorpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/connection"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type stubConnections struct {
	connection.Repository
	conn *connection.Connection
}

func (s stubConnections) GetByProvider(context.Context, types.SecretProvider) (*connection.Connection, error) {
	return s.conn, nil
}

type passthroughEncryption struct{}

func (passthroughEncryption) Encrypt(s string) (string, error) { return s, nil }
func (passthroughEncryption) Decrypt(s string) (string, error) { return s, nil }
func (passthroughEncryption) Hash(s string) string             { return s }

func TestVerifyWebhookSignature(t *testing.T) {
	ctx := context.Background()
	store := stubConnections{conn: &connection.Connection{
		ID:           "conn_1",
		ProviderType: types.SecretProviderRazorpay,
		EncryptedSecretData: types.ConnectionMetadata{
			Razorpay: &types.RazorpayConnectionMetadata{
				KeyID:         "key",
				SecretKey:     "api-secret",
				WebhookSecret: "whsec",
			},
		},
		BaseModel: types.BaseModel{Status: types.StatusPublished},
	}}
	client := NewClient(store, passthroughEncryption{}, logger.NewNoopLogger())

	payload := []byte(`{"event":"payment.captured"}`)
	mac := hmac.New(sha256.New, []byte("whsec"))
	mac.Write(payload)
	good := hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name    string
		sig     string
		wantErr bool
	}{
		{"match", good, false},
		{"truncated", good[:len(good)-1], true},
		{"empty", "", true},
		{"wrong secret", webhookSignature(payload, "other"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.VerifyWebhookSignature(ctx, payload, tt.sig)
			require.Equal(t, tt.wantErr, err != nil)
		})
	}
}
