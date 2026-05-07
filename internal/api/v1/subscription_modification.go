package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// SubscriptionModificationHandler handles API requests for mid-cycle subscription modifications.
type SubscriptionModificationHandler struct {
	modificationService service.SubscriptionModificationService
	log                 *logger.Logger
}

// NewSubscriptionModificationHandler creates a new SubscriptionModificationHandler.
func NewSubscriptionModificationHandler(
	modificationService service.SubscriptionModificationService,
	log *logger.Logger,
) *SubscriptionModificationHandler {
	return &SubscriptionModificationHandler{
		modificationService: modificationService,
		log:                 log,
	}
}

// @Summary Execute subscription modification
// @ID executeSubscriptionModify
// @Description Execute a mid-cycle subscription modification (inheritance or quantity change).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Subscription ID"
// @Param request body dto.ExecuteSubscriptionModifyRequest true "Modification request"
// @Success 200 {object} dto.SubscriptionModifyResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/modify/execute [post]
func (h *SubscriptionModificationHandler) Execute(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ExecuteSubscriptionModifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind JSON", zap.Error(err))
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.modificationService.Execute(c.Request.Context(), subscriptionID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Preview subscription modification
// @ID previewSubscriptionModify
// @Description Preview the impact of a mid-cycle subscription modification without committing changes.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Subscription ID"
// @Param request body dto.ExecuteSubscriptionModifyRequest true "Modification preview request"
// @Success 200 {object} dto.SubscriptionModifyResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/modify/preview [post]
func (h *SubscriptionModificationHandler) Preview(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ExecuteSubscriptionModifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind JSON", zap.Error(err))
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.modificationService.Preview(c.Request.Context(), subscriptionID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
