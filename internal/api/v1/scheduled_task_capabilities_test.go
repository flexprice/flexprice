package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type capabilitiesStub struct{ service.ScheduledTaskService }

func (capabilitiesStub) GetCapabilities(context.Context) *dto.ScheduledTaskCapabilitiesResponse {
	return &dto.ScheduledTaskCapabilitiesResponse{Email: dto.ExportEmailCapability{MaxRecipients: types.MaxExportEmailRecipients}}
}

func TestGetCapabilities_UnavailableByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewScheduledTaskHandler(capabilitiesStub{}, nil)
	r := gin.New()
	r.GET("/tasks/scheduled/capabilities", h.GetCapabilities)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/tasks/scheduled/capabilities", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var resp dto.ScheduledTaskCapabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Email.Available || resp.Email.MaxRecipients != 10 {
		t.Errorf("unexpected %+v", resp)
	}
}
