package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeadersMiddleware sets response hardening headers.
//
// This is a JSON API: it serves no HTML and uses no cookie-based auth, so
// Content-Security-Policy and X-Frame-Options carry little weight here. They
// are set anyway because they cost nothing and the API host also answers
// /swagger and /health, which a browser may render.
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
