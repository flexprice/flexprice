package api

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// EERouteParams carries what EE route registrars need. It exists so the hook
// signature does not have to change every time an EE feature needs another
// dependency.
type EERouteParams struct {
	Config *config.Configuration
	Logger *logger.Logger
	// Public is the unauthenticated /v1 group. EE routes that must be reachable
	// before a session exists (SSO redirects, IdP callbacks) mount here.
	Public *gin.RouterGroup
	// Private is the authenticated /v1 group.
	Private *gin.RouterGroup
}

// EERouteRegistrar mounts routes owned by the ee/ submodule.
type EERouteRegistrar func(params EERouteParams)

var eeRouteRegistrars []EERouteRegistrar

// RegisterEERoutes is called from ee-tagged init() functions. Registrars run in
// registration order, after every community route is mounted — so an EE feature
// can add paths but cannot shadow an existing one (gin panics on duplicates,
// which surfaces the conflict at startup rather than in production).
func RegisterEERoutes(r EERouteRegistrar) {
	eeRouteRegistrars = append(eeRouteRegistrars, r)
}

// applyEERoutes mounts every EE contribution. No-op in a community build.
func applyEERoutes(params EERouteParams) {
	for _, register := range eeRouteRegistrars {
		register(params)
	}
}

// EERouteRegistrarCount reports how many enterprise route registrars are
// mounted. See temporal.EEContributorCount for why this is exported.
func EERouteRegistrarCount() int { return len(eeRouteRegistrars) }
