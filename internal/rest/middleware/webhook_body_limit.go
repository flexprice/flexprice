package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxWebhookBodyBytes is the default webhook body size cap.
const MaxWebhookBodyBytes = 5 << 20

// WebhookBodyLimitMiddleware rejects oversized webhook bodies with 413.
func WebhookBodyLimitMiddleware(limitBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limitBytes {
			abortBodyTooLarge(c)
			return
		}
		if c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		buf, err := io.ReadAll(io.LimitReader(c.Request.Body, limitBytes+1))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		if int64(len(buf)) > limitBytes {
			abortBodyTooLarge(c)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(buf))
		c.Request.ContentLength = int64(len(buf))
		c.Next()
	}
}

func abortBodyTooLarge(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
		"error": "request body too large",
	})
}
