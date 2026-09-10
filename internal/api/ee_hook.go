package api

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// EERouteParams carries what EE route registrars need.
type EERouteParams struct {
	Config        *config.Configuration
	Logger        *logger.Logger
	ServiceParams service.ServiceParams
	// Public is the unauthenticated /v1 group. EE routes reachable before a
	// session exists (SSO redirects, IdP callbacks) mount here.
	Public *gin.RouterGroup
	// Private is the authenticated /v1 group.
	Private *gin.RouterGroup
}

// EERouteRegistrar mounts routes owned by the ee/ directory.
type EERouteRegistrar func(params EERouteParams)

var eeRouteRegistrars []EERouteRegistrar

// RegisterEERoutes is called from ee-tagged init() functions. Registrars run
// after every community route is mounted, so an EE feature can add paths but
// cannot shadow an existing one (gin panics on duplicates at startup).
func RegisterEERoutes(r EERouteRegistrar) {
	eeRouteRegistrars = append(eeRouteRegistrars, r)
}

// applyEERoutes mounts every EE contribution. No-op in a community build.
func applyEERoutes(params EERouteParams) {
	for _, register := range eeRouteRegistrars {
		register(params)
	}
}

// EERouteRegistrarCount reports how many EE route registrars are mounted.
func EERouteRegistrarCount() int { return len(eeRouteRegistrars) }
