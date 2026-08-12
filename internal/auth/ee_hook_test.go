package auth

import (
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
)

func withNoEEProviders(t *testing.T) {
	t.Helper()
	original := eeProviders
	eeProviders = map[types.AuthProvider]ProviderFactory{}
	t.Cleanup(func() { eeProviders = original })
}

// TestNewProviderCommunityBuildUnchanged pins the behaviour a community build
// must keep: the two built-ins resolve, and anything else falls back to
// flexprice rather than erroring.
func TestNewProviderCommunityBuildUnchanged(t *testing.T) {
	withNoEEProviders(t)

	cases := []struct {
		configured types.AuthProvider
		want       types.AuthProvider
	}{
		{types.AuthProviderFlexprice, types.AuthProviderFlexprice},
		{types.AuthProviderSupabase, types.AuthProviderSupabase},
		{"external_idp", types.AuthProviderFlexprice},
		{"", types.AuthProviderFlexprice},
	}

	for _, tc := range cases {
		cfg := &config.Configuration{Auth: config.AuthConfig{Provider: tc.configured}}
		if got := NewProvider(cfg).GetProvider(); got != tc.want {
			t.Errorf("auth.provider=%q: got %q, want %q", tc.configured, got, tc.want)
		}
	}
}

// TestEEProviderIsAdditive verifies an EE contribution adds a new provider
// value without disturbing the built-ins.
func TestEEProviderIsAdditive(t *testing.T) {
	withNoEEProviders(t)

	const eeName types.AuthProvider = "external_idp"
	RegisterEEProvider(eeName, func(cfg *config.Configuration) Provider {
		return NewFlexpriceAuth(cfg)
	})

	cfg := &config.Configuration{Auth: config.AuthConfig{Provider: eeName}}
	if _, ok := lookupEEProvider(eeName, cfg); !ok {
		t.Fatal("registered ee provider did not resolve")
	}

	// Built-ins must still take their own path, not the EE one.
	for _, builtin := range []types.AuthProvider{types.AuthProviderFlexprice, types.AuthProviderSupabase} {
		cfg := &config.Configuration{Auth: config.AuthConfig{Provider: builtin}}
		if got := NewProvider(cfg).GetProvider(); got != builtin {
			t.Errorf("ee registration disturbed built-in %q: got %q", builtin, got)
		}
	}
}

// TestEEProviderMayOverrideBuiltins covers the deliberate case where an
// enterprise build supplies a variation of a built-in provider under the same
// auth.provider value — e.g. flexprice password auth plus SSO enforcement.
// The override is announced by NewProvider at startup, since it is invisible
// from configuration alone.
func TestEEProviderMayOverrideBuiltins(t *testing.T) {
	for _, builtin := range []types.AuthProvider{types.AuthProviderFlexprice, types.AuthProviderSupabase} {
		t.Run(string(builtin), func(t *testing.T) {
			withNoEEProviders(t)

			sentinel := &overrideSentinel{name: builtin}
			RegisterEEProvider(builtin, func(cfg *config.Configuration) Provider { return sentinel })

			cfg := &config.Configuration{Auth: config.AuthConfig{Provider: builtin}}
			if got := NewProvider(cfg); got != Provider(sentinel) {
				t.Fatalf("override for %q did not win: NewProvider returned the built-in", builtin)
			}
		})
	}
}

// TestEEProviderRejectsInvalidRegistrations covers the two cases that stay
// programming errors: an empty name, which would be selected whenever
// auth.provider is unset, and a duplicate, where the second registration would
// silently win.
func TestEEProviderRejectsInvalidRegistrations(t *testing.T) {
	noop := func(cfg *config.Configuration) Provider { return nil }

	t.Run("empty name", func(t *testing.T) {
		withNoEEProviders(t)
		defer func() {
			if recover() == nil {
				t.Error("registering an empty provider name should panic")
			}
		}()
		RegisterEEProvider("", noop)
	})

	t.Run("duplicate name", func(t *testing.T) {
		withNoEEProviders(t)
		RegisterEEProvider("external_idp", noop)
		defer func() {
			if recover() == nil {
				t.Error("registering the same provider name twice should panic")
			}
		}()
		RegisterEEProvider("external_idp", noop)
	})
}

// overrideSentinel is a Provider whose identity can be asserted. Only
// GetProvider is meaningful; the rest satisfy the interface.
type overrideSentinel struct {
	Provider
	name types.AuthProvider
}

func (s *overrideSentinel) GetProvider() types.AuthProvider { return s.name }
