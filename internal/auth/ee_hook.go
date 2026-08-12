package auth

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
)

// ProviderFactory builds an auth provider from configuration.
type ProviderFactory func(cfg *config.Configuration) Provider

// eeProviders holds providers contributed by the ee/ submodule, keyed by the
// value operators put in config as auth.provider. Empty in a community build.
var eeProviders = map[types.AuthProvider]ProviderFactory{}

// RegisterEEProvider is called from ee-tagged init() functions. Registration
// happens at package-init time, before any request is served.
//
// Contributions are additive: an EE provider can only add a new auth.provider
// value. Registering a name the community build already owns is a programming
// error and panics at startup rather than silently shadowing it.
func RegisterEEProvider(name types.AuthProvider, factory ProviderFactory) {
	if name == types.AuthProviderFlexprice || name == types.AuthProviderSupabase {
		panic("ee auth provider may not override the built-in provider: " + string(name))
	}
	if _, exists := eeProviders[name]; exists {
		panic("ee auth provider registered twice: " + string(name))
	}
	eeProviders[name] = factory
}

// lookupEEProvider resolves an EE-contributed provider. Always returns false in
// a community build, so NewProvider falls through to its own switch unchanged.
func lookupEEProvider(name types.AuthProvider, cfg *config.Configuration) (Provider, bool) {
	factory, ok := eeProviders[name]
	if !ok {
		return nil, false
	}
	return factory(cfg), true
}

// EEProviderCount reports how many enterprise auth providers registered.
// See temporal.EEContributorCount for why this is exported.
func EEProviderCount() int { return len(eeProviders) }
