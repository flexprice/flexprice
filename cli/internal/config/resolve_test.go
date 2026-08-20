package config

import (
	"strings"
	"testing"
)

type stubStore struct {
	keys map[string]string
}

func (s *stubStore) Set(p, k string) error { s.keys[p] = k; return nil }
func (s *stubStore) Get(p string) (string, error) {
	k, ok := s.keys[p]
	if !ok {
		return "", errNotFound
	}
	return k, nil
}
func (s *stubStore) Delete(p string) error { delete(s.keys, p); return nil }
func (s *stubStore) Name() string          { return "stub" }

func baseConfig() *Config {
	return &Config{
		DefaultProfile: "acme-production",
		Profiles: map[string]Profile{
			"acme-production": {Region: "us", BaseURL: "https://us.example/v1", Label: "prod"},
			"acme-dev":        {Region: "us", BaseURL: "https://us.example/v1", Label: "dev"},
		},
	}
}

func TestResolveContext_FlagBeatsKeyring(t *testing.T) {
	store := &stubStore{keys: map[string]string{"acme-production": "sk_from_keyring"}}
	rc, err := ResolveContext(baseConfig(), store, Overrides{APIKey: "sk_from_flag"})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.APIKey != "sk_from_flag" {
		t.Errorf("APIKey = %q, want the flag value", rc.APIKey)
	}
}

func TestResolveContext_EnvBeatsKeyring(t *testing.T) {
	store := &stubStore{keys: map[string]string{"acme-production": "sk_from_keyring"}}
	t.Setenv("FLEXPRICE_API_KEY", "sk_from_env")
	rc, err := ResolveContext(baseConfig(), store, Overrides{})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.APIKey != "sk_from_env" {
		t.Errorf("APIKey = %q, want the env value", rc.APIKey)
	}
}

func TestResolveContext_ProfileOverrideSelectsThatProfile(t *testing.T) {
	store := &stubStore{keys: map[string]string{"acme-dev": "sk_dev"}}
	rc, err := ResolveContext(baseConfig(), store, Overrides{Profile: "acme-dev"})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.ProfileName != "acme-dev" || rc.Profile.Label != "dev" {
		t.Errorf("profile = %q %+v, want acme-dev", rc.ProfileName, rc.Profile)
	}
}

// A bare key carries no region, so guessing a base URL would produce a 401 that
// looks like an invalid key. Design doc §6.
func TestResolveContext_BareAPIKeyWithoutProfileIsAnError(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	_, err := ResolveContext(empty, store, Overrides{APIKey: "sk_loose"})
	if err == nil {
		t.Fatal("want an error for --api-key with no region or base URL")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("error = %q, want it to name --region", err)
	}
}

func TestResolveContext_APIKeyWithRegionResolvesBaseURL(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	rc, err := ResolveContext(empty, store, Overrides{
		APIKey:  "sk_loose",
		Region:  "us",
		Regions: map[string]string{"us": "https://us.example/v1"},
	})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.BaseURL != "https://us.example/v1" {
		t.Errorf("BaseURL = %q, want the US region URL", rc.BaseURL)
	}
}

// No overrides at all: the default profile and its keyring entry must be
// enough on their own, since this is the everyday no-flags invocation.
func TestResolveContext_DefaultPrecedence_UsesProfileAndKeyring(t *testing.T) {
	store := &stubStore{keys: map[string]string{"acme-production": "sk_from_keyring"}}
	rc, err := ResolveContext(baseConfig(), store, Overrides{})
	if err != nil {
		t.Fatalf("ResolveContext: %v", err)
	}
	if rc.APIKey != "sk_from_keyring" {
		t.Errorf("APIKey = %q, want the keyring value", rc.APIKey)
	}
	if rc.BaseURL != "https://us.example/v1" {
		t.Errorf("BaseURL = %q, want the profile's base URL", rc.BaseURL)
	}
	if rc.ProfileName != "acme-production" {
		t.Errorf("ProfileName = %q, want the default profile", rc.ProfileName)
	}
}

