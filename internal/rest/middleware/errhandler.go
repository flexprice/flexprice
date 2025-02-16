package middleware

import (
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

func ErrHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) > 0 {
			err := c.Errors.Last().Err
			var ierr *errors.InternalError

			if e, ok := err.(*errors.InternalError); ok {
				ierr = e
			} else {
				// Handle unknown errors as internal errors
				ierr = errors.New(errors.ErrCodeSystemError, "something went wrong")
			}

			c.JSON(errors.GetHTTPStatusCode(ierr.Code), gin.H{
				"message": errors.ErrorMessage(err),
				"code":    errors.ErrorCode(err),
				"detail":  err.Error(),
			})
		}
	}
}
