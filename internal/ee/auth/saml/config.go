package saml

import (
	"fmt"
	"net/url"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
)

// SettingKeySAML is the tenant-level settings key holding a tenant's identity
// provider configuration.
const SettingKeySAML types.SettingKey = "saml_config"

// Config is a tenant's SAML identity provider configuration.
//
// Stored as a tenant-level setting rather than its own table: an identity
// provider is per-organisation, and tenant-level settings are readable without
// an environment in context, which the pre-login endpoints require.
//
// Only the identity provider's public signing certificate is held here, so
// plain JSONB storage is appropriate. An SP private key would need
// internal/security's EncryptionService instead.
type Config struct {
	// Enabled is the tenant's own switch, set by its super admin through the
	// settings API.
	Enabled bool `json:"enabled"`

	// Active is the Flexprice-side approval, and is deliberately not settable
	// through the API — it is flipped directly in the database once the tenant's
	// claim to its identity provider has been checked out of band.
	//
	// It exists because a tenant configuring SSO for itself decides which
	// identity provider Flexprice will trust for its users, and nothing in the
	// settings write proves the tenant controls that provider. Both flags must
	// be on before a login is served, so a tenant can turn its own SSO off but
	// cannot turn it on unilaterally.
	//
	// This stands in for per-domain verification: when email-domain discovery
	// arrives, a verified domain becomes the thing that grants Active
	// automatically.
	Active bool `json:"active"`

	// EnforceSSO refuses password login for this tenant's users, so enabling SSO
	// closes the password path rather than adding a second way in.
	//
	// Super admins stay exempt (see ShouldEnforceSSO): an identity provider
	// outage would otherwise lock the tenant out of its own account with no way
	// back that does not involve direct database access.
	EnforceSSO bool `json:"enforce_sso"`

	// IDPEntityID identifies the identity provider; it must match the Issuer of
	// every assertion we accept.
	IDPEntityID string `json:"idp_entity_id"`
	// IDPSSOUrl is where a user is redirected to authenticate.
	IDPSSOUrl string `json:"idp_sso_url"`
	// IDPCertificate is the PEM-encoded X.509 certificate whose key signs
	// assertions. Assertions signed by anything else are rejected.
	IDPCertificate string `json:"idp_certificate"`

	// EmailAttribute names the assertion attribute carrying the user's email.
	// Empty means use the NameID.
	EmailAttribute string `json:"email_attribute"`

	// DefaultRole is granted to just-in-time provisioned users. An admin adjusts
	// it afterwards.
	DefaultRole string `json:"default_role"`
}

func defaultConfig() map[string]interface{} {
	return map[string]interface{}{
		"enabled":         false,
		"active":          false,
		"enforce_sso":     false,
		"idp_entity_id":   "",
		"idp_sso_url":     "",
		"idp_certificate": "",
		"email_attribute": "",
		"default_role":    string(types.RoleAllReader),
	}
}

// IsLive reports whether this tenant's SSO may actually serve a login.
//
// Both the tenant's own switch and the Flexprice-side approval must be on. Every
// decision that turns on "is SAML usable for this tenant" goes through here, so
// the two flags cannot fall out of step between the login redirect, the
// assertion callback, and password enforcement.
func (c Config) IsLive() bool {
	return c.Enabled && c.Active
}

// ShouldEnforceSSO reports whether password login must be refused for a user
// holding the given roles.
//
// Enforcement only applies once SSO is actually live: a tenant that set the flag
// while still waiting for approval would otherwise lock itself out of a login
// that SAML cannot yet serve.
//
// Super admins are exempt so an identity provider outage leaves a way back in.
// The exemption is narrow by design — it is the same group already trusted to
// configure SAML, and their password is the tenant's recovery path.
func (c Config) ShouldEnforceSSO(roles []string) bool {
	if !c.IsLive() || !c.EnforceSSO {
		return false
	}
	return !lo.Contains(roles, types.RoleSuperAdmin.String())
}

// Validate rejects a configuration that cannot produce a safe login.
//
// The checks are deliberately strict about enablement: a half-configured
// provider that is switched on would fail at assertion time, after the user has
// already authenticated with their identity provider, which is a confusing place
// to discover a typo.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if strings.TrimSpace(c.IDPEntityID) == "" {
		return fmt.Errorf("idp_entity_id is required when SAML is enabled")
	}

	if strings.TrimSpace(c.IDPSSOUrl) == "" {
		return fmt.Errorf("idp_sso_url is required when SAML is enabled")
	}
	u, err := url.Parse(c.IDPSSOUrl)
	if err != nil || u.Host == "" {
		return fmt.Errorf("idp_sso_url must be an absolute URL")
	}
	// An assertion travels through the browser, so a plaintext identity
	// provider exposes it in transit. The sole exemption is a loopback host,
	// which never leaves the machine and is how a local identity provider is
	// run during development — SimpleSAMLphp and friends serve plain http.
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("idp_sso_url must use https (plain http is allowed only for localhost)")
	}

	if strings.TrimSpace(c.IDPCertificate) == "" {
		return fmt.Errorf("idp_certificate is required when SAML is enabled")
	}
	if _, err := parseCertificate(c.IDPCertificate); err != nil {
		return fmt.Errorf("idp_certificate is not a valid PEM-encoded X.509 certificate: %w", err)
	}

	if c.DefaultRole == "" {
		return fmt.Errorf("default_role is required when SAML is enabled")
	}
	if err := validateDefaultRole(c.DefaultRole); err != nil {
		return err
	}

	return nil
}

// validateDefaultRole keeps just-in-time provisioning inside the roles a human
// user may hold, so a configuration change cannot mint a role the RBAC model
// does not recognise.
//
// super_admin is excluded even though a user may hold it. Provisioning happens
// on the strength of an assertion alone, so a default of super_admin would grant
// tenant administration to whoever the identity provider lets through — and to
// anyone who can reach the ACS endpoint with an assertion it accepts. An
// administrator is promoted deliberately afterwards, through the user-roles
// endpoint, which is itself super_admin-only.
func validateDefaultRole(role string) error {
	if role == string(types.RoleSuperAdmin) {
		return fmt.Errorf("default_role %q may not be granted by just-in-time provisioning; promote an administrator explicitly after their first login", role)
	}

	for _, allowed := range types.UserTypeUser.AllowedRoles() {
		if string(allowed) == role {
			return nil
		}
	}
	return fmt.Errorf("default_role %q is not assignable to a user", role)
}

// validateStoredConfig checks a value arriving through the settings API, where
// it is an untyped map rather than a Config. Registered as the key's validator
// so a bad identity provider configuration is rejected at write time instead of
// at login, after the user has already authenticated.
func validateStoredConfig(value map[string]interface{}) error {
	cfg, err := configFromMap(value)
	if err != nil {
		return ierr.WithError(err).
			WithHint("saml_config is not a valid SAML configuration").
			Mark(ierr.ErrValidation)
	}
	if err := cfg.Validate(); err != nil {
		// Marked as a validation failure so a rejected configuration is
		// reported to the caller as a bad request rather than a server fault.
		return ierr.WithError(err).
			WithHint(err.Error()).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// configFromMap decodes the stored JSONB blob into a Config.
func configFromMap(value map[string]interface{}) (Config, error) {
	cfg, err := utils.ToStruct[Config](value)
	if err != nil {
		return Config{}, fmt.Errorf("saml_config is not a valid SAML configuration: %w", err)
	}
	return cfg, nil
}

// isLoopbackHost reports whether the host never leaves this machine.
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
