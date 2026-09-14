package v1

import (
	"net/http"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	domainUser "github.com/flexprice/flexprice/internal/domain/user"
	eeservice "github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/licensing"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// staffEmailDomain gates is_admin: only Flexprice staff (verified @flexprice.io
// email) may mint enterprise / other-tenant licenses.
const staffEmailDomain = "@flexprice.io"

// LicensingHandler mints licensing tokens and publishes the backend's
// public key as a JWKS. See internal/licensing for the signing logic.
type LicensingHandler struct {
	cfg           *config.Configuration
	signer        *licensing.Signer
	userRepo      domainUser.Repository
	tenantService eeservice.TenantService
	log           *logger.Logger
}

func NewLicensingHandler(cfg *config.Configuration, signer *licensing.Signer, userRepo domainUser.Repository, tenantService eeservice.TenantService, log *logger.Logger) *LicensingHandler {
	return &LicensingHandler{cfg: cfg, signer: signer, userRepo: userRepo, tenantService: tenantService, log: log}
}

type licensingTokenResponse struct {
	Token string `json:"token"`
}

// @Summary Mint a licensing token
// @Description Issues a short-lived EdDSA-signed token, capped at the caller's session expiry, for the browser to present to the central licensing service.
// @Tags Licensing
// @Produce json
// @Success 200 {object} licensingTokenResponse
// @Router /licensing-token [get]
func (h *LicensingHandler) IssueToken(c *gin.Context) {
	if h.signer == nil {
		c.Error(ierr.NewError("licensing is not configured on this deployment").
			WithHint("Licensing is not enabled").
			Mark(ierr.ErrNotFound))
		return
	}

	ctx := c.Request.Context()
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		c.Error(ierr.NewError("missing tenant in authenticated request").
			WithHint("Unauthorized").
			Mark(ierr.ErrPermissionDenied))
		return
	}

	// Only a tenant super-admin may mint licenses. A licensing token is the
	// browser's only path to the licensing service's mint endpoint, so gating it here gates
	// minting for the whole tenant — non-admin members get 403 and no token.
	if !types.IsSuperAdminUser(ctx) {
		c.Error(ierr.NewError("only a tenant super-admin may mint licenses").
			WithHint("You do not have permission to generate a license").
			Mark(ierr.ErrPermissionDenied))
		return
	}

	// A licensing token's exp is bound to the caller's verified session JWT
	// below via an unverified re-parse — safe only because AuthenticateMiddleware
	// already verified that exact token's signature. An API-key caller (config
	// key or DB-backed secret) never went through that verification: any
	// Authorization header they also attach is unauthenticated user input, and
	// reading "exp" off it would let them forge an arbitrarily long-lived
	// licensing token. Refuse minting outright for API-key callers — they have
	// no user session to bound against.
	if c.GetHeader(h.cfg.Auth.APIKey.Header) != "" {
		c.Error(ierr.NewError("licensing tokens require a user session").
			WithHint("Please log in with a user session to mint a licensing token").
			Mark(ierr.ErrPermissionDenied))
		return
	}

	sessionExp, err := sessionExpiryFromRequest(c)
	if err != nil {
		c.Error(err)
		return
	}

	// is_admin means Flexprice STAFF (us), not tenant owner — every user is
	// super-admin of their own tenant, so that check is wrong here. Gate on a
	// @flexprice.io email instead.
	// SECURITY: the user model carries no IdP-verified-email or trusted-provider
	// signal (domainUser.User has no EmailVerified/Provider field, and
	// Supabase's EmailConfirmed is an Admin-API call keyed on Supabase user ID,
	// not something userRepo exposes here). Under the self-serve flexprice
	// provider, email is unverified and spoofable, so an email-domain match
	// alone must not grant staff powers. Fail closed by default; an operator
	// who has verified their deployment's email flow can opt in explicitly.
	// TODO: wire a real verified-email/trusted-provider signal into
	// domainUser.User (or a dedicated lookup) and require it here instead of
	// this config escape hatch.
	isAdmin := false
	if h.cfg.Licensing.TrustEmailDomainForStaff {
		if u, err := h.userRepo.GetByID(ctx, types.GetUserID(ctx)); err == nil && u != nil {
			isAdmin = strings.HasSuffix(strings.ToLower(u.Email), staffEmailDomain)
		}
	}

	// customer = the caller's org/tenant display name, carried into the token so
	// the licensing service can stamp it on community licenses. For admin mints
	// on another tenant, the licensing service uses a request-supplied customer.
	customer := ""
	if tr, err := h.tenantService.GetTenantByID(ctx, tenantID); err == nil && tr != nil {
		customer = tr.Name
	}

	// issued-by uuid identifies the minting user, sent for everyone (incl.
	// staff) so the licensing service keeps a real audit trail. The is_admin
	// flag rides alongside; the licensing service blanks issued_by in FE
	// responses when it is set, so a staff uuid is stored but never shown.
	issuedBy := types.GetUserID(ctx)

	token, err := h.signer.Mint(tenantID, issuedBy, customer, isAdmin, sessionExp)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, licensingTokenResponse{Token: token})
}

// sessionExpiryFromRequest reads the "exp" claim off the caller's own session
// JWT. The signature was already verified by AuthenticateMiddleware to reach
// this handler, so parsing unverified here only reads a claim, it does not
// widen trust.
func sessionExpiryFromRequest(c *gin.Context) (time.Time, error) {
	authHeader := c.GetHeader(types.HeaderAuthorization)
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == "" || tokenString == authHeader {
		// API-key callers (no bearer JWT) carry no session to bound the
		// licensing token against.
		return time.Time{}, ierr.NewError("licensing tokens require a user session").
			WithHint("Please log in to mint a licensing token").
			Mark(ierr.ErrPermissionDenied)
	}

	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tokenString, claims); err != nil {
		return time.Time{}, ierr.WithError(err).
			WithHint("Invalid session token").
			Mark(ierr.ErrPermissionDenied)
	}

	expFloat, ok := claims["exp"].(float64)
	if !ok {
		return time.Time{}, ierr.NewError("session token has no expiry").
			WithHint("Invalid session token").
			Mark(ierr.ErrPermissionDenied)
	}

	return time.Unix(int64(expFloat), 0), nil
}

// @Summary Publish the backend's licensing JWKS
// @Description Public JWKS of the backend's Ed25519 signing keys, for the licensing service to verify licensing tokens.
// @Tags Licensing
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /.well-known/licensing-jwks.json [get]
func (h *LicensingHandler) JWKS(c *gin.Context) {
	if h.signer == nil {
		c.JSON(http.StatusOK, gin.H{"keys": []licensing.JWK{}})
		return
	}

	// Short CDN cache: long enough to absorb the licensing service's fetch fan-out, short
	// enough that a rotated key propagates within the hour.
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{"keys": h.signer.JWKS()})
}
