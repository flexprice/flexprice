//go:build !ee

package main

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
)

func TestCommunityBuild_NoEEContributions(t *testing.T) {
	if got := temporal.EEContributorCount(); got != 0 {
		t.Errorf("community build must have 0 temporal EE contributors, got %d", got)
	}
	if got := api.EERouteRegistrarCount(); got != 0 {
		t.Errorf("community build must have 0 EE route registrars, got %d", got)
	}
	if got := types.EESettingCount(); got != 0 {
		t.Errorf("community build must have 0 EE settings, got %d", got)
	}
}
