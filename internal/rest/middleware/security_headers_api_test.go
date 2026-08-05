package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
)

// Security headers must not alter API behaviour for programmatic clients.
func TestSecurityHeadersDoNotAffectAPIClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORSMiddleware, middleware.SecurityHeadersMiddleware)
	r.POST("/v1/events", func(c *gin.Context) { c.JSON(202, gin.H{"message": "accepted"}) })
	r.OPTIONS("/v1/events", func(c *gin.Context) {})

	t.Run("POST from SDK still succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/events", nil)
		req.Header.Set("x-api-key", "sk_test")
		req.Header.Set("User-Agent", "flexprice-go-sdk/1.0")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 202 {
			t.Fatalf("status = %d, want 202", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct == "" {
			t.Error("Content-Type missing — clients parse on this")
		}
	})

	t.Run("CORS preflight still permissive", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/v1/events", nil)
		req.Header.Set("Origin", "https://customer-dashboard.example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "x-api-key, content-type")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("ACAO = %q, want * — browser clients would break", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
			t.Errorf("ACAM = %q, want it to include POST", got)
		}
		if got := w.Header().Get("Access-Control-Allow-Headers"); got != "*" {
			t.Errorf("ACAH = %q, want * — x-api-key must be allowed", got)
		}
		if w.Code != http.StatusOK {
			t.Errorf("preflight status = %d, want 200", w.Code)
		}
	})

	t.Run("no request-side gating", func(t *testing.T) {
		// No Origin, no User-Agent — a bare curl. Must still pass.
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/events", nil))
		if w.Code != 202 {
			t.Errorf("bare client status = %d, want 202", w.Code)
		}
	})
}
