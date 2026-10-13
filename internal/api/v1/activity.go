package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

type ActivityHandler struct {
	service service.ActivityService
	log     *logger.Logger
}

func NewActivityHandler(service service.ActivityService, log *logger.Logger) *ActivityHandler {
	return &ActivityHandler{service: service, log: log}
}

// ListActivity godoc
// @Summary List activity
// @ID listActivity
// @Description Use when you need the audit trail of changes to billing entities: who changed what, when, from where. Filter by entity, customer, actor, action or request; pages are newest first.
// @Tags Activity
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param filter body dto.ListActivityRequest true "Filters"
// @Success 200 {object} dto.ListActivityResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /activity [post]
func (h *ActivityHandler) ListActivity(c *gin.Context) {
	var req dto.ListActivityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
		return
	}
	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}
	resp, err := h.service.List(c.Request.Context(), &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GetActivity godoc
// @Summary Get activity
// @ID getActivity
// @Description Use when you need one activity entry with the state of the entity right after the change.
// @Tags Activity
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Activity ID"
// @Success 200 {object} dto.ActivityResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid activity id"
// @Failure 404 {object} ierr.ErrorResponse "Activity not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /activity/{id} [get]
func (h *ActivityHandler) GetActivity(c *gin.Context) {
	resp, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, resp)
}
