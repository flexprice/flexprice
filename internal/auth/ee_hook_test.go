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
		{"saml", types.AuthProviderFlexprice},
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

	const eeName types.AuthProvider = "saml"
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

// TestEEProviderCannotShadowBuiltins ensures an EE feature cannot silently take
// over an existing auth provider — the failure is loud and at startup.
func TestEEProviderCannotShadowBuiltins(t *testing.T) {
	withNoEEProviders(t)

	for _, builtin := range []types.AuthProvider{types.AuthProviderFlexprice, types.AuthProviderSupabase} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("registering ee provider %q should panic, but did not", builtin)
				}
			}()
			RegisterEEProvider(builtin, func(cfg *config.Configuration) Provider { return nil })
		}()
	}
}
