package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto/admin"
	adminsvc "github.com/flexprice/flexprice/internal/ee/service/admin"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	service adminsvc.UserService
}

func NewUserHandler(service adminsvc.UserService) *UserHandler {
	return &UserHandler{service: service}
}

func (h *UserHandler) AddUser(c *gin.Context) {
	var req admin.AddUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.AddUser(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

func (h *UserHandler) RemoveUser(c *gin.Context) {
	var req admin.RemoveUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.RemoveUser(c.Request.Context(), req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
