package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestBodyLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const limit = 64

	tests := []struct {
		name          string
		body          string
		wantTooLarge  bool
		wantBytesRead int
	}{
		{
			name:          "body under the limit is read in full",
			body:          strings.Repeat("a", limit-1),
			wantTooLarge:  false,
			wantBytesRead: limit - 1,
		},
		{
			name:          "body exactly at the limit is allowed",
			body:          strings.Repeat("a", limit),
			wantTooLarge:  false,
			wantBytesRead: limit,
		},
		{
			name:         "body over the limit is rejected",
			body:         strings.Repeat("a", limit+1),
			wantTooLarge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var readErr error
			var n int

			router := gin.New()
			router.POST("/", BodyLimitMiddleware(limit), func(c *gin.Context) {
				b, err := io.ReadAll(c.Request.Body)
				readErr = err
				n = len(b)
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(tt.body))
			router.ServeHTTP(httptest.NewRecorder(), req)

			if tt.wantTooLarge {
				var maxErr *http.MaxBytesError
				assert.True(t, errors.As(readErr, &maxErr),
					"expected *http.MaxBytesError, got %v", readErr)
				return
			}

			assert.NoError(t, readErr)
			assert.Equal(t, tt.wantBytesRead, n)
		})
	}
}

// A nil body must not panic — GET-style requests reach the middleware too.
func TestBodyLimitMiddleware_NilBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/", BodyLimitMiddleware(64), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() { router.ServeHTTP(w, req) })
	assert.Equal(t, http.StatusOK, w.Code)
}

// Regression for the bypass CodeAnt flagged on PR #2527: encoding/json stops at
// the first complete value, so a small valid object followed by a large tail was
// accepted despite exceeding the cap. BindJSONWithLimit drains after binding,
// which is what makes the limit hold.
func TestBindJSONWithLimit_RejectsOversizedTail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const limit = 1024

	tests := []struct {
		name string
		tail string
	}{
		{name: "trailing whitespace", tail: strings.Repeat(" ", limit*10)},
		{name: "trailing garbage", tail: strings.Repeat("A", limit*10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bindErr error
			router := gin.New()
			router.POST("/", BodyLimitMiddleware(limit), func(c *gin.Context) {
				var v map[string]any
				bindErr = BindJSONWithLimit(c, &v)
				c.Status(http.StatusOK)
			})

			body := `{"a":1}` + tt.tail
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(httptest.NewRecorder(), req)

			assert.Error(t, bindErr, "%d-byte body must be rejected under a %d-byte cap", len(body), limit)
			var maxErr *http.MaxBytesError
			assert.True(t, errors.As(bindErr, &maxErr), "want *http.MaxBytesError, got %v", bindErr)
		})
	}
}

// A well-formed body under the cap must still bind cleanly.
func TestBindJSONWithLimit_AllowsValidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var bindErr error
	var got map[string]any

	router := gin.New()
	router.POST("/", BodyLimitMiddleware(1024), func(c *gin.Context) {
		bindErr = BindJSONWithLimit(c, &got)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req)

	assert.NoError(t, bindErr)
	assert.Equal(t, float64(1), got["a"])
}
