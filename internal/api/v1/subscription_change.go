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
		h.log.Error("subscription ID is required")
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.SubscriptionChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind JSON", zap.Error(err))
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	logger := h.log.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("target_plan_id", req.TargetPlanID),
		zap.String("operation", "preview_subscription_change"),
	)

	logger.Info("processing subscription change preview request")

	resp, err := h.subscriptionChangeService.PreviewSubscriptionChange(
		c.Request.Context(),
		subscriptionID,
		req,
	)
	if err != nil {
		logger.Error("failed to preview subscription change", zap.Error(err))
		c.Error(err)
		return
	}

	logger.Info("subscription change preview completed successfully")
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
		h.log.Error("subscription ID is required")
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return
	}

	var req dto.SubscriptionChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("failed to bind JSON", zap.Error(err))
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	logger := h.log.With(
		zap.String("subscription_id", subscriptionID),
		zap.String("target_plan_id", req.TargetPlanID),
		zap.String("operation", "execute_subscription_change"),
	)

	logger.Info("processing subscription change execution request")

	resp, err := h.subscriptionChangeService.ExecuteSubscriptionChange(
		c.Request.Context(),
		subscriptionID,
		req,
	)
	if err != nil {
		logger.Error("failed to execute subscription change", zap.Error(err))
		c.Error(err)
		return
	}

	logger.Info("subscription change executed successfully",
		zap.String("old_subscription_id", resp.OldSubscription.ID),
		zap.String("new_subscription_id", resp.NewSubscription.ID),
		zap.String("change_type", string(resp.ChangeType)),
	)

	c.JSON(http.StatusOK, resp)
}
