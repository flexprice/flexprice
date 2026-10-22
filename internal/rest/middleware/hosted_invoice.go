package middleware

import (
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// HostedInvoiceTokenMiddleware authenticates public hosted invoice requests by their token and
// scopes the request to the token's invoice. Rejections go to ErrorHandler, mounted before it.
func HostedInvoiceTokenMiddleware(hostedInvoiceService service.HostedInvoiceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Robots-Tag", "noindex")

		ctx, err := hostedInvoiceService.Resolve(c.Request.Context(), c.GetHeader(types.HeaderSessionToken))
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}

		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
