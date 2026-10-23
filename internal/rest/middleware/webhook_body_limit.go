package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const MaxWebhookBodyBytes = 5 << 20

// WebhookBodyLimitMiddleware rejects oversized webhook bodies with 413.
func WebhookBodyLimitMiddleware(limitBytes int64) gin.HandlerFunc {
	capBody := BodyLimitMiddleware(limitBytes)
	return func(c *gin.Context) {
		if c.Request.ContentLength > limitBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "request body too large",
			})
			return
		}
		capBody(c)
	}
}
