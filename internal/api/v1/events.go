package v1

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type EventsHandler struct {
	eventService service.EventService
	log          *logger.Logger
}

func NewEventsHandler(eventService service.EventService, log *logger.Logger) *EventsHandler {
	return &EventsHandler{
		eventService: eventService,
		log:          log,
	}
}

// @Summary Ingest event
// @Description Ingest a new event into the system
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param event body dto.IngestEventRequest true "Event data"
// @Success 202 {object} map[string]string "message:Event accepted for processing"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /events [post]
func (h *EventsHandler) IngestEvent(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.IngestEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid request payload"})
		return
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	err := h.eventService.CreateEvent(ctx, &req)
	if err != nil {
		h.log.Error("Failed to ingest event", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to ingest event"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Event accepted for processing", "event_id": req.EventID})
}

// @Summary Get usage by meter
// @Description Retrieve aggregated usage statistics using meter configuration
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageByMeterRequest true "Request body"
// @Success 200 {object} dto.GetUsageResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /events/usage/meter [post]
func (h *EventsHandler) GetUsageByMeter(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetUsageByMeterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	req.StartTime, req.EndTime, err = validateStartAndEndTime(req.StartTime, req.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	result, err := h.eventService.GetUsageByMeter(ctx, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	response := dto.FromAggregationResult(result)
	c.JSON(http.StatusOK, response)
}

// @Summary Get usage statistics
// @Description Retrieve aggregated usage statistics for events
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageRequest true "Request body"
// @Success 200 {object} dto.GetUsageResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /events/usage [post]
func (h *EventsHandler) GetUsage(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	req.StartTime, req.EndTime, err = validateStartAndEndTime(req.StartTime, req.EndTime)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	result, err := h.eventService.GetUsage(ctx, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	response := dto.FromAggregationResult(result)
	c.JSON(http.StatusOK, response)
}

// @Summary Get raw events
// @Description Retrieve raw events with pagination and filtering
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Param external_customer_id query string false "External Customer ID"
// @Param event_name query string false "Event Name"
// @Param start_time query string false "Start Time (RFC3339)"
// @Param end_time query string false "End Time (RFC3339)"
// @Param iter_first_key query string false "Iter First Key (unix_timestamp_nanoseconds::event_id)"
// @Param iter_last_key query string false "Iter Last Key (unix_timestamp_nanoseconds::event_id)"
// @Param page_size query int false "Page Size (1-50)"
// @Success 200 {object} dto.GetEventsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /events [get]
func (h *EventsHandler) GetEvents(c *gin.Context) {
	ctx := c.Request.Context()
	externalCustomerID := c.Query("external_customer_id")
	eventName := c.Query("event_name")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	iterFirstKey := c.Query("iter_first_key")
	iterLastKey := c.Query("iter_last_key")

	pageSize := 50
	if size := c.Query("page_size"); size != "" {
		if parsed, err := strconv.Atoi(size); err == nil {
			if parsed > 0 && parsed <= 100 {
				pageSize = parsed
			}
		}
	}

	startTime, endTime, err := parseStartAndEndTime(startTimeStr, endTimeStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	events, err := h.eventService.GetEvents(ctx, &dto.GetEventsRequest{
		ExternalCustomerID: externalCustomerID,
		EventName:          eventName,
		StartTime:          startTime,
		EndTime:            endTime,
		PageSize:           pageSize,
		IterFirstKey:       iterFirstKey,
		IterLastKey:        iterLastKey,
	})
	if err != nil {
		h.log.Error("Failed to get events", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get events"})
		return
	}

	c.JSON(http.StatusOK, events)
}

func parseStartAndEndTime(startTimeStr, endTimeStr string) (time.Time, time.Time, error) {
	var startTime time.Time
	var endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}

	return validateStartAndEndTime(startTime, endTime)
}

func validateStartAndEndTime(startTime, endTime time.Time) (time.Time, time.Time, error) {
	if endTime.IsZero() {
		endTime = time.Now()
	}

	if startTime.IsZero() {
		startTime = endTime.AddDate(0, 0, -3)
	}

	if endTime.Before(startTime) {
		return time.Time{}, time.Time{}, errors.New("end time must be after start time")
	}

	// Ensure times are in UTC
	startTime = startTime.UTC()
	endTime = endTime.UTC()

	return startTime, endTime, nil
}
