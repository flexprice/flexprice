package middleware

import (
	"github.com/cockroachdb/errors"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

// Middleware for handling errors
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) > 0 {
			err := c.Errors.Last().Err

			// Create error response
			errResp := gin.H{
				"success": false,
				"error": gin.H{
					"message": errors.UnwrapAll(err).Error(),
				},
			}

			// Get HTTP status
			status := ierr.HttpStatusFromErr(err)
			c.JSON(status, errResp)
		}
	}
}
