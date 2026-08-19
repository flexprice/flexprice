package saml

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/crewjam/saml"

	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// assertionResult is what a validated assertion yields.
type assertionResult struct {
	Email string
}

// validateAssertion parses and verifies a SAMLResponse.
//
// crewjam/saml's ParseResponse performs the checks that matter, and each is here
// for a specific published attack:
//
//   - signature verified against the configured certificate only, never one
//     carried inside the response
//   - XML signature wrapping detection, so the signature must cover the
//     assertion actually consumed
//   - audience restriction, so an assertion minted for a different service
//     provider is rejected
//   - NotBefore / NotOnOrAfter condition window
//   - InResponseTo matched against request IDs we issued
//   - XML entity expansion disabled, closing XXE
//
// possibleRequestIDs carries the AuthnRequest IDs this browser session started.
// An empty list means an unsolicited (identity-provider-initiated) response,
// which crewjam rejects unless explicitly permitted — we do not permit it, so a
// response must always answer a request we made.
func validateAssertion(
	sp *saml.ServiceProvider,
	req *http.Request,
	possibleRequestIDs []string,
	cfg Config,
) (*assertionResult, error) {
	assertion, err := sp.ParseResponse(req, possibleRequestIDs)
	if err != nil {
		// crewjam reports the real cause — expired condition window, audience
		// mismatch, unknown InResponseTo, bad signature — on PrivateErr, and its
		// Error() deliberately says nothing useful so it is safe to return to a
		// caller. Keep the response generic, but carry the detail so an operator
		// integrating an identity provider can see which check failed; without
		// it every failure is indistinguishable and the only recourse is
		// guessing.
		cause := err
		var invalid *saml.InvalidResponseError
		if errors.As(err, &invalid) && invalid.PrivateErr != nil {
			cause = invalid.PrivateErr
		}
		return nil, ierr.WithError(cause).
			WithHint("The identity provider's response could not be validated").
			Mark(ierr.ErrPermissionDenied)
	}

	if assertion.Issuer.Value != cfg.IDPEntityID {
		return nil, ierr.NewError("saml assertion issuer mismatch").
			WithHint("The response came from a different identity provider than configured").
			Mark(ierr.ErrPermissionDenied)
	}

	email, err := extractEmail(assertion, cfg)
	if err != nil {
		return nil, err
	}

	return &assertionResult{Email: email}, nil
}

// extractEmail reads the user's email from the configured attribute, or the
// NameID when no attribute is configured.
//
// An assertion that authenticates someone we cannot identify is useless and must
// not fall back to a guess — a wrong email here would log the user into the
// wrong account.
func extractEmail(assertion *saml.Assertion, cfg Config) (string, error) {
	if attr := strings.TrimSpace(cfg.EmailAttribute); attr != "" {
		for _, stmt := range assertion.AttributeStatements {
			for _, a := range stmt.Attributes {
				if a.Name != attr && a.FriendlyName != attr {
					continue
				}
				for _, v := range a.Values {
					if value := strings.TrimSpace(v.Value); value != "" {
						// Checked here as well as on the NameID path. An
						// identity provider can map the configured attribute to
						// a username or an employee number, and users.email is
						// globally unique — provisioning one of those writes a
						// permanent row that blocks the real address later.
						if !strings.Contains(value, "@") {
							return "", ierr.NewErrorf("assertion %s attribute is not an email address", attr).
								WithHint("The identity provider sent something other than an email address in the configured attribute").
								Mark(ierr.ErrValidation)
						}
						return normaliseEmail(value), nil
					}
				}
			}
		}
		return "", ierr.NewErrorf("assertion has no %s attribute", attr).
			WithHint("The identity provider did not send the configured email attribute").
			Mark(ierr.ErrValidation)
	}

	if assertion.Subject == nil || assertion.Subject.NameID == nil {
		return "", ierr.NewError("assertion has no NameID").
			WithHint("Configure an email attribute, or have the identity provider send the email as NameID").
			Mark(ierr.ErrValidation)
	}

	value := strings.TrimSpace(assertion.Subject.NameID.Value)
	if value == "" || !strings.Contains(value, "@") {
		return "", ierr.NewError("assertion NameID is not an email address").
			WithHint("Configure an email attribute, or set the identity provider's NameID format to emailAddress").
			Mark(ierr.ErrValidation)
	}

	return normaliseEmail(value), nil
}

// normaliseEmail lowercases so identity-provider casing cannot create a second
// account for a user who already exists.
func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// resolveUser finds the user an assertion authenticates, provisioning one if the
// tenant has never seen them.
//
// The cross-tenant check is the important branch. Email is globally unique
// (a partial unique index on users.email that is not scoped by tenant), so a
// user belongs to exactly one tenant for the lifetime of the account. An
// assertion validated for tenant A must never authenticate a user who belongs to
// tenant B, and the failure has to be explicit rather than a silent login into
// the wrong tenant.
func (p *samlProvider) resolveUser(ctx context.Context, params service.ServiceParams, tenantID, email string, cfg Config) (string, error) {
	// Look the user up without a tenant in context. GetByEmail filters by tenant
	// when one is set, which would hide a user belonging to another
	// organisation — the caller would then fall through to provisioning and hit
	// the global unique index on users.email as an opaque database error rather
	// than the explicit refusal below.
	existing, err := params.UserRepo.GetByEmail(types.SetTenantID(ctx, ""), email)
	if err == nil && existing != nil {
		if existing.TenantID != tenantID {
			return "", ierr.NewError("user belongs to a different organisation").
				WithHint("This email already has a Flexprice account under another organisation").
				Mark(ierr.ErrPermissionDenied)
		}
		return existing.ID, nil
	}
	if err != nil && !ierr.IsNotFound(err) {
		return "", err
	}

	return p.provisionUser(ctx, params, tenantID, email, cfg)
}

func (p *samlProvider) provisionUser(ctx context.Context, params service.ServiceParams, tenantID, email string, cfg Config) (string, error) {
	role := cfg.DefaultRole
	if role == "" {
		role = string(types.RoleAllReader)
	}
	if err := validateDefaultRole(role); err != nil {
		return "", ierr.WithError(err).
			WithHint("The tenant's SAML default_role is not assignable").
			Mark(ierr.ErrValidation)
	}

	// Provisioning is deliberately not routed through InviteUser: that path
	// requires a super_admin in context to authorise the invite, and an
	// assertion has no acting user. The identity provider is the authority here.
	ctx = types.SetTenantID(ctx, tenantID)
	newUser := user.NewUser(email, tenantID)
	newUser.Roles = []string{role}

	if err := params.UserRepo.Create(ctx, newUser); err != nil {
		return "", fmt.Errorf("provision saml user: %w", err)
	}
	return newUser.ID, nil
}
