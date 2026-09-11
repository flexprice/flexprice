// Package licensing mints short-lived Heimdall licensing tokens and publishes
// the backend's public key as a JWKS so Heimdall can verify them without ever
// holding the flexprice user-auth secret.
package licensing

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/golang-jwt/jwt/v4"
)

// TokenClaims are the claims minted into a Heimdall licensing token.
type TokenClaims struct {
	TenantID string `json:"tenant_id"`
	Region   string `json:"region"`
	IsAdmin  bool   `json:"is_admin"`
	Customer string `json:"customer,omitempty"` // tenant/org display name
	jwt.RegisteredClaims
}

// Signer mints EdDSA licensing tokens and exposes the matching JWKS.
type Signer struct {
	kid        string
	region     string
	issuer     string
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// NewSigner builds a Signer from config. Returns (nil, nil) when no signing
// key is configured, so callers can treat licensing as an optional feature.
func NewSigner(cfg *config.Configuration) (*Signer, error) {
	seedB64 := cfg.Licensing.SigningKeySeed
	if seedB64 == "" {
		return nil, nil
	}

	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid licensing signing key seed").
			Mark(ierr.ErrSystem)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, ierr.NewErrorf("licensing signing key seed must be %d bytes, got %d", ed25519.SeedSize, len(seed)).
			WithHint("Invalid licensing signing key seed").
			Mark(ierr.ErrSystem)
	}

	privateKey := ed25519.NewKeyFromSeed(seed)
	region := cfg.Licensing.Region
	if region == "" {
		region = "default"
	}
	issuer := cfg.Licensing.Issuer
	if issuer == "" {
		issuer = "flexprice-backend"
	}

	return &Signer{
		// kid rotates monthly so a key can be retired without invalidating
		// tokens minted seconds ago — Heimdall's JWKS fetch just needs to see
		// the new kid before the old one drops off.
		kid:        kidFor(region, time.Now()),
		region:     region,
		issuer:     issuer,
		privateKey: privateKey,
		publicKey:  privateKey.Public().(ed25519.PublicKey),
	}, nil
}

func kidFor(region string, t time.Time) string {
	return fmt.Sprintf("backend-%s-%s", region, t.UTC().Format("2006-01"))
}

// Mint signs a licensing token for the given tenant/admin status, capping exp
// at the caller-supplied session expiry (the token must never outlive the
// user's own session).
func (s *Signer) Mint(tenantID, customer string, isAdmin bool, sessionExp time.Time) (string, error) {
	now := time.Now()
	exp := sessionExp
	// A session with no/expired exp mints nothing rather than defaulting to a
	// token that outlives an already-invalid session.
	if exp.IsZero() || !exp.After(now) {
		return "", ierr.NewError("session has no valid expiry to bound the licensing token").
			WithHint("Your session has expired, please log in again").
			Mark(ierr.ErrPermissionDenied)
	}

	claims := TokenClaims{
		TenantID: tenantID,
		Region:   s.region,
		IsAdmin:  isAdmin,
		Customer: customer,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{"heimdall-mint"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = s.kid

	signed, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", ierr.WithError(err).
			WithHint("Failed to sign licensing token").
			Mark(ierr.ErrSystem)
	}
	return signed, nil
}

// JWK is the OKP/Ed25519 JSON Web Key shape Heimdall expects.
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
}

// JWKS renders the backend's current public key set. Only one key is active
// at a time today (kid rotates monthly); a fixed-size slice keeps the door
// open for publishing the previous month's key during rotation overlap
// without changing the response shape.
func (s *Signer) JWKS() []JWK {
	return []JWK{
		{
			Kty: "OKP",
			Crv: "Ed25519",
			X:   base64.RawURLEncoding.EncodeToString(s.publicKey),
			Kid: s.kid,
			Use: "sig",
			Alg: "EdDSA",
		},
	}
}
