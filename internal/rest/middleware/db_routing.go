package middleware

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// DBWriterPinMiddleware installs a writer pin on every request context.
// The first postgres write anywhere in the request flips the pin, routing all
// subsequent reads in the same request to the writer endpoint. This guarantees
// read-after-write consistency within a request even when reads and writes
// happen outside a transaction, while pure-read requests keep using the
// read replica.
func DBWriterPinMiddleware(c *gin.Context) {
	c.Request = c.Request.WithContext(types.WithWriterPinning(c.Request.Context()))
	c.Next()
}
