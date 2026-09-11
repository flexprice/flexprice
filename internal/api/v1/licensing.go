package v1

import (
	"net/http"
	"strings"
	"time"

	domainUser "github.com/flexprice/flexprice/internal/domain/user"
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

// LicensingHandler mints Heimdall licensing tokens and publishes the backend's
// public key as a JWKS. See internal/licensing for the signing logic.
type LicensingHandler struct {
	signer   *licensing.Signer
	userRepo domainUser.Repository
	log      *logger.Logger
}

func NewLicensingHandler(signer *licensing.Signer, userRepo domainUser.Repository, log *logger.Logger) *LicensingHandler {
	return &LicensingHandler{signer: signer, userRepo: userRepo, log: log}
}

type licensingTokenResponse struct {
	Token string `json:"token"`
}

// @Summary Mint a Heimdall licensing token
// @Description Issues a short-lived EdDSA-signed token, capped at the caller's session expiry, for the browser to present to the central Heimdall licensing service.
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

	sessionExp, err := sessionExpiryFromRequest(c)
	if err != nil {
		c.Error(err)
		return
	}

	// is_admin means Flexprice STAFF (us), not tenant owner — every user is
	// super-admin of their own tenant, so that check is wrong here. Gate on a
	// @flexprice.io email instead.
	// SECURITY: trustworthy only when the email is IdP-verified (Supabase/SSO).
	// Under the self-serve flexprice provider email is unverified and therefore
	// spoofable — do not rely on this for staff auth in that mode.
	isAdmin := false
	if u, err := h.userRepo.GetByID(ctx, types.GetUserID(ctx)); err == nil && u != nil {
		isAdmin = strings.HasSuffix(strings.ToLower(u.Email), staffEmailDomain)
	}

	token, err := h.signer.Mint(tenantID, isAdmin, sessionExp)
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
// @Description Public JWKS of the backend's Ed25519 signing keys, for Heimdall to verify licensing tokens.
// @Tags Licensing
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /.well-known/licensing-jwks.json [get]
func (h *LicensingHandler) JWKS(c *gin.Context) {
	if h.signer == nil {
		c.JSON(http.StatusOK, gin.H{"keys": []licensing.JWK{}})
		return
	}

	// Short CDN cache: long enough to absorb Heimdall's fetch fan-out, short
	// enough that a rotated key propagates within the hour.
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, gin.H{"keys": h.signer.JWKS()})
}
