package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	adminsvc "github.com/flexprice/flexprice/internal/ee/service/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

// DLQHandler exposes replay through the internal admin router.
type DLQHandler struct{ service adminsvc.DLQService }

// NewDLQHandler constructs the admin replay handler.
func NewDLQHandler(service adminsvc.DLQService) *DLQHandler { return &DLQHandler{service: service} }

// Replay waits for a bounded replay and returns its summary.
func (h *DLQHandler) Replay(c *gin.Context) {
	var req admin.ReplayDLQRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Please check the request payload").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.service.Replay(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
