package v1

import (
	"net/http"
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
// @Tags events
// @Accept json
// @Produce json
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

	err := h.eventService.CreateEvent(ctx, &req)
	if err != nil {
		h.log.Error("Failed to ingest event", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to ingest event"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Event accepted for processing", "event_id": req.EventID})
}

// @Summary Get usage statistics
// @Description Retrieve aggregated usage statistics for events
// @Tags events
// @Produce json
// @Param external_customer_id query string true "External Customer ID"
// @Param event_name query string true "Event Name"
// @Param property_name query string false "Property Name"
// @Param aggregation_type query string false "Aggregation Type (sum, count, avg)"
// @Param start_time query string false "Start Time (RFC3339)"
// @Param end_time query string false "End Time (RFC3339)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /events/usage [get]
func (h *EventsHandler) GetUsage(c *gin.Context) {
	ctx := c.Request.Context()
	externalCustomerID := c.Query("external_customer_id")
	eventName := c.Query("event_name")
	propertyName := c.Query("property_name")
	aggregationType := c.Query("aggregation_type")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	if externalCustomerID == "" || eventName == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Missing required parameters"})
		return
	}

	if startTimeStr == "" || endTimeStr == "" {
		// Default to last 7 days
		endTimeStr = time.Now().Format(time.RFC3339)
		startTimeStr = time.Now().AddDate(0, 0, -7).Format(time.RFC3339)
	}

	// Parse times
	startTime, err := time.Parse(time.RFC3339, startTimeStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid start_time format"})
		return
	}

	endTime, err := time.Parse(time.RFC3339, endTimeStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Invalid end_time format"})
		return
	}

	// Ensure times are in UTC
	startTime = startTime.UTC()
	endTime = endTime.UTC()

	result, err := h.eventService.GetUsage(ctx, &dto.GetUsageRequest{
		ExternalCustomerID: externalCustomerID,
		EventName:          eventName,
		PropertyName:       propertyName,
		AggregationType:    aggregationType,
		StartTime:          startTime,
		EndTime:            endTime,
	})
	if err != nil {
		h.log.Error("Failed to get usage", "error", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "Failed to get usage"})
		return
	}

	c.JSON(http.StatusOK, result)
}
