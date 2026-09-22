//go:build ee

package ee

import (
	"go.uber.org/fx"

	// Blank imports run the init()s that populate the core extension registries:
	// temporal contributors, EE routes, auth providers.
	_ "github.com/flexprice/flexprice/ee/alerts"
	_ "github.com/flexprice/flexprice/ee/auth/saml"
)

// Module returns the Enterprise Edition fx options. Empty for now — SAML and
// alerts self-register via init(); this is the aggregation point for EE fx
// providers/invokes added in later phases.
func Module() fx.Option {
	return fx.Options()
}
