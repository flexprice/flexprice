package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/gin-gonic/gin"
)

// SubscriptionChangeHandler handles API requests for subscription plan changes
type SubscriptionChangeHandler struct {
	subscriptionChangeService service.SubscriptionChangeService
	log                       *logger.Logger
}

// NewSubscriptionChangeHandler creates a new subscription change handler
func NewSubscriptionChangeHandler(
	subscriptionChangeService service.SubscriptionChangeService,
	log *logger.Logger,
) *SubscriptionChangeHandler {
	return &SubscriptionChangeHandler{
		subscriptionChangeService: subscriptionChangeService,
		log:                       log,
	}
}

// @Summary Preview subscription plan change
// @ID previewSubscriptionChange
// @Description Use when showing a customer the cost of a plan change before they confirm (e.g. upgrade/downgrade preview with proration).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.SubscriptionChangeRequest true "Subscription change preview request"
// @Success 200 {object} dto.SubscriptionChangePreviewResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/change/preview [post]
func (h *SubscriptionChangeHandler) PreviewSubscriptionChange(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		h.log.Info(c.Request.Context(), "subscription ID is required")
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.SubscriptionChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error(c.Request.Context(), "failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	logger := h.log.With(
		"subscription_id", subscriptionID,
		"target_plan_id", req.TargetPlanID,
		"operation", "preview_subscription_change",
	)

	logger.Info(c.Request.Context(), "processing subscription change preview request")

	resp, err := h.subscriptionChangeService.PreviewSubscriptionChange(
		c.Request.Context(),
		subscriptionID,
		req,
	)
	if err != nil {
		logger.Error(c.Request.Context(), "failed to preview subscription change", "error", err)
		c.Error(err)
		return
	}

	logger.Info(c.Request.Context(), "subscription change preview completed successfully")
	c.JSON(http.StatusOK, resp)
}

// @Summary Execute subscription plan change
// @ID executeSubscriptionChange
// @Description Use when applying a plan change (e.g. upgrade or downgrade). Executes proration and generates invoice or credit as needed.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Subscription ID"
// @Param request body dto.SubscriptionChangeRequest true "Subscription change request"
// @Success 200 {object} dto.SubscriptionChangeExecuteResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/change/execute [post]
func (h *SubscriptionChangeHandler) ExecuteSubscriptionChange(c *gin.Context) {
	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		h.log.Info(c.Request.Context(), "subscription ID is required")
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.SubscriptionChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error(c.Request.Context(), "failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	logger := h.log.With(
		"subscription_id", subscriptionID,
		"target_plan_id", req.TargetPlanID,
		"operation", "execute_subscription_change",
	)

	logger.Info(c.Request.Context(), "processing subscription change execution request")

	resp, err := h.subscriptionChangeService.ExecuteSubscriptionChange(
		c.Request.Context(),
		subscriptionID,
		req,
	)
	if err != nil {
		logger.Error(c.Request.Context(), "failed to execute subscription change", "error", err)
		c.Error(err)
		return
	}

	logger.Info(c.Request.Context(), "subscription change executed successfully",
		"old_subscription_id", resp.OldSubscription.ID,
		"new_subscription_id", resp.NewSubscription.ID,
		"change_type", string(resp.ChangeType),
	)

	c.JSON(http.StatusOK, resp)
}
