package v1

import (
	"net/http"
	"time"

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
// @Description
// @Description change_at controls timing. Omitted or 'immediate' applies the change now. 'end_of_period' records a pending schedule that executes at the subscription's current period end: the response returns is_scheduled, schedule_id and scheduled_at instead of a completed change, and nothing is swapped or billed until the boundary.
// @Description
// @Description scheduled_at is resolved from the subscription's current period end at request time. If that period end is already in the past (a backdated start date, a resumed pause, or worker downtime can all leave a subscription behind), the change is due immediately and fires on the next billing scan rather than a period away — inspect scheduled_at to see this.
// @Description
// @Description Only one plan change may be pending per subscription. By default (on_conflict_policies.on_pending_schedule = 'reject') a second request returns 400; cancel the existing schedule via POST /subscriptions/schedules/{schedule_id}/cancel first. Pending schedules are listable via GET /subscriptions/{id}/schedules.
// @Description
// @Description Set on_conflict_policies.on_pending_schedule to 'supersede' to replace the queued change instead: the pending schedule is cancelled and this request applied in the same transaction, so both land or neither does. The cancelled schedule ids are returned in superseded_schedules, and preview reports the same list without writing.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @x-scope "write"
// @Param id path string true "Subscription ID"
// @Param request body dto.SubscriptionChangeV2Request true "Plan change request"
// @Success 200 {object} dto.SubscriptionChangeV2Response
// @Failure 400 {object} ierr.ErrorResponse "Invalid request, a change this endpoint cannot make (interval, currency, hierarchy, phases, paused), a deferred change with proration, or a plan change already scheduled"
// @Failure 404 {object} ierr.ErrorResponse "Subscription or plan not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/change/v2/execute [post]
func (h *SubscriptionHandler) ExecuteSubscriptionPlanChangeV2(c *gin.Context) {
	subscriptionID, req, ok := bindPlanChangeV2Request(c)
	if !ok {
		return
	}

	resp, err := h.service.ExecutePlanChange(c.Request.Context(), subscriptionID, req, time.Now().UTC())
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
