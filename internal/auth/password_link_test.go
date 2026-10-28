package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const testLinkSecret = "test-link-secret"

func newTestSigner(t *testing.T, secret string) *passwordLinkSigner {
	t.Helper()
	signer, err := NewPasswordLinkSigner(secret)
	require.NoError(t, err)
	return signer
}

func testLinkClaims(now time.Time) PasswordLinkClaims {
	return PasswordLinkClaims{
		UserID:    "user_1",
		TenantID:  "tenant_1",
		Purpose:   PasswordLinkPurposeReset,
		ExpiresAt: now.Add(time.Hour).Unix(),
	}
}

// retargetLink rewrites the user inside a link but keeps its signature, the way an attacker would.
func retargetLink(t *testing.T, token, userID string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 2)
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	data["u"] = userID
	edited, err := json.Marshal(data)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(edited) + "." + parts[1]
}

func TestPasswordLink_VerifiesWhatItSigned(t *testing.T) {
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	signer := newTestSigner(t, testLinkSecret)
	token, err := signer.Sign(testLinkClaims(now), "h1")
	require.NoError(t, err)

	claims, err := signer.Verify(token, "h1", now)

	require.NoError(t, err)
	assert.Equal(t, "user_1", claims.UserID)
	assert.Equal(t, "tenant_1", claims.TenantID)
	assert.Equal(t, PasswordLinkPurposeReset, claims.Purpose)
	assert.Equal(t, now.Add(time.Hour).Unix(), claims.ExpiresAt)
}

func TestPasswordLink_Refuses(t *testing.T) {
	now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
	signer := newTestSigner(t, testLinkSecret)
	token, err := signer.Sign(testLinkClaims(now), "h1")
	require.NoError(t, err)

	unknownPurpose := testLinkClaims(now)
	unknownPurpose.Purpose = "admin"
	unknownPurposeToken, err := signer.Sign(unknownPurpose, "h1")
	require.NoError(t, err)

	loginToken, _, err := newTestAuth().GenerateDevToken("tenant_1", "", "user_1", "", 1)
	require.NoError(t, err)

	tests := []struct {
		name   string
		signer *passwordLinkSigner
		token  string
		hash   string
		now    time.Time
	}{
		{name: "password changed since the link was made", signer: signer, token: token, hash: "h2", now: now},
		{name: "signed with another secret", signer: newTestSigner(t, "another-secret"), token: token, hash: "h1", now: now},
		{name: "expired", signer: signer, token: token, hash: "h1", now: now.Add(time.Hour)},
		{name: "data edited to another user", signer: signer, token: retargetLink(t, token, "user_2"), hash: "h1", now: now},
		{name: "unknown purpose", signer: signer, token: unknownPurposeToken, hash: "h1", now: now},
		{name: "empty", signer: signer, token: "", hash: "h1", now: now},
		{name: "one part", signer: signer, token: "abc", hash: "h1", now: now},
		{name: "three parts", signer: signer, token: "a.b.c", hash: "h1", now: now},
		{name: "login token", signer: signer, token: loginToken, hash: "h1", now: now},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := tt.signer.Verify(tt.token, tt.hash, tt.now)

			assert.Nil(t, claims)
			assert.True(t, ierr.IsNotFound(err), "expected the bad-link error, got %v", err)
		})
	}
}

func TestPasswordLink_IsNotALoginToken(t *testing.T) {
	login := newTestAuth()
	signer := newTestSigner(t, login.AuthConfig.Secret)
	token, err := signer.Sign(testLinkClaims(time.Now()), "h1")
	require.NoError(t, err)

	_, err = login.ValidateToken(context.Background(), token)

	assert.Error(t, err)
}

func TestPasswordLink_TwoLinksDiffer(t *testing.T) {
	signer := newTestSigner(t, testLinkSecret)
	claims := testLinkClaims(time.Now())

	first, err := signer.Sign(claims, "h1")
	require.NoError(t, err)
	second, err := signer.Sign(claims, "h1")
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestNewPasswordLinkSigner_RefusesEmptySecret(t *testing.T) {
	_, err := NewPasswordLinkSigner("")

	assert.Error(t, err)
}

func TestReadPasswordLinkClaims(t *testing.T) {
	signer := newTestSigner(t, testLinkSecret)
	token, err := signer.Sign(testLinkClaims(time.Now()), "h1")
	require.NoError(t, err)

	claims, err := ReadPasswordLinkClaims(token)
	require.NoError(t, err)
	assert.Equal(t, "user_1", claims.UserID)
	assert.Equal(t, "tenant_1", claims.TenantID)

	_, err = ReadPasswordLinkClaims("not-a-link")
	assert.True(t, ierr.IsNotFound(err), "expected the bad-link error, got %v", err)
}

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("password123")
	require.NoError(t, err)

	assert.NotEqual(t, "password123", hash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("password123")))
	assert.Error(t, bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong-pass")))
}

// A link signed with the right key is still refused when its claims cannot be read.
func TestPasswordLink_RefusesUnreadableClaims(t *testing.T) {
	signer := newTestSigner(t, testLinkSecret)
	encode := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

	tests := []struct {
		name string
		data string
	}{
		{name: "claims are not base64", data: "not*base64"},
		{name: "claims are not JSON", data: encode("not json")},
		{name: "claims name no user", data: encode(`{"t":"tenant_1","p":"reset","e":4102444800}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token := tc.data + "." + base64.RawURLEncoding.EncodeToString(signer.signature(tc.data, ""))

			claims, err := signer.Verify(token, "", time.Now())

			assert.Nil(t, claims)
			assert.True(t, ierr.IsNotFound(err), "expected the bad-link error, got %v", err)
		})
	}
}

// bcrypt reads at most 72 bytes, so a longer password must fail instead of being cut short.
func TestHashPassword_RefusesMoreThan72Bytes(t *testing.T) {
	_, err := HashPassword(strings.Repeat("a", 73))

	assert.True(t, ierr.IsSystem(err), "got %v", err)
}
