//go:build ee

package saml

import "github.com/flexprice/flexprice/internal/api"

func init() {
	api.RegisterEERoutes(func(p api.EERouteParams) {
		// Preserve develop's gate: mount only when the deployment offers SAML,
		// so endpoints 404 as though the feature did not exist when disabled.
		if !p.Config.Auth.SAML.Enabled {
			return
		}
		h := NewHandler(p.Config, p.ServiceParams, p.Logger)
		g := p.Public.Group("/auth/saml/:tenant")
		g.GET("/metadata", h.Metadata)
		g.GET("/login", h.Login)
		g.POST("/acs", h.ACS)
	})
}
