package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// SubscriptionPauseHandler handles API requests for pausing and resuming subscriptions
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

// @Summary Pause subscription
// @ID pauseSubscription
// @Description Use when a customer stops temporarily. Pauses an active subscription now: usage and arrear fixed charges up to the pause are billed on a closing invoice, and every paused period after it gets a $0 skipped invoice.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.PauseSubscriptionRequest true "Pause subscription request"
// @Success 200 {object} dto.PauseSubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/pause [post]
func (h *SubscriptionPauseHandler) PauseSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.PauseSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error(c.Request.Context(), "failed to bind pause subscription request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.PauseSubscription(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Resume subscription
// @ID resumeSubscription
// @Description Use when a paused customer comes back. Resumes a paused subscription now on its existing billing anchor: the first period runs from the resume to the next billing date and its advance charges are invoiced at once.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.ResumeSubscriptionRequest true "Resume subscription request"
// @Success 200 {object} dto.ResumeSubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/resume [post]
func (h *SubscriptionPauseHandler) ResumeSubscription(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.ResumeSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error(c.Request.Context(), "failed to bind resume subscription request", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	resp, err := h.service.ResumeSubscription(c.Request.Context(), id, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}
