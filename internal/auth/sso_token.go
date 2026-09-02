package auth

import (
	"context"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"github.com/flexprice/flexprice/internal/config"
	domainauth "github.com/flexprice/flexprice/internal/domain/auth"
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ssoTokenClaim marks a token as minted by the SSO flow.
//
// This claim decides nothing on its own. It is checked AFTER the signature and
// only on a deployment that has SAML switched on, so it distinguishes an SSO
// token from an ordinary one — it never grants trust by itself. Treating a claim
// as authority would be the mistake this design exists to avoid: claims travel
// inside the token, so anyone able to mint one could set it.
const ssoTokenClaim = "flexprice_sso"

// SSO tokens carry the flexprice claim schema — user_id and tenant_id — rather
// than the Supabase one, because the user they name exists only in our own
// database. An identity provider assertion creates a Flexprice user; there is no
// corresponding Supabase account for Supabase to have issued a token for.
const (
	ssoClaimTenantID      = "tenant_id"
	ssoClaimUserID        = "user_id"
	ssoClaimEnvironmentID = "environment_id"
)

// SSOTokenIssuer mints the token handed to the browser after an assertion is
// accepted.
type SSOTokenIssuer struct {
	secret string
}

func NewSSOTokenIssuer(cfg *config.Configuration) *SSOTokenIssuer {
	return &SSOTokenIssuer{secret: cfg.Auth.Secret}
}

// Issue mints a token for a user an identity provider has just authenticated.
//
// Both identities are required. The middleware scopes every request by the
// tenant and user a token names, so a token naming neither would widen that
// scope rather than narrow it — better to fail at the point of minting than to
// hand out a token whose scope is undefined.
func (i *SSOTokenIssuer) Issue(tenantID, userID string, expiryHours int) (string, time.Time, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", time.Time{}, ierr.NewError("tenantID is required").
			WithHint("A SSO token must name the tenant it belongs to").
			Mark(ierr.ErrValidation)
	}
	if strings.TrimSpace(userID) == "" {
		return "", time.Time{}, ierr.NewError("userID is required").
			WithHint("A SSO token must name the user it belongs to").
			Mark(ierr.ErrValidation)
	}
	// A non-positive expiry mints a token that is already expired while still
	// returning a future expiresAt to the caller, so the login appears to
	// succeed and fails as a 401 on the first dashboard request — the same
	// mint-succeeds-verify-fails shape this type exists to prevent.
	if expiryHours <= 0 {
		return "", time.Time{}, ierr.NewError("expiryHours must be positive").
			WithHint("A SSO token must expire in the future").
			Mark(ierr.ErrValidation)
	}

	// One clock reading for both claims: taken separately, iat can land after
	// exp for a sufficiently small expiry.
	now := time.Now()
	expiresAt := now.Add(time.Duration(expiryHours) * time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		ssoClaimTenantID: tenantID,
		ssoClaimUserID:   userID,
		ssoTokenClaim:    true,
		"exp":            expiresAt.Unix(),
		"iat":            now.Unix(),
	})

	signed, err := token.SignedString([]byte(i.secret))
	if err != nil {
		return "", time.Time{}, ierr.WithError(err).
			WithHint("Failed to sign the SSO token").
			Mark(ierr.ErrSystem)
	}
	return signed, expiresAt, nil
}

// SSOTokenValidator accepts tokens minted by SSOTokenIssuer.
//
// It exists because the provider that handles password login is not necessarily
// the one that can validate an SSO token. A deployment using Supabase for
// passwords validates those tokens against the Supabase claim schema (`sub`,
// `app_metadata.tenant_id`); a SAML login mints Flexprice claims for a user that
// has no Supabase account at all, so that validator rejected them and every
// request after a successful SSO login came back 401.
type SSOTokenValidator struct {
	secret      string
	samlEnabled bool
}

func NewSSOTokenValidator(cfg *config.Configuration) *SSOTokenValidator {
	return &SSOTokenValidator{
		secret:      cfg.Auth.Secret,
		samlEnabled: cfg.Auth.SAML.Enabled,
	}
}

// Validate accepts only a token this deployment's SSO flow could have minted.
//
// Three conditions, in this order, none of which the caller controls:
//
//  1. The deployment offers SAML. A deployment with SSO switched off accepts no
//     SSO token at all, so turning SAML on is the only thing that widens what
//     the API accepts — a deployment that does not use SSO is unaffected by any
//     of this.
//  2. The signature verifies against auth.secret, with the algorithm pinned to
//     HMAC. Pinning matters: `alg=none` and an RSA-public-key-as-HMAC-secret
//     confusion are both standard JWT forgeries.
//  3. The token carries the SSO marker. Both providers sign with the same
//     secret, so without this check the SSO path would become a second way to
//     validate any token signed with it — including a session or checkout token
//     never meant to authenticate a dashboard request.
//
// A token that passes still authenticates nothing by itself: the middleware
// loads the named user, refuses one that is archived or missing, and takes the
// session's roles from that record rather than from any claim. So a token can
// name an identity but never grant itself privileges.
func (v *SSOTokenValidator) Validate(_ context.Context, tokenString string) (*domainauth.Claims, error) {
	if !v.samlEnabled {
		return nil, ierr.NewError("sso is not enabled on this deployment").
			WithHint("Single sign-on is not available").
			Mark(ierr.ErrPermissionDenied)
	}

	parsed, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ierr.NewError("unexpected signing method").
				WithHint("Unexpected signing method").
				Mark(ierr.ErrPermissionDenied)
		}
		return []byte(v.secret), nil
	})
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid token").
			Mark(ierr.ErrPermissionDenied)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		return nil, ierr.NewError("invalid token claims").
			WithHint("Invalid token").
			Mark(ierr.ErrPermissionDenied)
	}

	if marker, ok := claims[ssoTokenClaim].(bool); !ok || !marker {
		return nil, ierr.NewError("token was not issued by the sso flow").
			WithHint("Invalid token").
			Mark(ierr.ErrPermissionDenied)
	}

	tenantID, _ := claims[ssoClaimTenantID].(string)
	userID, _ := claims[ssoClaimUserID].(string)
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(userID) == "" {
		return nil, ierr.NewError("token missing identity claims").
			WithHint("Invalid token").
			Mark(ierr.ErrPermissionDenied)
	}

	environmentID, _ := claims[ssoClaimEnvironmentID].(string)

	return &domainauth.Claims{
		UserID:        userID,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
	}, nil
}

// IsSSOToken reports whether a token claims to have come from the SSO flow.
//
// Used only to decide WHICH validator to run, never whether to trust the token:
// the chosen validator then verifies the signature and re-checks this marker
// itself. Reading an unverified claim is safe for routing precisely because
// being routed to the SSO validator is not an advantage — that validator is
// strictly stricter than the ordinary one, requiring both a valid signature and
// the marker.
func IsSSOToken(tokenString string) bool {
	parser := jwt.Parser{SkipClaimsValidation: true}
	claims := jwt.MapClaims{}
	if _, _, err := parser.ParseUnverified(tokenString, claims); err != nil {
		return false
	}
	marker, ok := claims[ssoTokenClaim].(bool)
	return ok && marker
}
