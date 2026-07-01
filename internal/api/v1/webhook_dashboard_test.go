package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGetDashboardURL_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &WebhookHandler{config: &config.Configuration{}} // Svix.Enabled defaults false
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/webhooks/dashboard", nil)

	h.GetDashboardURL(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Equal(t, false, body["svix_enabled"])
	assert.NotContains(t, body, "token")
}
