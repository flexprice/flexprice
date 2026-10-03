package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type ActivityLogHandler struct {
	svc service.ActivityLogService
}

func NewActivityLogHandler(svc service.ActivityLogService) *ActivityLogHandler {
	return &ActivityLogHandler{svc: svc}
}

// @Summary List activity
// @ID listActivity
// @Description Use when auditing what changed in the account. Returns newest first with keyset pagination; pass next_cursor to page.
// @Tags Activity
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter query types.ActivityFilter false "Filter"
// @Success 200 {object} dto.ListActivityResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Router /activity [get]
func (h *ActivityLogHandler) List(c *gin.Context) {
	var f types.ActivityFilter
	if err := c.ShouldBindQuery(&f); err != nil {
		c.Error(ierr.WithError(err).WithHint("invalid filter").Mark(ierr.ErrValidation))
		return
	}
	resp, err := h.svc.List(c.Request.Context(), &f)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Get activity entry
// @ID getActivity
// @Tags Activity
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Activity ID"
// @Success 200 {object} dto.ActivityResponse
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Router /activity/{id} [get]
func (h *ActivityLogHandler) Get(c *gin.Context) {
	resp, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
