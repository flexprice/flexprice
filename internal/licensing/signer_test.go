package licensing

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func testSeed(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(priv.Seed())
}

func TestNewSigner_NoSeedDisablesLicensing(t *testing.T) {
	s, err := NewSigner(&config.Configuration{})
	require.NoError(t, err)
	require.Nil(t, s)
}

func TestMint_ClaimsAndExpCapping(t *testing.T) {
	cfg := &config.Configuration{Licensing: config.LicensingConfig{
		SigningKeySeed: testSeed(t),
		Region:         "us-east",
		Issuer:         "flexprice-backend",
	}}
	s, err := NewSigner(cfg)
	require.NoError(t, err)
	require.NotNil(t, s)

	sessionExp := time.Now().Add(10 * time.Minute)
	tokenStr, err := s.Mint("tenant_123", true, sessionExp)
	require.NoError(t, err)

	parsed, err := jwt.ParseWithClaims(tokenStr, &TokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		return s.publicKey, nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	require.Equal(t, "EdDSA", parsed.Method.Alg())
	require.Equal(t, s.kid, parsed.Header["kid"])

	claims := parsed.Claims.(*TokenClaims)
	require.Equal(t, "flexprice-backend", claims.Issuer)
	require.Equal(t, jwt.ClaimStrings{"heimdall-mint"}, claims.Audience)
	require.Equal(t, "tenant_123", claims.TenantID)
	require.Equal(t, "us-east", claims.Region)
	require.True(t, claims.IsAdmin)
	require.WithinDuration(t, sessionExp, claims.ExpiresAt.Time, time.Second)
	require.LessOrEqual(t, claims.ExpiresAt.Time, sessionExp.Add(time.Second))
}

func TestMint_NonStaffGetsIsAdminFalse(t *testing.T) {
	cfg := &config.Configuration{Licensing: config.LicensingConfig{SigningKeySeed: testSeed(t)}}
	s, err := NewSigner(cfg)
	require.NoError(t, err)

	tokenStr, err := s.Mint("tenant_x", false, time.Now().Add(time.Minute))
	require.NoError(t, err)

	parsed, err := jwt.ParseWithClaims(tokenStr, &TokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		return s.publicKey, nil
	})
	require.NoError(t, err)
	claims := parsed.Claims.(*TokenClaims)
	require.False(t, claims.IsAdmin)
}

func TestMint_RejectsExpiredSession(t *testing.T) {
	cfg := &config.Configuration{Licensing: config.LicensingConfig{SigningKeySeed: testSeed(t)}}
	s, err := NewSigner(cfg)
	require.NoError(t, err)

	_, err = s.Mint("tenant_x", false, time.Now().Add(-time.Minute))
	require.Error(t, err)
}

func TestJWKS_MatchesSigningKey(t *testing.T) {
	cfg := &config.Configuration{Licensing: config.LicensingConfig{SigningKeySeed: testSeed(t)}}
	s, err := NewSigner(cfg)
	require.NoError(t, err)

	keys := s.JWKS()
	require.Len(t, keys, 1)
	require.Equal(t, "OKP", keys[0].Kty)
	require.Equal(t, "Ed25519", keys[0].Crv)
	require.Equal(t, s.kid, keys[0].Kid)

	decoded, err := base64.RawURLEncoding.DecodeString(keys[0].X)
	require.NoError(t, err)
	require.Equal(t, ed25519.PublicKey(decoded), s.publicKey)

	// The published key must actually verify tokens minted by this signer.
	tokenStr, err := s.Mint("tenant_1", false, time.Now().Add(time.Minute))
	require.NoError(t, err)
	_, err = jwt.ParseWithClaims(tokenStr, &TokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		return ed25519.PublicKey(decoded), nil
	})
	require.NoError(t, err)
}
