package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/service"
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
// @Description Retrieves details of a specific subscription schedule
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param id path string true "Schedule ID"
// @Success 200 {object} dto.SubscriptionScheduleResponse
// @Failure 404 {object} dto.ErrorResponse
// @Router /v1/subscription-schedules/{id} [get]
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
// @Description Retrieves all schedules for a specific subscription
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param subscription_id path string true "Subscription ID"
// @Success 200 {object} dto.GetPendingSchedulesResponse
// @Router /v1/subscriptions/{subscription_id}/schedules [get]
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
// @Description Cancels a pending subscription schedule. Supports two modes: 1) By schedule ID in path, or 2) By subscription ID + schedule type in request body
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param schedule_id path string false "Schedule ID (optional if using request body)"
// @Param request body dto.CancelScheduleRequest false "Cancel request (optional if using path parameter)"
// @Success 200 {object} dto.CancelScheduleResponse
// @Failure 400 {object} dto.ErrorResponse
// @Failure 404 {object} dto.ErrorResponse
// @Router /v1/subscriptions/schedules/{schedule_id}/cancel [post]
func (h *SubscriptionScheduleHandler) CancelSchedule(c *gin.Context) {
	scheduleID := c.Param("schedule_id")

	// Try to parse request body
	var req dto.CancelScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// If no body provided, use schedule_id from path
		if scheduleID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Either schedule_id in path or request body with subscription_id and schedule_type must be provided"})
			return
		}
		req.ScheduleID = &scheduleID
	}

	// Validate the request
	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		// Check if it's a not found error
		if err.Error() == "subscription schedule not found" ||
			err.Error() == "failed to get schedule" ||
			err.Error() == "failed to get pending schedule" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Schedule not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
// @Description Retrieves subscription schedules with optional filtering
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param pending_only query boolean false "Filter to pending schedules only"
// @Param subscription_id query string false "Filter by subscription ID"
// @Param limit query int false "Limit results"
// @Param offset query int false "Offset for pagination"
// @Success 200 {object} dto.GetPendingSchedulesResponse
// @Router /v1/subscription-schedules [get]
func (h *SubscriptionScheduleHandler) ListSchedules(c *gin.Context) {
	// Parse filter from query params
	filter := &types.SubscriptionScheduleFilter{
		QueryFilter: types.NewDefaultQueryFilter(),
	}

	if err := c.ShouldBindQuery(filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
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
