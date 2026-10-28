package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"golang.org/x/crypto/bcrypt"
)

// PasswordLinkPurpose says why a password link was issued.
type PasswordLinkPurpose string

const (
	// PasswordLinkPurposeInvite lets an invited user create their first password.
	PasswordLinkPurposeInvite PasswordLinkPurpose = "invite"
	// PasswordLinkPurposeReset lets an existing user replace their password.
	PasswordLinkPurposeReset PasswordLinkPurpose = "reset"
)

// passwordLinkKeyLabel keeps the link key apart from every other use of auth.secret.
const passwordLinkKeyLabel = "flexprice-password-link-v1"

// PasswordLinkClaims is the data carried inside a password link.
type PasswordLinkClaims struct {
	UserID    string              `json:"u"`
	TenantID  string              `json:"t"`
	Purpose   PasswordLinkPurpose `json:"p"`
	ExpiresAt int64               `json:"e"` // Unix seconds
	Nonce     string              `json:"n"`
}

// passwordLinkSigner makes and checks one-time password links. A link is
// <data>.<signature>, and the signature also covers the user's current password
// hash, so any password change invalidates every older link.
type passwordLinkSigner struct {
	key []byte
}

// NewPasswordLinkSigner derives the link key from the auth secret.
func NewPasswordLinkSigner(secret string) (*passwordLinkSigner, error) {
	if secret == "" {
		return nil, ierr.NewError("auth secret is not set").
			WithHint("Password links need auth.secret to be configured").
			Mark(ierr.ErrSystem)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(passwordLinkKeyLabel))
	return &passwordLinkSigner{key: mac.Sum(nil)}, nil
}

// Sign returns a link for claims, bound to currentPasswordHash ("" when the user has no password yet).
func (s *passwordLinkSigner) Sign(claims PasswordLinkClaims, currentPasswordHash string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create password link").
			Mark(ierr.ErrSystem)
	}
	claims.Nonce = base64.RawURLEncoding.EncodeToString(nonce)

	raw, err := json.Marshal(claims)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to create password link").
			Mark(ierr.ErrSystem)
	}

	data := base64.RawURLEncoding.EncodeToString(raw)
	return data + "." + base64.RawURLEncoding.EncodeToString(s.signature(data, currentPasswordHash)), nil
}

// Verify returns the claims only for a link this key signed against currentPasswordHash that has not expired.
func (s *passwordLinkSigner) Verify(token, currentPasswordHash string, now time.Time) (*PasswordLinkClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, NewInvalidPasswordLinkError()
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, s.signature(parts[0], currentPasswordHash)) {
		return nil, NewInvalidPasswordLinkError()
	}

	claims, err := decodePasswordLinkClaims(parts[0])
	if err != nil {
		return nil, err
	}
	if claims.Purpose != PasswordLinkPurposeInvite && claims.Purpose != PasswordLinkPurposeReset {
		return nil, NewInvalidPasswordLinkError()
	}
	if !now.Before(time.Unix(claims.ExpiresAt, 0)) {
		return nil, NewInvalidPasswordLinkError()
	}

	return claims, nil
}

func (s *passwordLinkSigner) signature(data, currentPasswordHash string) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(data + "." + currentPasswordHash))
	return mac.Sum(nil)
}

// ReadPasswordLinkClaims decodes a link without checking it, to learn which user to load before Verify.
func ReadPasswordLinkClaims(token string) (*PasswordLinkClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, NewInvalidPasswordLinkError()
	}
	return decodePasswordLinkClaims(parts[0])
}

func decodePasswordLinkClaims(data string) (*PasswordLinkClaims, error) {
	raw, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return nil, NewInvalidPasswordLinkError()
	}

	var claims PasswordLinkClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, NewInvalidPasswordLinkError()
	}
	if claims.UserID == "" || claims.TenantID == "" {
		return nil, NewInvalidPasswordLinkError()
	}

	return &claims, nil
}

// NewInvalidPasswordLinkError is the one answer for any link that cannot be used.
func NewInvalidPasswordLinkError() error {
	return ierr.NewError("password link is invalid").
		WithHint("This link has expired or was already used. Ask your admin for a new one.").
		Mark(ierr.ErrNotFound)
}

// HashPassword returns the bcrypt hash stored for flexprice logins.
func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to hash password").
			Mark(ierr.ErrSystem)
	}
	return string(hashed), nil
}
