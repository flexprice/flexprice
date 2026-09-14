//go:build ee

package main

import (
	"testing"

	"github.com/flexprice/flexprice/internal/api"
	"github.com/flexprice/flexprice/internal/temporal"

	_ "github.com/flexprice/flexprice/ee" // link EE init()s
)

func TestEEBuild_RegistersContributions(t *testing.T) {
	if temporal.EEContributorCount() < 1 {
		t.Error("ee build must register the alert temporal contributor")
	}
	if api.EERouteRegistrarCount() < 1 {
		t.Error("ee build must register the SAML route registrar")
	}
}
