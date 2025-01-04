package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

func NewUserHandler(userService service.UserService, logger *logger.Logger) *UserHandler {
	return &UserHandler{userService: userService, logger: logger}
}

type UserHandler struct {
	userService service.UserService
	logger      *logger.Logger
}

// @Summary Get user info
// @Description Get the current user's information
// @Tags Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.UserResponse
// @Failure 500 {object} ErrorResponse
// @Router /users/me [get]
func (h *UserHandler) GetUserInfo(c *gin.Context) {
	user, err := h.userService.GetUserInfo(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, user)
}
