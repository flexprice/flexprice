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
