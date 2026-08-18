package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

var errNotFound = errors.New("credential not found")

// Store mirrors keyring.Store. It is redeclared here so that config does not
// import keyring, keeping the dependency one-directional and the tests stubbable.
type Store interface {
	Set(profile, key string) error
	Get(profile string) (string, error)
	Delete(profile string) error
	Name() string
}

// Overrides carries per-invocation flags. Regions maps a region key to its base
// URL and comes from the embedded spec's servers[].
type Overrides struct {
	Profile string
	APIKey  string
	BaseURL string
	Region  string
	Regions map[string]string
}

// RuntimeContext is everything a command needs to build a client.
type RuntimeContext struct {
	ProfileName string
	Profile     Profile
	APIKey      string
	BaseURL     string
}

// ResolveContext applies credential precedence: flag, environment variable,
// keyring, config file. Design doc §6.
func ResolveContext(cfg *Config, store Store, o Overrides) (RuntimeContext, error) {
	var rc RuntimeContext

	name, profile, profileErr := cfg.Resolve(o.Profile)
	if profileErr == nil {
		rc.ProfileName = name
		rc.Profile = profile
		rc.BaseURL = profile.BaseURL
	}

	switch {
	case o.BaseURL != "":
		rc.BaseURL = o.BaseURL
	case o.Region != "":
		url, ok := o.Regions[o.Region]
		if !ok {
			return rc, fmt.Errorf("unknown region %q (available: %s) — run `flexprice login` to see the available regions", o.Region, availableRegions(o.Regions))
		}
		rc.BaseURL = url
	}

	switch {
	case o.APIKey != "":
		rc.APIKey = o.APIKey
	case os.Getenv("FLEXPRICE_API_KEY") != "":
		rc.APIKey = os.Getenv("FLEXPRICE_API_KEY")
	case rc.ProfileName != "":
		key, err := store.Get(rc.ProfileName)
		if err != nil {
			return rc, fmt.Errorf("no stored key for profile %q — run: flexprice login --profile %s", rc.ProfileName, rc.ProfileName)
		}
		rc.APIKey = key
	}

	if rc.APIKey == "" {
		return rc, fmt.Errorf("not authenticated — run: flexprice init")
	}
	if rc.BaseURL == "" {
		return rc, fmt.Errorf(
			"a key alone does not identify a region — pass --region (us, in) or --base-url,\n" +
				"or run `flexprice login` to store a profile")
	}
	return rc, nil
}

// availableRegions renders the known region keys for an error message, or a
// placeholder when the caller passed no region map at all (e.g. a spec that
// failed to load) so the message never degrades to an empty list.
func availableRegions(regions map[string]string) string {
	if len(regions) == 0 {
		return "none configured"
	}
	names := make([]string, 0, len(regions))
	for name := range regions {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
