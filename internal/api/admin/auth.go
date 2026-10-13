package admin

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const secretHeader = "X-Admin-Secret"

// requireSecret gates admin routes with admin.secret (FLEXPRICE_ADMIN_SECRET).
// An empty configured secret fails closed.
func requireSecret(secret string) gin.HandlerFunc {
	secret = strings.TrimSpace(secret)
	return func(c *gin.Context) {
		got := c.GetHeader(secretHeader)
		if secret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
