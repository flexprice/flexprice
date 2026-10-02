package v1

import (
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
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

	ctx := types.SetActor(c.Request.Context(), types.SystemActor("signup", "Signup"))
	ctx = types.SetSource(ctx, types.SourceDashboard)
	authResponse, err := h.authService.SignUp(ctx, &req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to sign up", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, authResponse)
}

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

	ctx := types.SetActor(c.Request.Context(), types.SystemActor("login", "Login"))
	ctx = types.SetSource(ctx, types.SourceDashboard)
	authResponse, err := h.authService.Login(ctx, &req)
	if err != nil {
		h.logger.Error(c.Request.Context(), "failed to login", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, authResponse)
}
