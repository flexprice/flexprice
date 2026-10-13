package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// SubscriptionScheduleHandler handles subscription schedule HTTP requests
type SubscriptionScheduleHandler struct {
	scheduleService service.SubscriptionScheduleService
}

// NewSubscriptionScheduleHandler creates a new subscription schedule handler
func NewSubscriptionScheduleHandler(scheduleService service.SubscriptionScheduleService) *SubscriptionScheduleHandler {
	return &SubscriptionScheduleHandler{
		scheduleService: scheduleService,
	}
}

// GetSchedule retrieves a specific schedule
// @Summary Get subscription schedule
// @ID getSubscriptionSchedule
// @Description Use when you need to load a single scheduled change (e.g. to show when a plan change or renewal takes effect).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param schedule_id path string true "Schedule ID"
// @Success 200 {object} dto.SubscriptionScheduleResponse
// @Router /subscriptions/schedules/{schedule_id} [get]
func (h *SubscriptionScheduleHandler) GetSchedule(c *gin.Context) {
	scheduleID := c.Param("schedule_id")

	schedule, err := h.scheduleService.Get(c.Request.Context(), scheduleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
		return
	}

	response := dto.SubscriptionScheduleResponseFromDomain(schedule)
	c.JSON(http.StatusOK, response)
}

// ListSchedulesForSubscription lists all schedules for a subscription
// @Summary List subscription schedules
// @ID listSubscriptionSchedules
// @Description Use when listing scheduled changes for a subscription (e.g. upcoming plan change or renewal). Returns all schedules for that subscription.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Subscription ID"
// @Success 200 {object} dto.GetPendingSchedulesResponse
// @Router /subscriptions/{id}/schedules [get]
func (h *SubscriptionScheduleHandler) ListSchedulesForSubscription(c *gin.Context) {
	subscriptionID := c.Param("id")

	schedules, err := h.scheduleService.GetBySubscriptionID(c.Request.Context(), subscriptionID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve schedules"})
		return
	}

	response := &dto.GetPendingSchedulesResponse{
		Schedules: dto.SubscriptionScheduleListResponseFromDomain(schedules),
		Count:     len(schedules),
	}

	c.JSON(http.StatusOK, response)
}

// CancelSchedule cancels a pending schedule
// @Summary Cancel subscription schedule
// @ID cancelSubscriptionSchedule
// @Description Use when cancelling a scheduled change (e.g. customer changed mind). Identify by schedule ID in path or by subscription ID + schedule type in body.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param schedule_id path string false "Schedule ID (optional if using request body)"
// @Param request body dto.CancelScheduleRequest false "Cancel request (optional if using path parameter)"
// @Success 200 {object} dto.CancelScheduleResponse
// @Router /subscriptions/schedules/{schedule_id}/cancel [post]
func (h *SubscriptionScheduleHandler) CancelSchedule(c *gin.Context) {
	scheduleID := c.Param("schedule_id")

	// Try to parse request body
	var req dto.CancelScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if scheduleID == "" {
			c.Error(ierr.NewError("missing schedule identifier").
				WithHint("Provide schedule_id in the path, or a body with subscription_id and schedule_type").
				Mark(ierr.ErrValidation))
			return
		}
		req.ScheduleID = &scheduleID
	}

	// Path schedule_id is authoritative when present.
	if scheduleID != "" {
		if req.ScheduleID != nil && *req.ScheduleID != scheduleID {
			c.Error(ierr.NewError("schedule_id mismatch").
				WithHint("schedule_id in the path does not match the request body").
				Mark(ierr.ErrValidation))
			return
		}
		req.ScheduleID = &scheduleID
	}

	if err := req.Validate(); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid cancel-schedule request").
			Mark(ierr.ErrValidation))
		return
	}

	var err error

	// Mode 1: Cancel by schedule ID
	if req.ScheduleID != nil {
		err = h.scheduleService.Cancel(c.Request.Context(), *req.ScheduleID)
	} else {
		// Mode 2: Cancel by subscription ID + schedule type
		err = h.scheduleService.CancelBySubscriptionAndType(
			c.Request.Context(),
			*req.SubscriptionID,
			*req.ScheduleType,
		)
	}

	if err != nil {
		switch err.Error() {
		case "subscription schedule not found",
			"failed to get schedule",
			"failed to get pending schedule":
			c.Error(ierr.NewError("schedule not found").
				WithHint("Schedule not found").
				Mark(ierr.ErrNotFound))
		default:
			c.Error(err)
		}
		return
	}

	response := &dto.CancelScheduleResponse{
		Status:  types.ScheduleStatusCancelled,
		Message: "Schedule cancelled successfully",
	}

	c.JSON(http.StatusOK, response)
}

// ListSchedules lists schedules with filtering
// @Summary List all subscription schedules
// @ID listAllSubscriptionSchedules
// @Description Use when listing or searching scheduled changes across subscriptions (e.g. admin view). Returns schedules with optional filtering.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param pending_only query boolean false "Filter to pending schedules only"
// @Param subscription_id query string false "Filter by subscription ID"
// @Param subscription_ids query []string false "Filter by subscription IDs" collectionFormat(multi)
// @Param schedule_type query []string false "Filter by schedule type" Enums(plan_change, cancellation) collectionFormat(multi)
// @Param schedule_status query []string false "Filter by schedule status" Enums(pending, executing, executed, cancelled, failed) collectionFormat(multi)
// @Param limit query int false "Limit results"
// @Param offset query int false "Offset for pagination"
// @Success 200 {object} dto.GetPendingSchedulesResponse
// @Router /subscriptions/schedules [get]
func (h *SubscriptionScheduleHandler) ListSchedules(c *gin.Context) {
	// Parse filter from query params
	filter := &types.SubscriptionScheduleFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	}

	if err := c.ShouldBindQuery(filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Accept singular subscription_id (documented param) in addition to subscription_ids.
	if subID := c.Query("subscription_id"); subID != "" {
		filter.SubscriptionIDs = append(filter.SubscriptionIDs, subID)
	}

	schedules, err := h.scheduleService.List(c.Request.Context(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve schedules"})
		return
	}

	response := &dto.GetPendingSchedulesResponse{
		Schedules: dto.SubscriptionScheduleListResponseFromDomain(schedules),
		Count:     len(schedules),
	}

	c.JSON(http.StatusOK, response)
}
