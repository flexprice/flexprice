package auth

import (
	"strings"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTokenID = "hit_01JAB3K7Q9X2M4N8P6R5T1V0WZ"

func TestHostedInvoiceToken_RoundTrip(t *testing.T) {
	signer := newHostedInvoiceTokenSigner("secret")

	token := signer.Sign(testTokenID)
	id, sig, _ := strings.Cut(token, ".")
	assert.Equal(t, testTokenID, id)
	assert.Len(t, sig, 43)

	tokenID, err := signer.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, testTokenID, tokenID)
}

func TestHostedInvoiceToken_RejectsEditedToken(t *testing.T) {
	signer := newHostedInvoiceTokenSigner("secret")
	token := signer.Sign(testTokenID)

	for i := range token {
		if token[i] == '.' {
			continue
		}
		edited := []byte(token)
		edited[i] = flip(edited[i])
		_, err := signer.Verify(string(edited))
		require.Error(t, err, "edit at %d", i)
		assert.True(t, ierr.IsNotFound(err))
	}
}

func TestHostedInvoiceToken_RejectsMalformed(t *testing.T) {
	signer := newHostedInvoiceTokenSigner("secret")
	sig := strings.SplitN(signer.Sign(testTokenID), ".", 2)[1]

	for _, token := range []string{
		"",
		testTokenID,
		"." + sig,
		testTokenID + ".",
		testTokenID + ".!!!",
		testTokenID + "." + sig + "=",
		testTokenID + "." + sig + ".x",
		testTokenID + "." + sig[:20],
	} {
		_, err := signer.Verify(token)
		assert.Error(t, err, "token %q", token)
	}
}

func TestHostedInvoiceToken_RejectsSignatureOfAnotherID(t *testing.T) {
	signer := newHostedInvoiceTokenSigner("secret")
	sig := strings.SplitN(signer.Sign("hit_A"), ".", 2)[1]

	_, err := signer.Verify("hit_B." + sig)
	assert.Error(t, err)
}

func TestHostedInvoiceToken_RejectsTokenFromAnotherKey(t *testing.T) {
	token := newHostedInvoiceTokenSigner("other").Sign(testTokenID)

	_, err := newHostedInvoiceTokenSigner("secret").Verify(token)
	assert.Error(t, err)
}

func flip(b byte) byte {
	if b == 'A' {
		return 'B'
	}
	return 'A'
}

func TestHostedInvoiceToken_RejectsAlternateSpellingOfSignature(t *testing.T) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	signer := newHostedInvoiceTokenSigner("secret")
	token := signer.Sign(testTokenID)

	// The last character of a 43-char signature carries 2 unused bits; flipping one
	// leaves the decoded bytes unchanged unless decoding is strict.
	last := strings.IndexByte(alphabet, token[len(token)-1])
	edited := token[:len(token)-1] + string(alphabet[last^1])

	_, err := signer.Verify(edited)
	assert.Error(t, err)
}
