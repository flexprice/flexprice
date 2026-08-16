package middleware

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxEventIngestionBodyBytes caps a single event-ingestion request body.
//
// Set deliberately high: bulk ingestion is a supported pattern and customers
// post large batches in normal operation, so this is a backstop against a
// single request consuming unbounded memory, not a throughput control.
// Sustained volume is bounded separately by the per-pipeline consumer
// throttles (see EventProcessing.RateLimit and friends).
const MaxEventIngestionBodyBytes = 32 << 20 // 32 MiB

// BodyLimitMiddleware caps the request body at limitBytes.
//
// MaxBytesReader enforces the limit while the body is read rather than
// buffering it first, so an oversized request is cut off mid-stream instead of
// being fully materialised. The handler's existing bind-error path surfaces the
// resulting *http.MaxBytesError as a validation error (HTTP 400).
//
// The wrapper alone is not sufficient. ShouldBindJSON decodes a single JSON
// value and stops at its closing brace — it does not read to EOF — so a small
// valid object followed by megabytes of trailing whitespace or garbage decodes
// successfully without the reader ever reaching its limit. drainBody therefore
// consumes whatever follows the decoded value, which is what actually trips
// MaxBytesReader on an oversized body. Anything left after the first value is
// junk the handler would have ignored regardless.
func BodyLimitMiddleware(limitBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limitBytes)
		}
		c.Next()
	}
}

// BindJSONWithLimit binds the request body and enforces the cap set by
// BodyLimitMiddleware.
//
// The middleware alone does not enforce it. encoding/json stops at the closing
// brace of the first complete value and does not read to EOF — measured, a small
// object is decoded in a single 512-byte read — so a valid object followed by
// megabytes of trailing bytes binds successfully without MaxBytesReader ever
// reaching its limit. Draining after the bind is what actually trips the reader:
// if the remainder pushes the request past the cap, the drain fails and the
// request is rejected.
//
// Trailing content after the first JSON value is malformed input regardless, so
// nothing legitimate is lost by consuming it.
func BindJSONWithLimit(c *gin.Context, obj any) error {
	if err := c.ShouldBindJSON(obj); err != nil {
		return err
	}
	if c.Request.Body == nil {
		return nil
	}
	_, err := io.Copy(io.Discard, c.Request.Body)
	return err
}
