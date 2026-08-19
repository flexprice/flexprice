package auth

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
)

const testSecret = "test-secret-key-32-bytes-minimum!"

func ssoTestConfig(provider types.AuthProvider, samlEnabled bool) *config.Configuration {
	cfg := &config.Configuration{}
	cfg.Auth.Provider = provider
	cfg.Auth.Secret = testSecret
	cfg.Auth.SAML.Enabled = samlEnabled
	return cfg
}

// mintSSOToken produces what the SAML ACS handler issues.
func mintSSOToken(t *testing.T, cfg *config.Configuration, tenantID, userID string) string {
	t.Helper()
	token, _, err := NewSSOTokenIssuer(cfg).Issue(tenantID, userID, 24)
	require.NoError(t, err)
	return token
}

// TestSSOTokenRoundTrip is the bug this exists for: a deployment whose password
// provider is Supabase could not validate the token its own SAML login minted,
// because the two use different claim names. The SSO validator has to accept it
// regardless of which provider handles password login.
func TestSSOTokenRoundTrip(t *testing.T) {
	for _, provider := range []types.AuthProvider{types.AuthProviderSupabase, types.AuthProviderFlexprice} {
		t.Run(string(provider), func(t *testing.T) {
			cfg := ssoTestConfig(provider, true)
			token := mintSSOToken(t, cfg, "tenant_1", "user_1")

			claims, err := NewSSOTokenValidator(cfg).Validate(context.Background(), token)
			require.NoError(t, err)
			assert.Equal(t, "tenant_1", claims.TenantID)
			assert.Equal(t, "user_1", claims.UserID)
		})
	}
}

// ── Security properties ─────────────────────────────────────────────────────
//
// Both providers sign with the same auth.secret, so a signature alone cannot
// prove a token came from the SSO flow. These cover what must hold anyway.

// A deployment that does not offer SAML must not accept an SSO token at all,
// so enabling SAML is the only thing that widens what the API accepts.
func TestSSOTokenRejectedWhenSAMLDisabled(t *testing.T) {
	minting := ssoTestConfig(types.AuthProviderSupabase, true)
	token := mintSSOToken(t, minting, "tenant_1", "user_1")

	disabled := ssoTestConfig(types.AuthProviderSupabase, false)
	_, err := NewSSOTokenValidator(disabled).Validate(context.Background(), token)

	require.Error(t, err, "a deployment with SAML off must refuse an SSO token")
}

// An ordinary token — one without the SSO marker — must NOT be accepted by the
// SSO validator. Otherwise the SSO path would become a second, weaker way to
// validate any token signed with the shared secret.
func TestSSOValidatorRejectsNonSSOToken(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	plain := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"tenant_id": "tenant_1",
		"user_id":   "user_1",
		"exp":       time.Now().Add(time.Hour).Unix(),
	})
	signed, err := plain.SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
	require.Error(t, err, "a token without the SSO marker must not validate as SSO")
}

// A token signed with a different secret must never validate, marker or not.
func TestSSOValidatorRejectsForeignSignature(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"tenant_id":   "tenant_1",
		"user_id":     "attacker",
		ssoTokenClaim: true,
		"exp":         time.Now().Add(time.Hour).Unix(),
	})
	signed, err := forged.SignedString([]byte("a-completely-different-secret-key"))
	require.NoError(t, err)

	_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
	require.Error(t, err, "a token signed with another secret must be refused")
}

// alg=none must be refused; accepting it would let anyone mint any identity.
func TestSSOValidatorRejectsUnsignedToken(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"tenant_id":   "tenant_1",
		"user_id":     "attacker",
		ssoTokenClaim: true,
		"exp":         time.Now().Add(time.Hour).Unix(),
	})
	signed, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
	require.Error(t, err, "an unsigned token must be refused")
}

func TestSSOValidatorRejectsExpiredToken(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"tenant_id":   "tenant_1",
		"user_id":     "user_1",
		ssoTokenClaim: true,
		"exp":         time.Now().Add(-time.Hour).Unix(),
	})
	signed, err := expired.SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
	require.Error(t, err, "an expired token must be refused")
}

// Claims that name no user or no tenant must be refused: the middleware uses
// both to scope the session, and an empty value would widen that scope.
func TestSSOValidatorRequiresIdentityClaims(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	for name, claims := range map[string]jwt.MapClaims{
		"no user":    {"tenant_id": "tenant_1", ssoTokenClaim: true, "exp": time.Now().Add(time.Hour).Unix()},
		"no tenant":  {"user_id": "user_1", ssoTokenClaim: true, "exp": time.Now().Add(time.Hour).Unix()},
		"empty user": {"tenant_id": "tenant_1", "user_id": "", ssoTokenClaim: true, "exp": time.Now().Add(time.Hour).Unix()},
	} {
		t.Run(name, func(t *testing.T) {
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
			require.NoError(t, err)

			_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
			require.Error(t, err)
		})
	}
}

