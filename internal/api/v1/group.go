package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type GroupHandler struct {
	service service.GroupService
	log     *logger.Logger
}

func NewGroupHandler(service service.GroupService, log *logger.Logger) *GroupHandler {
	return &GroupHandler{service: service, log: log}
}

// @Summary Create group
// @ID createGroup
// @Description Use when organizing entities into a group (e.g. for filtering prices or plans by product line or region).
// @Tags Groups
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param group body dto.CreateGroupRequest true "Group"
// @Success 201 {object} dto.GroupResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /groups [post]
func (h *GroupHandler) CreateGroup(c *gin.Context) {
	var req dto.CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.CreateGroup(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Get group
// @ID getGroup
// @Description Use when you need to load a single group (e.g. for display or to assign entities).
// @Tags Groups
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Group ID"
// @Success 200 {object} dto.GroupResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /groups/{id} [get]
func (h *GroupHandler) GetGroup(c *gin.Context) {
	id := c.Param("id")

	resp, err := h.service.GetGroup(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete group
// @ID deleteGroup
// @Description Use when removing a group and clearing its entity associations (e.g. retiring a product line). Returns 204 or 200 on success.
// @Tags Groups
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Group ID"
// @Success 204 "No Content"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /groups/{id} [delete]
func (h *GroupHandler) DeleteGroup(c *gin.Context) {
	id := c.Param("id")

	err := h.service.DeleteGroup(c.Request.Context(), id)
	if err != nil {
		c.Error(err)
		return
	}

	c.Status(http.StatusOK)
}

// @Summary Query groups
// @ID queryGroup
// @Description Use when listing or searching groups (e.g. admin catalog). Returns a paginated list; supports filtering and sorting.
// @Tags Groups
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.GroupFilter true "Filter"
// @Success 200 {object} dto.ListGroupsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /groups/search [post]
func (h *GroupHandler) QueryGroups(c *gin.Context) {
	var filter types.GroupFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if err := filter.Validate(); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	if filter.GetLimit() == 0 {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}

	resp, err := h.service.ListGroups(c.Request.Context(), &filter)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
