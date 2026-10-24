package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWebhookBodyLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const limit = 16

	tests := []struct {
		name    string
		body    string
		chunked bool
		want    int
		called  bool
	}{
		{"within limit", strings.Repeat("a", limit), false, http.StatusOK, true},
		{"declared length over limit", strings.Repeat("a", limit+1), false, http.StatusRequestEntityTooLarge, false},
		{"streamed body over limit", strings.Repeat("a", limit+1), true, http.StatusRequestEntityTooLarge, false},
		{"streamed body within limit", strings.Repeat("a", limit), true, http.StatusOK, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			var got string
			var called bool
			r.Use(WebhookBodyLimitMiddleware(limit))
			r.POST("/", func(c *gin.Context) {
				called = true
				b, err := io.ReadAll(c.Request.Body)
				if err != nil {
					c.Status(http.StatusBadRequest)
					return
				}
				got = string(b)
				c.Status(http.StatusOK)
			})

			var body io.Reader = strings.NewReader(tt.body)
			if tt.chunked {
				body = io.MultiReader(body)
			}
			req := httptest.NewRequest(http.MethodPost, "/", body)
			if tt.chunked {
				req.ContentLength = -1
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tt.want, w.Code)
			require.Equal(t, tt.called, called)
			if tt.called {
				require.Equal(t, tt.body, got)
			}
		})
	}
}
