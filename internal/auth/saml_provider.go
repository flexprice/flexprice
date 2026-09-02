package auth

import "github.com/flexprice/flexprice/internal/config"

// samlProviderFactory builds the SAML provider.
//
// The implementation lives in internal/ee/auth/saml, which imports this package
// for the Provider interface — so this package cannot import it back. The
// enterprise package registers itself from an init() instead, which breaks the
// cycle without a registry.
//
// Nil until that package is linked in, in which case auth.provider=saml falls
// through to the default provider.
var samlProviderFactory func(cfg *config.Configuration) Provider

// RegisterSAMLProvider is called from internal/ee/auth/saml at init time.
func RegisterSAMLProvider(factory func(cfg *config.Configuration) Provider) {
	samlProviderFactory = factory
}
