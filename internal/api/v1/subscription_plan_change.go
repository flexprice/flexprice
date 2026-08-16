package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/gin-gonic/gin"
)

// @Summary Preview a plan change (v2, swap in place)
// @ID previewSubscriptionPlanChangeV2
// @Description Preview a subscription plan change without writing. Swap-in-place: subscription id, billing anchor and period bounds are preserved.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "read"
// @Param id path string true "Subscription ID"
// @Param request body dto.SubscriptionChangeV2Request true "Plan change request"
// @Success 200 {object} dto.SubscriptionChangeV2Response
// @Failure 400 {object} ierr.ErrorResponse "Invalid request, or a change this endpoint cannot make (interval, currency, hierarchy, phases, paused)"
// @Failure 404 {object} ierr.ErrorResponse "Subscription or plan not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/change/v2/preview [post]
func (h *SubscriptionHandler) PreviewSubscriptionPlanChangeV2(c *gin.Context) {
	subscriptionID, req, ok := bindPlanChangeV2Request(c)
	if !ok {
		return
	}

	resp, err := h.service.PreviewPlanChange(c.Request.Context(), subscriptionID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// @Summary Execute a plan change (v2, swap in place)
// @ID executeSubscriptionPlanChangeV2
// @Description Change a subscription's plan in place. Subscription id, billing anchor and period bounds are preserved; line items are sliced and settled in one transaction.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Subscription ID"
// @Param request body dto.SubscriptionChangeV2Request true "Plan change request"
// @Success 200 {object} dto.SubscriptionChangeV2Response
// @Failure 400 {object} ierr.ErrorResponse "Invalid request, or a change this endpoint cannot make (interval, currency, hierarchy, phases, paused)"
// @Failure 404 {object} ierr.ErrorResponse "Subscription or plan not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/change/v2/execute [post]
func (h *SubscriptionHandler) ExecuteSubscriptionPlanChangeV2(c *gin.Context) {
	subscriptionID, req, ok := bindPlanChangeV2Request(c)
	if !ok {
		return
	}

	resp, err := h.service.ExecutePlanChange(c.Request.Context(), subscriptionID, req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

func bindPlanChangeV2Request(c *gin.Context) (string, dto.SubscriptionChangeV2Request, bool) {
	var req dto.SubscriptionChangeV2Request

	subscriptionID := c.Param("id")
	if subscriptionID == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Please provide a valid subscription ID").
			Mark(ierr.ErrValidation))
		return "", req, false
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return "", req, false
	}

	return subscriptionID, req, true
}
