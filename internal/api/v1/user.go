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

// @Summary Create user or service account
// @ID createUser
// @Description Create a user account (type=user, email required; returns user + password for login) or a service account (type=service_account, roles required) for API/automation access.
// @Tags Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.CreateUserRequest true "Create user (email, type=user) or service account (type=service_account, roles)"
// @Success 201 {object} dto.CreateUserResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(c.Request.Context(), "invalid request body", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request. For user: type and email required. For service_account: type and roles required.").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.userService.CreateUser(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to create user", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// @Summary Update current user
// @ID updateUser
// @Description Update the current authenticated user. Supports name and metadata updates.
// @Tags Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.UpdateUserRequest true "Update user request"
// @Success 200 {object} dto.UpdateUserResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users/me [put]
func (h *UserHandler) UpdateUser(c *gin.Context) {
	var req dto.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(c.Request.Context(), "invalid request body", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.userService.UpdateUser(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to update user", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
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
		h.logger.Error(c.Request.Context(), "failed to list service accounts", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, users)
}

// @Summary Update service account
// @ID updateServiceAccount
// @Description Update a service account by ID (name only).
// @Tags Users
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Service Account ID"
// @Param request body dto.UpdateServiceAccountRequest true "Update service account request"
// @Success 200 {object} dto.UpdateServiceAccountResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users/{id} [put]
func (h *UserHandler) UpdateServiceAccount(c *gin.Context) {
	id := c.Param("id")
	var req dto.UpdateServiceAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(c.Request.Context(), "invalid request body", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.userService.UpdateServiceAccount(c.Request.Context(), id, &req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to update service account", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Delete service account
// @ID deleteServiceAccount
// @Description Soft-delete (archive) a service account by ID.
// @Tags Users
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Service Account ID"
// @Success 204 "No content"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /users/{id} [delete]
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id := c.Param("id")
	if err := h.userService.DeleteUser(c.Request.Context(), id); err != nil {
		h.logger.Error(c.Request.Context(), "failed to delete user", "error", err)
		c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
