package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.SecurityHeadersMiddleware)
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))

	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		"Permissions-Policy":      "geolocation=(), microphone=(), camera=()",
	}
	for h, exp := range want {
		if got := w.Header().Get(h); got != exp {
			t.Errorf("%s = %q, want %q", h, got, exp)
		}
	}
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}
