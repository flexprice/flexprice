package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

const (
	// debugRoutingRequestHeader — client sends this to get X-DB-Routing-* response headers.
	debugRoutingRequestHeader = "X-Debug-DB-Routing"

	// pinToWriterRequestHeader — client sends this to force the current request's
	// reads to route to the writer (cross-request read-after-write consistency).
	pinToWriterRequestHeader = "X-Pin-To-Writer"

	// writerPinnedUntilHeader — server sends this when a write occurred, telling
	// the client to pin subsequent reads to the writer until the given epoch-ms.
	writerPinnedUntilHeader = "X-Writer-Pinned-Until"

	// writerPinWindowMS is the suggested pin duration after a write (ms).
	// 10 s is a generous ceiling for replica lag in most Aurora setups.
	writerPinWindowMS = 10_000
)

// DBWriterPinMiddleware installs a writer pin and routing stats counter on
// every request context. At request end it:
//   - logs a one-line routing summary
//   - when X-Debug-DB-Routing: true OR FLEXPRICE_DB_ROUTING_DEBUG=true, emits
//     X-DB-Routing-* response headers for test assertion
//   - when any write occurred, emits X-Writer-Pinned-Until: <epoch-ms> so the
//     client can pin subsequent reads to the writer for cross-request consistency
//
// Cross-request read-after-write: when the client sends X-Pin-To-Writer: true,
// the request context is pre-pinned so every Reader() call routes to the writer
// and increments the writer_pinned counter (same code-path as within-request pin).
func DBWriterPinMiddleware(log *logger.Logger) gin.HandlerFunc {
	debugEnabledEnv := os.Getenv("FLEXPRICE_DB_ROUTING_DEBUG") == "true"
	return func(c *gin.Context) {
		ctx := types.WithWriterPinning(c.Request.Context())
		ctx = types.WithRoutingStats(ctx)

		// Honor cross-request writer pin sent by the client. Calling PinWriter
		// before the handlers run makes IsWriterPinned(ctx)==true for the entire
		// request, so every Reader() call routes to the writer and increments
		// WriterPinned — identical behaviour to a within-request pin.
		if c.GetHeader(pinToWriterRequestHeader) == "true" {
			types.PinWriter(ctx)
		}

		c.Request = c.Request.WithContext(ctx)

		debugEnabled := c.GetHeader(debugRoutingRequestHeader) == "true" || debugEnabledEnv

		var bw *bufferedResponseWriter
		if debugEnabled {
			bw = newBufferedResponseWriter(c.Writer)
			c.Writer = bw
		}

		c.Next()

		stats := types.GetRoutingStats(c.Request.Context())
		if stats == nil {
			if bw != nil {
				bw.flush()
			}
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

		if debugEnabled && bw != nil {
			// Inject routing counters BEFORE flushing the buffered body so the
			// underlying ResponseWriter hasn't committed headers yet.
			bw.ResponseWriter.Header().Set("X-DB-Routing-Reader", fmt.Sprintf("%d", r))
			bw.ResponseWriter.Header().Set("X-DB-Routing-Writer-Pinned", fmt.Sprintf("%d", wp))
			bw.ResponseWriter.Header().Set("X-DB-Routing-Writer-Tx", fmt.Sprintf("%d", wt))
			bw.ResponseWriter.Header().Set("X-DB-Routing-Writer-Forced", fmt.Sprintf("%d", wf))
			bw.ResponseWriter.Header().Set("X-DB-Routing-Writer-Calls", fmt.Sprintf("%d", wc))

			// Advise the client how long to pin subsequent reads to the writer.
			if wc > 0 {
				expiry := time.Now().UnixMilli() + writerPinWindowMS
				bw.ResponseWriter.Header().Set(writerPinnedUntilHeader, fmt.Sprintf("%d", expiry))
			}

			bw.flush()
		}
	}
}

// bufferedResponseWriter wraps gin.ResponseWriter and buffers the response
// body so that headers can be added after the handlers run.
type bufferedResponseWriter struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func newBufferedResponseWriter(w gin.ResponseWriter) *bufferedResponseWriter {
	return &bufferedResponseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (b *bufferedResponseWriter) Write(data []byte) (int, error) {
	return b.body.Write(data)
}

func (b *bufferedResponseWriter) WriteHeader(code int) {
	b.status = code
}

// WriteHeaderNow is called by Gin internals; buffer it too.
func (b *bufferedResponseWriter) WriteHeaderNow() {}

// Written returns false so Gin doesn't think headers are committed.
func (b *bufferedResponseWriter) Written() bool { return false }

// Status returns the buffered status code.
func (b *bufferedResponseWriter) Status() int { return b.status }

// flush writes the buffered status + body to the real ResponseWriter.
func (b *bufferedResponseWriter) flush() {
	b.ResponseWriter.WriteHeader(b.status)
	if b.body.Len() > 0 {
		_, _ = b.ResponseWriter.Write(b.body.Bytes())
	}
}
