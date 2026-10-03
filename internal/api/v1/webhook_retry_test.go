package v1

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestWebhookRetry_RateLimiting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(middleware.ErrorHandler())

	rateLimitMW := middleware.RateLimitByTenant(rate.Limit(1), 2)
	handlerCalled := 0
	router.POST("/v1/webhooks/retry", rateLimitMW, func(c *gin.Context) {
		handlerCalled++
		c.JSON(http.StatusAccepted, gin.H{"success": true})
	})

	makeReq := func(tenantID, envID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/retry", bytes.NewBufferString(`{"system_event_id":"se_123"}`))
		req.Header.Set("Content-Type", "application/json")
		ctx := types.SetTenantID(req.Context(), tenantID)
		ctx = types.SetEnvironmentID(ctx, envID)
		req = req.WithContext(ctx)
		router.ServeHTTP(w, req)
		return w
	}

	// 1st request - ok
	w1 := makeReq("tenant_test", "env_test")
	assert.Equal(t, http.StatusAccepted, w1.Code)

	// 2nd request - ok (burst of 2)
	w2 := makeReq("tenant_test", "env_test")
	assert.Equal(t, http.StatusAccepted, w2.Code)

	// 3rd request - rate limited (429)
	w3 := makeReq("tenant_test", "env_test")
	assert.Equal(t, http.StatusTooManyRequests, w3.Code)

	// Other tenant is unaffected
	w4 := makeReq("tenant_other", "env_test")
	assert.Equal(t, http.StatusAccepted, w4.Code)

	assert.Equal(t, 3, handlerCalled)
}