// The profile resolves but nothing was ever stored for it in the keyring;
// the error must name the profile and the fix.
func TestResolveContext_ProfileExistsButKeyringEmpty(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	_, err := ResolveContext(baseConfig(), store, Overrides{})
	if err == nil {
		t.Fatal("want an error when the keyring has no key for the resolved profile")
	}
	if !strings.Contains(err.Error(), "acme-production") {
		t.Errorf("error = %q, want it to name the profile", err)
	}
	if !strings.Contains(err.Error(), "flexprice login") {
		t.Errorf("error = %q, want it to name the fix", err)
	}
}

// No default profile configured and nothing supplied on the command line:
// there is no profile to fall back to and no key from any other source.
func TestResolveContext_NoProfileNoFlagNoEnv_ErrorNamesInit(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	_, err := ResolveContext(empty, store, Overrides{})
	if err == nil {
		t.Fatal("want an error when nothing identifies a key")
	}
	if !strings.Contains(err.Error(), "flexprice init") {
		t.Errorf("error = %q, want it to name the fix", err)
	}
}

// A key with no profile and no --region/--base-url must fail the same way a
// bare --api-key does, not silently guess a region.
func TestResolveContext_EnvKeySetButNoProfileOrRegion_ErrorNamesRegion(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	t.Setenv("FLEXPRICE_API_KEY", "sk_from_env")
	_, err := ResolveContext(empty, store, Overrides{})
	if err == nil {
		t.Fatal("want an error when the region is still ambiguous")
	}
	if !strings.Contains(err.Error(), "--region") {
		t.Errorf("error = %q, want it to name --region", err)
	}
}

// --region is passed but doesn't match any key in the caller-supplied
// Regions map: the error must list the valid options, not just complain.
func TestResolveContext_UnknownRegion_ListsAvailableRegions(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	_, err := ResolveContext(empty, store, Overrides{
		APIKey:  "sk_loose",
		Region:  "eu",
		Regions: map[string]string{"us": "https://us.example/v1", "in": "https://in.example/v1"},
	})
	if err == nil {
		t.Fatal("want an error for an unrecognized region")
	}
	if !strings.Contains(err.Error(), "us") || !strings.Contains(err.Error(), "in") {
		t.Errorf("error = %q, want it to list the available regions", err)
	}
}

// A nil Regions map (e.g. spec discovery failed) must say so plainly, not
// print an empty list.
func TestResolveContext_RegionGivenButRegionsMapNil(t *testing.T) {
	store := &stubStore{keys: map[string]string{}}
	empty := &Config{Profiles: map[string]Profile{}}
	_, err := ResolveContext(empty, store, Overrides{APIKey: "sk_loose", Region: "us"})
	if err == nil {
		t.Fatal("want an error when Regions is nil")
	}
	if !strings.Contains(err.Error(), "none configured") {
		t.Errorf("error = %q, want it to say no regions are configured", err)
	}
}

// The failure paths must never interpolate the actual API key value into an
// error string — that would land the secret in logs and terminal scrollback.
func TestResolveContext_ErrorsNeverLeakAPIKeyValue(t *testing.T) {
	const secret = "sk_super_secret_value"
	store := &stubStore{keys: map[string]string{}}

	cases := []struct {
		name string
		cfg  *Config
		o    Overrides
	}{
		{"bare key no region", &Config{Profiles: map[string]Profile{}}, Overrides{APIKey: secret}},
		{"unknown region", &Config{Profiles: map[string]Profile{}}, Overrides{APIKey: secret, Region: "eu", Regions: map[string]string{"us": "https://us.example/v1"}}},
		{"profile keyring empty", baseConfig(), Overrides{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveContext(tc.cfg, store, tc.o)
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error = %q, leaks the API key value", err)
			}
		})
	}
}
