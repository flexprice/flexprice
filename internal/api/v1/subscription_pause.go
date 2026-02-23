package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

// SubscriptionPauseHandler handles API requests for subscription pauses
type SubscriptionPauseHandler struct {
	service service.SubscriptionService
	log     *logger.Logger
}

// NewSubscriptionPauseHandler creates a new subscription pause handler
func NewSubscriptionPauseHandler(service service.SubscriptionService, log *logger.Logger) *SubscriptionPauseHandler {
	return &SubscriptionPauseHandler{
		service: service,
		log:     log,
	}
}

// @Summary Pause a subscription
// @ID pauseSubscription
// @Description Pause a subscription with the specified parameters
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.PauseSubscriptionRequest true "Pause subscription request"
// @Success 200 {object} dto.SubscriptionPauseResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id}/pause [post]
func (h *SubscriptionPauseHandler) PauseSubscription(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		h.log.Errorw("subscription ID is required")
		c.Error(ierr.NewError("Subscription ID is required").Mark(ierr.ErrValidation))
		return
	}

	var req dto.PauseSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Errorw("failed to bind request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	// Handle dry run request to calculate impact
	if req.DryRun {
		impact, err := h.service.CalculatePauseImpact(
			c.Request.Context(),
			subscriptionID,
			&req,
		)
		if err != nil {
			h.log.Errorw("failed to calculate pause impact", "error", err, "subscription_id", subscriptionID)
			c.Error(err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"impact": impact,
		})
		return
	}

	// Process the actual pause
	resp, err := h.service.PauseSubscription(
		c.Request.Context(),
		subscriptionID,
		&req,
	)
	if err != nil {
		h.log.Errorw("failed to pause subscription", "error", err, "subscription_id", subscriptionID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Resume a paused subscription
// @ID resumeSubscription
// @Description Resume a paused subscription with the specified parameters
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.ResumeSubscriptionRequest true "Resume subscription request"
// @Success 200 {object} dto.SubscriptionPauseResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id}/resume [post]
func (h *SubscriptionPauseHandler) ResumeSubscription(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		h.log.Errorw("subscription ID is required")
		c.Error(ierr.NewError("Subscription ID is required").Mark(ierr.ErrValidation))
		return
	}

	var req dto.ResumeSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request body").
			Mark(ierr.ErrValidation))
		return
	}

	// Handle dry run request to calculate impact
	if req.DryRun {
		impact, err := h.service.CalculateResumeImpact(
			c.Request.Context(),
			subscriptionID,
			&req,
		)
		if err != nil {
			h.log.Errorw("failed to calculate resume impact", "error", err, "subscription_id", subscriptionID)
			c.Error(err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"impact": impact,
		})
		return
	}

	// Process the actual resume
	resp, err := h.service.ResumeSubscription(
		c.Request.Context(),
		subscriptionID,
		&req,
	)
	if err != nil {
		h.log.Errorw("failed to resume subscription", "error", err, "subscription_id", subscriptionID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary List all pauses for a subscription
// @ID listSubscriptionPauses
// @Description List all pauses for a subscription
// @Tags Subscriptions
// @Produce json
// @Param id path string true "Subscription ID"
// @Success 200 {array} dto.ListSubscriptionPausesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /subscriptions/{id}/pauses [get]
func (h *SubscriptionPauseHandler) ListPauses(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		h.log.Errorw("subscription ID is required")
		c.Error(ierr.NewError("Subscription ID is required").Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ListPauses(c.Request.Context(), subscriptionID)
	if err != nil {
		h.log.Errorw("failed to list subscription pauses", "error", err, "subscription_id", subscriptionID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
