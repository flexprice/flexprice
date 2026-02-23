package v1

import (
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService service.AuthService
	logger      *logger.Logger
	cfg         *config.Configuration
}

func NewAuthHandler(cfg *config.Configuration, authService service.AuthService, logger *logger.Logger) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		logger:      logger,
		cfg:         cfg,
	}
}

// @Summary Sign up
// @ID signUp
// @Description Sign up a new user
// @Tags Auth
// @Accept json
// @Produce json
// @Param signup body dto.SignUpRequest true "Sign up request"
// @Success 200 {object} dto.AuthResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /auth/signup [post]
func (h *AuthHandler) SignUp(c *gin.Context) {
	var req dto.SignUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	// For Supabase auth, extract token from Authorization header if available
	if req.Token == "" {
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			req.Token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	authResponse, err := h.authService.SignUp(c.Request.Context(), &req)
	if err != nil {
		h.logger.Errorw("failed to sign up", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, authResponse)
}

// @Summary Login
// @ID login
// @Description Login a user
// @Tags Auth
// @Accept json
// @Produce json
// @Param login body dto.LoginRequest true "Login request"
// @Success 200 {object} dto.AuthResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	authResponse, err := h.authService.Login(c.Request.Context(), &req)
	if err != nil {
		h.logger.Errorw("failed to login", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, authResponse)
}
