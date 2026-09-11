package v1

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domainUser "github.com/flexprice/flexprice/internal/domain/user"
	eeservice "github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/licensing"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

// fakeTenantService returns a fixed tenant name (embeds the interface so only
// GetTenantByID needs an implementation).
type fakeTenantService struct {
	eeservice.TenantService
	name string
}

func (f fakeTenantService) GetTenantByID(_ context.Context, id string) (*dto.TenantResponse, error) {
	return &dto.TenantResponse{ID: id, Name: f.name}, nil
}

// fakeUserRepo returns a fixed user email for is_admin domain checks.
type fakeUserRepo struct{ email string }

func (f fakeUserRepo) GetByID(_ context.Context, id string) (*domainUser.User, error) {
	return &domainUser.User{ID: id, Email: f.email}, nil
}
func (fakeUserRepo) Create(context.Context, *domainUser.User) error               { return nil }
func (fakeUserRepo) GetByEmail(context.Context, string) (*domainUser.User, error) { return nil, nil }
func (fakeUserRepo) Update(context.Context, *domainUser.User) error               { return nil }
func (fakeUserRepo) UpdateRoles(context.Context, string, []string) error          { return nil }
func (fakeUserRepo) Delete(context.Context, string) error                         { return nil }
func (fakeUserRepo) ListByFilter(context.Context, *types.UserFilter) ([]*domainUser.User, int64, error) {
	return nil, 0, nil
}

func makeLicensingSeed(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(priv.Seed())
}

func setupLicensingHandler(t *testing.T, seed string) *LicensingHandler {
	return setupLicensingHandlerWithEmail(t, seed, "user@acme.com")
}

func setupLicensingHandlerWithEmail(t *testing.T, seed, email string) *LicensingHandler {
	t.Helper()
	cfg := &config.Configuration{
		Logging:   config.LoggingConfig{Level: types.LogLevelInfo},
		Licensing: config.LicensingConfig{SigningKeySeed: seed, Region: "us-east"},
	}
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)

	signer, err := licensing.NewSigner(cfg)
	require.NoError(t, err)

	return NewLicensingHandler(signer, fakeUserRepo{email: email}, fakeTenantService{name: "Acme Corp"}, log)
}

// sessionBearerToken builds a JWT carrying only an "exp" claim, mirroring what
// AuthenticateMiddleware already validated before the handler runs — the
// handler only re-reads "exp", so the signature/secret here is irrelevant.
func sessionBearerToken(t *testing.T, exp time.Time) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": exp.Unix(),
	})
	signed, err := tok.SignedString([]byte("irrelevant-session-secret"))
	require.NoError(t, err)
	return signed
}

// newLicensingRequest builds a recorder + gin context wired the way
// AuthenticateMiddleware would leave it: tenant/roles in context, bearer
// token on the request.
func newLicensingRequest(t *testing.T, tenantID string, isAdmin bool, bearer string) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/v1/licensing-token", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	ctx := req.Context()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, "user_test")
	// isAdmin is now derived from the user's email domain (via userRepo), not
	// from roles; kept in the signature for call-site clarity.
	_ = isAdmin
	c.Request = req.WithContext(ctx)
	return w, c
}

func TestIssueToken_ClaimsMatchSessionAndTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// staff email -> is_admin true
	h := setupLicensingHandlerWithEmail(t, makeLicensingSeed(t), "someone@flexprice.io")

	sessionExp := time.Now().Add(15 * time.Minute).Truncate(time.Second)
	bearer := sessionBearerToken(t, sessionExp)
	w, c := newLicensingRequest(t, "tenant_abc", true, bearer)

	h.IssueToken(c)
	require.Equal(t, http.StatusOK, w.Code)

	var body licensingTokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotEmpty(t, body.Token)

	claims := &licensing.TokenClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(body.Token, claims)
	require.NoError(t, err)

	require.Equal(t, "flexprice-backend", claims.Issuer)
	require.Equal(t, jwt.ClaimStrings{"heimdall-mint"}, claims.Audience)
	require.Equal(t, "tenant_abc", claims.TenantID)
	require.Equal(t, "us-east", claims.Region)
	require.True(t, claims.IsAdmin)
	require.WithinDuration(t, sessionExp, claims.ExpiresAt.Time, time.Second)
	require.LessOrEqual(t, claims.ExpiresAt.Time.Unix(), sessionExp.Unix())
}

func TestIssueToken_NonStaffGetsIsAdminFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupLicensingHandler(t, makeLicensingSeed(t))

	bearer := sessionBearerToken(t, time.Now().Add(time.Minute))
	w, c := newLicensingRequest(t, "tenant_abc", false, bearer)

	h.IssueToken(c)
	require.Equal(t, http.StatusOK, w.Code)

	var body licensingTokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	claims := &licensing.TokenClaims{}
	_, _, err := jwt.NewParser().ParseUnverified(body.Token, claims)
	require.NoError(t, err)
	require.False(t, claims.IsAdmin)
}

func TestIssueToken_NoSessionRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupLicensingHandler(t, makeLicensingSeed(t))

	_, c := newLicensingRequest(t, "tenant_abc", false, "")
	h.IssueToken(c)

	// c.Error defers the HTTP status to ErrorHandler middleware (not wired in
	// this unit test), so the observable contract here is that the handler
	// records a failure and never reaches the success response.
	require.NotEmpty(t, c.Errors)
}

func TestJWKS_ServesMatchingPublicKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := setupLicensingHandler(t, makeLicensingSeed(t))

	bearer := sessionBearerToken(t, time.Now().Add(time.Minute))
	tokenW, tokenCtx := newLicensingRequest(t, "tenant_abc", true, bearer)
	h.IssueToken(tokenCtx)
	var tokenBody licensingTokenResponse
	require.NoError(t, json.Unmarshal(tokenW.Body.Bytes(), &tokenBody))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/.well-known/licensing-jwks.json", nil)
	h.JWKS(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotEmpty(t, w.Header().Get("Cache-Control"))

	var jwks struct {
		Keys []licensing.JWK `json:"keys"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &jwks))
	require.Len(t, jwks.Keys, 1)

	xBytes, err := base64.RawURLEncoding.DecodeString(jwks.Keys[0].X)
	require.NoError(t, err)
	pubKey := ed25519.PublicKey(xBytes)

	_, err = jwt.Parse(tokenBody.Token, func(tok *jwt.Token) (interface{}, error) {
		return pubKey, nil
	})
	require.NoError(t, err)
}
