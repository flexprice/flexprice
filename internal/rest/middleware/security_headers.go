package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware sets response hardening headers.
//
// This is a JSON API: no route serves HTML (no c.HTML, no LoadHTMLGlob, no
// static handler, and no Swagger UI is mounted — the router only populates
// swagger.SwaggerInfo for spec generation), and auth is header-based rather
// than cookie-based. Content-Security-Policy and X-Frame-Options therefore
// constrain nothing that exists today; they are set as a guard in case an
// HTML-rendering route is ever added.
//
// X-Content-Type-Options is the one that matters: it stops a browser from
// MIME-sniffing a JSON body into something executable if a response is ever
// reflected somewhere that renders it.
func SecurityHeadersMiddleware(c *gin.Context) {
	h := c.Writer.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
	c.Next()
}
