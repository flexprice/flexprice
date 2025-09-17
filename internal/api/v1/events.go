package v1

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type EventsHandler struct {
	eventService                service.EventService
	eventPostProcessingService  service.EventPostProcessingService
	featureUsageTrackingService service.FeatureUsageTrackingService
	log                         *logger.Logger
}

func NewEventsHandler(eventService service.EventService, eventPostProcessingService service.EventPostProcessingService, featureUsageTrackingService service.FeatureUsageTrackingService, log *logger.Logger) *EventsHandler {
	return &EventsHandler{
		eventService:                eventService,
		eventPostProcessingService:  eventPostProcessingService,
		featureUsageTrackingService: featureUsageTrackingService,
		log:                         log,
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
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /events [post]
func (h *EventsHandler) IngestEvent(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.IngestEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.NewError("invalid request payload").
			WithHint("Invalid request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	err := h.eventService.CreateEvent(ctx, &req)
	if err != nil {
		h.log.Error("Failed to ingest event", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Event accepted for processing", "event_id": req.EventID})
}

// @Summary Bulk Ingest events
// @Description Ingest bulk events into the system
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param event body dto.BulkIngestEventRequest true "Event data"
// @Success 202 {object} map[string]string "message:Event accepted for processing"
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /events/bulk [post]
func (h *EventsHandler) BulkIngestEvent(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.BulkIngestEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request payload").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.eventService.BulkCreateEvents(ctx, &req)
	if err != nil {
		h.log.Error("Failed to bulk ingest events", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Events accepted for processing"})
}

// @Summary Get usage by meter
// @Description Retrieve aggregated usage statistics using meter configuration
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageByMeterRequest true "Request body"
// @Success 200 {object} dto.GetUsageResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /events/usage/meter [post]
func (h *EventsHandler) GetUsageByMeter(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetUsageByMeterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	req.StartTime, req.EndTime, err = validateStartAndEndTime(req.StartTime, req.EndTime)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	result, err := h.eventService.GetUsageByMeter(ctx, &req)
	if err != nil {
		c.Error(err)
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
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /events/usage [post]
func (h *EventsHandler) GetUsage(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	req.StartTime, req.EndTime, err = validateStartAndEndTime(req.StartTime, req.EndTime)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	result, err := h.eventService.GetUsage(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}

	response := dto.FromAggregationResult(result)
	c.JSON(http.StatusOK, response)
}

func (h *EventsHandler) GetEvents(c *gin.Context) {
	ctx := c.Request.Context()
	externalCustomerID := c.Query("external_customer_id")
	eventName := c.Query("event_name")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	iterFirstKey := c.Query("iter_first_key")
	iterLastKey := c.Query("iter_last_key")
	eventID := c.Query("event_id")
	propertyFiltersStr := c.Query("property_filters")
	source := c.Query("source")
	sort := c.Query("sort")
	order := c.Query("order")

	// Parse property filters from query string (format: key1:value1,value2;key2:value3)
	propertyFilters := make(map[string][]string)
	if propertyFiltersStr != "" {
		filterGroups := strings.Split(propertyFiltersStr, ";")
		for _, group := range filterGroups {
			parts := strings.Split(group, ":")
			if len(parts) == 2 {
				key := parts[0]
				values := strings.Split(parts[1], ",")
				propertyFilters[key] = values
			}
		}
	}

	// Parse pagination parameters
	pageSize := 50
	if size := c.Query("page_size"); size != "" {
		if parsed, err := strconv.Atoi(size); err == nil {
			if parsed > 0 && parsed <= 50 {
				pageSize = parsed
			}
		}
	}

	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Parse count_total parameter
	countTotal := false
	if countTotalStr := c.Query("count_total"); countTotalStr != "" {
		if parsed, err := strconv.ParseBool(countTotalStr); err == nil {
			countTotal = parsed
		}
	}

	startTime, endTime, err := parseStartAndEndTime(startTimeStr, endTimeStr)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	events, err := h.eventService.GetEvents(ctx, &dto.GetEventsRequest{
		ExternalCustomerID: externalCustomerID,
		EventName:          eventName,
		EventID:            eventID,
		StartTime:          startTime,
		EndTime:            endTime,
		PageSize:           pageSize,
		IterFirstKey:       iterFirstKey,
		IterLastKey:        iterLastKey,
		PropertyFilters:    propertyFilters,
		Offset:             offset,
		CountTotal:         countTotal,
		Source:             source,
		Sort:               lo.Ternary(sort != "", &sort, nil),
		Order:              lo.Ternary(order != "", &order, nil),
	})
	if err != nil {
		h.log.Error("Failed to get events", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, events)
}

// @Summary List raw events
// @Description Retrieve raw events with pagination and filtering
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetEventsRequest true "Request body"
// @Success 200 {object} dto.GetEventsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /events/query [post]
func (h *EventsHandler) QueryEvents(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetEventsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	req.StartTime, req.EndTime, err = validateStartAndEndTime(req.StartTime, req.EndTime)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	events, err := h.eventService.GetEvents(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, events)
}

// @Summary Get usage analytics
// @Description Retrieve comprehensive usage analytics with filtering, grouping, and time-series data
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageAnalyticsRequest true "Request body"
// @Success 200 {object} dto.GetUsageAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse
// @Router /events/analytics [post]
func (h *EventsHandler) GetUsageAnalytics(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetUsageAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	req.StartTime, req.EndTime, err = validateStartAndEndTime(req.StartTime, req.EndTime)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	// Call the service to get detailed analytics
	response, err := h.eventPostProcessingService.GetDetailedUsageAnalytics(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *EventsHandler) GetUsageAnalyticsV2(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetUsageAnalyticsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	req.StartTime, req.EndTime, err = validateStartAndEndTime(req.StartTime, req.EndTime)
	if err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	// Call the service to get detailed analytics
	response, err := h.featureUsageTrackingService.GetDetailedUsageAnalytics(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
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
		startTime = endTime.AddDate(0, 0, -7)
	}

	if endTime.Before(startTime) {
		return time.Time{}, time.Time{}, errors.New("end time must be after start time")
	}

	// Ensure times are in UTC
	startTime = startTime.UTC()
	endTime = endTime.UTC()

	return startTime, endTime, nil
}