// The issuer must refuse to mint without an identity, so a bug upstream cannot
// produce a token that names nobody.
func TestSSOIssuerRequiresIdentity(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	_, _, err := NewSSOTokenIssuer(cfg).Issue("", "user_1", 24)
	require.Error(t, err, "minting without a tenant must fail")

	_, _, err = NewSSOTokenIssuer(cfg).Issue("tenant_1", "", 24)
	require.Error(t, err, "minting without a user must fail")
}

// A non-positive expiry would mint an already-expired token while returning a
// future expiresAt, so the login looks successful and fails on the first
// request instead — the shape this type exists to prevent.
func TestSSOIssuerRejectsNonPositiveExpiry(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	for _, hours := range []int{0, -1, -24} {
		_, _, err := NewSSOTokenIssuer(cfg).Issue("tenant_1", "user_1", hours)
		require.Error(t, err, "expiryHours=%d must be refused", hours)
	}
}

// ── Adversarial cases ───────────────────────────────────────────────────────
//
// Both providers sign with the same auth.secret, so these establish that
// routing by an unverified marker cannot be turned into an advantage.

func TestAttackMarkerDoesNotBypassValidation(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	// A Supabase-shaped token (sub, app_metadata) that the ordinary validator
	// WOULD accept, with the SSO marker bolted on to force SSO routing.
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":          "supabase-user",
		"app_metadata": map[string]any{"tenant_id": "tenant_1"},
		ssoTokenClaim:  true,
		"exp":          time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString([]byte(testSecret))
	require.NoError(t, err)

	require.True(t, IsSSOToken(signed), "marker should route this to the SSO validator")

	// The SSO validator must refuse it: it carries no user_id/tenant_id claims.
	_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
	require.Error(t, err, "SSO routing must not accept a token lacking SSO identity claims")
}

// ATTACK 2: on a deployment with SAML OFF, can an attacker who somehow obtains
// a validly-signed SSO token get in? Must be refused outright.
func TestAttackSSOTokenOnNonSAMLDeployment(t *testing.T) {
	minting := ssoTestConfig(types.AuthProviderFlexprice, true)
	stolen := mintSSOToken(t, minting, "tenant_1", "victim")

	for _, provider := range []types.AuthProvider{types.AuthProviderSupabase, types.AuthProviderFlexprice} {
		victim := ssoTestConfig(provider, false) // SAML disabled
		_, err := NewSSOTokenValidator(victim).Validate(context.Background(), stolen)
		require.Error(t, err, "provider=%s with SAML off must refuse an SSO token", provider)
	}
}

// ATTACK 3: a checkout/session token signed with the same secret must not be
// usable as a dashboard credential just by carrying the marker.
func TestAttackForeignTokenTypeRejected(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	// Session-token shaped claims, no user_id.
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"customer_id": "cust_1",
		"tenant_id":   "tenant_1",
		ssoTokenClaim: true,
		"exp":         time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString([]byte(testSecret))
	require.NoError(t, err)

	_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
	require.Error(t, err, "a session-shaped token must not authenticate a dashboard request")
}

// ATTACK 4: algorithm confusion — HMAC-signing using a value an attacker might
// guess is still refused unless it is the real secret.
func TestAttackWrongSecretRejectedAcrossAlgs(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	for _, method := range []jwt.SigningMethod{jwt.SigningMethodHS256, jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		tok := jwt.NewWithClaims(method, jwt.MapClaims{
			"tenant_id":   "tenant_1",
			"user_id":     "attacker",
			ssoTokenClaim: true,
			"exp":         time.Now().Add(time.Hour).Unix(),
		})
		signed, err := tok.SignedString([]byte("guessed-secret"))
		require.NoError(t, err)

		_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
		require.Error(t, err, "alg=%s with a wrong secret must be refused", method.Alg())
	}
}

// ATTACK 5: a non-SSO token must still work normally — the change must not
// break password login by hijacking ordinary tokens.
func TestOrdinaryTokenNotRoutedToSSO(t *testing.T) {
	plain := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u", "exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := plain.SignedString([]byte(testSecret))
	require.NoError(t, err)

	require.False(t, IsSSOToken(signed), "an ordinary token must not be routed to the SSO validator")
}

// ATTACK 6: marker as a non-bool (string "true", 1) must not satisfy the check.
func TestAttackMarkerTypeConfusion(t *testing.T) {
	cfg := ssoTestConfig(types.AuthProviderSupabase, true)

	for name, marker := range map[string]any{"string": "true", "number": 1, "nonempty": "yes"} {
		t.Run(name, func(t *testing.T) {
			tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"tenant_id":   "tenant_1",
				"user_id":     "attacker",
				ssoTokenClaim: marker,
				"exp":         time.Now().Add(time.Hour).Unix(),
			})
			signed, err := tok.SignedString([]byte(testSecret))
			require.NoError(t, err)

			_, err = NewSSOTokenValidator(cfg).Validate(context.Background(), signed)
			require.Error(t, err, "a non-bool marker must not satisfy the SSO check")
		})
	}
}
