package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// hostedInvoiceSignatureEncoding is strict so each signature has exactly one valid spelling.
var hostedInvoiceSignatureEncoding = base64.RawURLEncoding.Strict()

// HostedInvoiceSigner signs and verifies hosted invoice tokens of the form
// "<token id>.<base64url HMAC-SHA256 of the token id>".
type HostedInvoiceSigner struct {
	key []byte
}

// NewHostedInvoiceSigner signs with the deployment's auth secret.
func NewHostedInvoiceSigner(cfg *config.Configuration) *HostedInvoiceSigner {
	return &HostedInvoiceSigner{key: []byte(cfg.Auth.Secret)}
}

// Sign returns the token for a token id.
func (s *HostedInvoiceSigner) Sign(tokenID string) string {
	return tokenID + "." + hostedInvoiceSignatureEncoding.EncodeToString(hostedInvoiceMAC(s.key, tokenID))
}

// Verify returns the token id of a token signed with the signer's key.
func (s *HostedInvoiceSigner) Verify(token string) (string, error) {
	tokenID, encoded, ok := strings.Cut(token, ".")
	if !ok || tokenID == "" || encoded == "" {
		return "", errInvalidHostedInvoiceToken()
	}
	sig, err := hostedInvoiceSignatureEncoding.DecodeString(encoded)
	if err != nil || len(sig) != sha256.Size {
		return "", errInvalidHostedInvoiceToken()
	}
	if !hmac.Equal(sig, hostedInvoiceMAC(s.key, tokenID)) {
		return "", errInvalidHostedInvoiceToken()
	}
	return tokenID, nil
}

func hostedInvoiceMAC(key []byte, tokenID string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(tokenID))
	return mac.Sum(nil)
}

func errInvalidHostedInvoiceToken() error {
	return ierr.NewError("invalid hosted invoice token").
		WithHint("This link is not valid").
		Mark(ierr.ErrNotFound)
}
