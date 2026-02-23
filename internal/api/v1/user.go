package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func NewUserHandler(userService service.UserService, logger *logger.Logger) *UserHandler {
	return &UserHandler{userService: userService, logger: logger}
}

type UserHandler struct {
	userService service.UserService
	logger      *logger.Logger
}

// @Summary Get current user
// @ID getUserInfo
// @Description Use to show the logged-in user's profile in the UI or to check permissions and roles for the current session.
// @Tags Users
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.UserResponse
// @Failure 401 {object} ierr.ErrorResponse "Unauthorized"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users/me [get]
func (h *UserHandler) GetUserInfo(c *gin.Context) {
	user, err := h.userService.GetUserInfo(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, user)
}

// @Summary Create service account
// @ID createUser
// @Description Use when provisioning API access for automation, CI/CD pipelines, or headless integrations that need scoped API keys.
// @Tags Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateUserRequest true "Create service account request (type must be 'service_account', roles are required)"
// @Success 201 {object} dto.UserResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Errorw("invalid request body", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request. Only 'type' and 'roles' fields are allowed").
			Mark(ierr.ErrValidation))
		return
	}

	user, err := h.userService.CreateUser(c.Request.Context(), &req)
	if err != nil {
		h.logger.Errorw("failed to create user", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, user)
}

// @Summary Query users
// @ID queryUser
// @Description Use when listing or searching service accounts in an admin UI, or when auditing who has API access and which roles they have.
// @Tags Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param filter body types.UserFilter true "Filter"
// @Success 200 {object} dto.ListUsersResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users/search [post]
func (h *UserHandler) QueryUsers(c *gin.Context) {
	var filter types.UserFilter
	if err := c.ShouldBindJSON(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Set default limit if not provided
	if filter.GetLimit() == 0 {
		filter.Limit = lo.ToPtr(types.GetDefaultFilter().Limit)
	}

	// If no type is specified, default to service_account for backward compatibility
	// But allow users to explicitly filter by type="user" or type="service_account"
	if filter.Type == nil {
		filter.Type = lo.ToPtr(types.UserTypeServiceAccount)
	}

	users, err := h.userService.ListUsersByFilter(c.Request.Context(), &filter)
	if err != nil {
		h.logger.Errorw("failed to list service accounts", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, users)
}
