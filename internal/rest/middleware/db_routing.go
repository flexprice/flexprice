package middleware

import (
	"fmt"
	"os"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

const debugRoutingRequestHeader = "X-Debug-DB-Routing"

// DBWriterPinMiddleware installs a writer pin and routing stats counter on
// every request context. At request end it logs a one-line routing summary
// and, when the X-Debug-DB-Routing: true request header is present OR the
// FLEXPRICE_DB_ROUTING_DEBUG env var is set, emits X-DB-Routing-* response
// headers for test assertion.
func DBWriterPinMiddleware(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := types.WithWriterPinning(c.Request.Context())
		ctx = types.WithRoutingStats(ctx)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		stats := types.GetRoutingStats(c.Request.Context())
		if stats == nil {
			return
		}

		r := stats.Reader.Load()
		wp := stats.WriterPinned.Load()
		wt := stats.WriterTx.Load()
		wf := stats.WriterForced.Load()
		wc := stats.WriterCalls.Load()

		if log != nil {
			log.Debug(c.Request.Context(), "db_routing_summary",
				"method", c.Request.Method,
				"path", c.FullPath(),
				"reader", r,
				"writer_pinned", wp,
				"writer_tx", wt,
				"writer_forced", wf,
				"writer_calls", wc,
				"request_id", types.GetRequestID(c.Request.Context()),
			)
		}

		debugEnabled := c.GetHeader(debugRoutingRequestHeader) == "true" ||
			os.Getenv("FLEXPRICE_DB_ROUTING_DEBUG") == "true"
		if debugEnabled {
			c.Header("X-DB-Routing-Reader", fmt.Sprintf("%d", r))
			c.Header("X-DB-Routing-Writer-Pinned", fmt.Sprintf("%d", wp))
			c.Header("X-DB-Routing-Writer-Tx", fmt.Sprintf("%d", wt))
			c.Header("X-DB-Routing-Writer-Forced", fmt.Sprintf("%d", wf))
			c.Header("X-DB-Routing-Writer-Calls", fmt.Sprintf("%d", wc))
		}
	}
}
