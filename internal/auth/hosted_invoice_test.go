package auth

import (
	"fmt"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTokenID = "hit_01JAB3K7Q9X2M4N8P6R5T1V0WZ"

func hostedInvoiceTestSigner(secret string) *HostedInvoiceSigner {
	return NewHostedInvoiceSigner(&config.Configuration{Auth: config.AuthConfig{Secret: secret}})
}

func TestHostedInvoiceSigner_RoundTrip(t *testing.T) {
	signer := hostedInvoiceTestSigner("secret")

	token := signer.Sign(testTokenID)
	id, sig, _ := strings.Cut(token, ".")
	assert.Equal(t, testTokenID, id)
	assert.Len(t, sig, 43)

	tokenID, err := signer.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, testTokenID, tokenID)
}

func TestHostedInvoiceSigner_VerifyRejects(t *testing.T) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	signer := hostedInvoiceTestSigner("secret")
	token := signer.Sign(testTokenID)
	_, sig, _ := strings.Cut(token, ".")
	_, sigOfA, _ := strings.Cut(signer.Sign("hit_A"), ".")

	// The last character of a 43-char signature carries 2 unused bits; flipping one
	// leaves the decoded bytes unchanged unless decoding is strict.
	last := strings.IndexByte(alphabet, token[len(token)-1])

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "no signature", token: testTokenID},
		{name: "no token id", token: "." + sig},
		{name: "empty signature", token: testTokenID + "."},
		{name: "signature not base64url", token: testTokenID + ".!!!"},
		{name: "padded signature", token: token + "="},
		{name: "extra segment", token: token + ".x"},
		{name: "truncated signature", token: testTokenID + "." + sig[:20]},
		{name: "signature of another id", token: "hit_B." + sigOfA},
		{name: "signed with another key", token: hostedInvoiceTestSigner("other").Sign(testTokenID)},
		{name: "alternate spelling of signature", token: token[:len(token)-1] + string(alphabet[last^1])},
	}
	for i := range token {
		if token[i] == '.' {
			continue
		}
		edited := []byte(token)
		edited[i] = 'A'
		if token[i] == 'A' {
			edited[i] = 'B'
		}
		tests = append(tests, struct {
			name  string
			token string
		}{name: fmt.Sprintf("edit at %d", i), token: string(edited)})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := signer.Verify(tt.token)
			require.Error(t, err)
			assert.True(t, ierr.IsNotFound(err))
		})
	}
}
