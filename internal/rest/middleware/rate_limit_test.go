package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestRateLimitByTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("allows requests within burst and throttles exceeding requests", func(t *testing.T) {
		router := gin.New()
		router.Use(ErrorHandler())
		// 1 req/sec with burst of 2
		router.POST("/test", RateLimitByTenant(rate.Limit(1), 2), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		makeReq := func(tenantID, envID string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/test", nil)
			if tenantID != "" {
				ctx := types.SetTenantID(req.Context(), tenantID)
				if envID != "" {
					ctx = types.SetEnvironmentID(ctx, envID)
				}
				req = req.WithContext(ctx)
			}
			router.ServeHTTP(w, req)
			return w
		}

		// First two requests should pass (burst = 2)
		w1 := makeReq("tenant_1", "env_1")
		assert.Equal(t, http.StatusOK, w1.Code)

		w2 := makeReq("tenant_1", "env_1")
		assert.Equal(t, http.StatusOK, w2.Code)

		// Third request should be rate limited (429)
		w3 := makeReq("tenant_1", "env_1")
		assert.Equal(t, http.StatusTooManyRequests, w3.Code)

		// Different tenant should not be affected by tenant_1's rate limit
		w4 := makeReq("tenant_2", "env_1")
		assert.Equal(t, http.StatusOK, w4.Code)
	})

	t.Run("falls back to client IP when tenant ID is absent", func(t *testing.T) {
		router := gin.New()
		router.Use(ErrorHandler())
		router.POST("/test", RateLimitByTenant(rate.Limit(1), 1), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req1 := httptest.NewRequest(http.MethodPost, "/test", nil)
		req1.RemoteAddr = "192.0.2.1:1234"
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// Second request from same IP exceeds burst=1
		req2 := httptest.NewRequest(http.MethodPost, "/test", nil)
		req2.RemoteAddr = "192.0.2.1:1234"
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// Different IP passes
		req3 := httptest.NewRequest(http.MethodPost, "/test", nil)
		req3.RemoteAddr = "192.0.2.2:1234"
		w3 := httptest.NewRecorder()
		router.ServeHTTP(w3, req3)
		assert.Equal(t, http.StatusOK, w3.Code)
	})

	t.Run("cleans up expired limiters", func(t *testing.T) {
		rl := NewRateLimiter(rate.Limit(1), 1, 10*time.Millisecond)
		rl.getLimiter("key1")

		assert.Len(t, rl.limiters, 1)

		time.Sleep(20 * time.Millisecond)

		// Populate dummy entries to exceed the 1000 threshold to trigger cleanup
		rl.mu.Lock()
		for i := 0; i < 1001; i++ {
			rl.limiters[string(rune(i+100))] = &clientLimiter{
				limiter:  rate.NewLimiter(rate.Limit(1), 1),
				lastSeen: time.Now().Add(-1 * time.Hour),
			}
		}
		rl.mu.Unlock()

		// Trigger getLimiter which triggers cleanup
		rl.getLimiter("key_new")

		rl.mu.Lock()
		// All expired entries should be deleted
		assert.Less(t, len(rl.limiters), 5)
		rl.mu.Unlock()
	})
}
