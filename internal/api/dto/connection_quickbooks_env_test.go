package dto

import (
	"encoding/json"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCreateConnectionRequest_QuickBooksEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    string
		wantErr bool
	}{
		{"lowercase production", "production", "production", false},
		{"capitalised production", "Production", "production", false},
		{"padded sandbox", "  SANDBOX ", "sandbox", false},
		{"unknown rejected", "staging", "staging", true},
		{"empty rejected", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"name":          "qb",
				"provider_type": types.SecretProviderQuickBooks,
				"encrypted_secret_data": map[string]any{
					"client_id":     "id",
					"client_secret": "secret",
					"realm_id":      "123456",
					"environment":   tt.env,
				},
			})
			require.NoError(t, err)

			var req CreateConnectionRequest
			require.NoError(t, json.Unmarshal(body, &req))
			qb := req.EncryptedSecretData.QuickBooks
			require.NotNil(t, qb)
			require.Equal(t, tt.want, qb.Environment)
			if tt.wantErr {
				require.Error(t, qb.Validate())
			} else {
				require.NoError(t, qb.Validate())
			}
		})
	}
}
