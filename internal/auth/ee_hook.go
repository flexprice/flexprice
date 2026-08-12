package auth

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
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
// An enterprise build may register a name the community build already owns —
// `flexprice` or `supabase` — when the enterprise behaviour is a variation of
// the built-in rather than a new provider. That is a full replacement, not a
// wrapper: the built-in constructor is not called unless the enterprise
// factory calls it. Because swapping the provider behind an unchanged
// auth.provider value is invisible from configuration, every override is
// announced by NewProvider at startup.
//
// Registering the same name twice within a single enterprise build is a
// programming error and panics — the second registration would silently win.
func RegisterEEProvider(name types.AuthProvider, factory ProviderFactory) {
	// An empty name would be selected whenever auth.provider is unset, silently
	// replacing the documented flexprice fallback.
	if name == "" {
		panic("ee auth provider must have a non-empty name")
	}
	if _, exists := eeProviders[name]; exists {
		panic("ee auth provider registered twice: " + string(name))
	}
	eeProviders[name] = factory
}

// overridesBuiltin reports whether name is one the community build ships.
func overridesBuiltin(name types.AuthProvider) bool {
	return name == types.AuthProviderFlexprice || name == types.AuthProviderSupabase
}

// lookupEEProvider resolves an EE-contributed provider. Always returns false in
// a community build, so NewProvider falls through to its own switch unchanged.
//
// An override of a built-in name is logged once per resolution. NewProvider is
// called at startup and per auth middleware construction, not per request, so
// this does not become log noise.
func lookupEEProvider(name types.AuthProvider, cfg *config.Configuration) (Provider, bool) {
	factory, ok := eeProviders[name]
	if !ok {
		return nil, false
	}

	if overridesBuiltin(name) {
		if log, err := logger.NewLogger(cfg); err == nil {
			log.Info(context.Background(),
				"auth provider overridden by enterprise build",
				"provider", string(name),
			)
		}
	}
	return factory(cfg), true
}

// EEProviderCount reports how many enterprise auth providers are registered.
// See temporal.EEContributorCount for why this is exported.
func EEProviderCount() int { return len(eeProviders) }
