//go:build !ee

package main

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api"
	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/temporal"
)

// TestCommunityBuildHasNoEECode is the mirror of TestEERegistriesPopulated: a
// community binary must register nothing enterprise. A non-zero count here
// means EE code leaked past the build tag into the AGPL build.
func TestCommunityBuildHasNoEECode(t *testing.T) {
	if got := len(eeOptions()); got != 0 {
		t.Errorf("eeOptions() returned %d options in a community build, want 0", got)
	}

	checks := []struct {
		registry string
		count    int
	}{
		{"temporal contributors", temporal.EEContributorCount()},
		{"auth providers", auth.EEProviderCount()},
		{"api route registrars", api.EERouteRegistrarCount()},
	}

	for _, c := range checks {
		if c.count != 0 {
			t.Errorf("%s has %d entries in a community build, want 0 — "+
				"enterprise code leaked past the build tag", c.registry, c.count)
		}
	}
}
