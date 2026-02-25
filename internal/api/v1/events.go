package v1

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

type EventsHandler struct {
	eventService                 service.EventService
	eventPostProcessingService   service.EventPostProcessingService
	featureUsageTrackingService  service.FeatureUsageTrackingService
	rawEventsReprocessingService service.RawEventsReprocessingService
	config                       *config.Configuration
	log                          *logger.Logger
}

func NewEventsHandler(eventService service.EventService, eventPostProcessingService service.EventPostProcessingService, featureUsageTrackingService service.FeatureUsageTrackingService, rawEventsReprocessingService service.RawEventsReprocessingService, config *config.Configuration, log *logger.Logger) *EventsHandler {
	return &EventsHandler{
		eventService:                 eventService,
		eventPostProcessingService:   eventPostProcessingService,
		featureUsageTrackingService:  featureUsageTrackingService,
		rawEventsReprocessingService: rawEventsReprocessingService,
		config:                       config,
		log:                          log,
	}
}

// @Summary Ingest event
// @ID ingestEvent
// @Description Use when sending a single usage event from your app (e.g. one API call or one GB stored). Events are processed asynchronously; returns 202 with event_id.
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param event body dto.IngestEventRequest true "Event data"
// @Success 202 {object} map[string]string "message:Event accepted for processing"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

// @Summary Bulk ingest events
// @ID ingestEventsBulk
// @Description Use when batching usage events (e.g. backfill or high-volume ingestion). More efficient than single event calls; returns 202 when accepted.
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param event body dto.BulkIngestEventRequest true "Event data"
// @Success 202 {object} map[string]string "message:Event accepted for processing"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @ID getUsageByMeter
// @Description Use when showing usage for a specific meter (e.g. dashboard or overage check). Supports time range, filters, and grouping by customer or subscription.
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageByMeterRequest true "Request body"
// @Success 200 {object} dto.GetUsageResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Resource not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @ID getUsageStatistics
// @Description Use when building usage reports or dashboards across events. Supports filters and grouping; defaults to last 7 days if no range provided.
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageRequest true "Request body"
// @Success 200 {object} dto.GetUsageResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @ID listRawEvents
// @Description Use when debugging ingestion or exporting raw event data (e.g. support or audit). Returns a paginated list; supports time range and sorting.
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetEventsRequest true "Request body"
// @Success 200 {object} dto.GetEventsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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
// @ID getUsageAnalytics
// @Description Use when building analytics views (e.g. usage by feature or customer over time). Supports filtering, grouping, and time-series breakdown.
// @Tags Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body dto.GetUsageAnalyticsRequest true "Request body"
// @Success 200 {object} dto.GetUsageAnalyticsResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
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

	// Call the appropriate service based on feature flag
	var response *dto.GetUsageAnalyticsResponse

	if !h.config.FeatureFlag.EnableFeatureUsageForAnalytics || h.config.FeatureFlag.ForceV1ForTenant == types.GetTenantID(ctx) {
		// Use v1 (eventPostProcessingService) when flag is disabled
		response, err = h.eventPostProcessingService.GetDetailedUsageAnalytics(ctx, &req)
	} else {
		// Use v2 (featureUsageTrackingService) when flag is enabled
		response, err = h.featureUsageTrackingService.GetDetailedUsageAnalytics(ctx, &req)
	}

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

func (h *EventsHandler) GetMonitoringData(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.GetMonitoringDataRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the query parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// Call the service to get monitoring data
	response, err := h.eventService.GetMonitoringData(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// @Summary Get Hugging Face inference data
// @ID getHuggingfaceInferenceData
// @Description Use when fetching Hugging Face inference usage or billing data (e.g. for HF-specific reporting or reconciliation).
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} dto.GetHuggingFaceBillingDataResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /events/huggingface-inference [post]
func (h *EventsHandler) GetHuggingFaceBillingData(c *gin.Context) {
	ctx := c.Request.Context()
	var err error

	var req dto.GetHuggingFaceBillingDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.featureUsageTrackingService.GetHuggingFaceBillingData(ctx, &req)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *EventsHandler) BenchmarkV1(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.BenchmarkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	// Convert to event
	event := req.ToEvent(ctx)

	result, err := h.featureUsageTrackingService.BenchmarkPrepareV1(ctx, event)
	if err != nil {
		h.log.Error("Failed to benchmark V1", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *EventsHandler) BenchmarkV2(c *gin.Context) {
	ctx := c.Request.Context()

	var req dto.BenchmarkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request payload").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		c.Error(err)
		return
	}

	// Convert to event
	event := req.ToEvent(ctx)

	result, err := h.featureUsageTrackingService.BenchmarkPrepareV2(ctx, event)
	if err != nil {
		h.log.Error("Failed to benchmark V2", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}

// @Summary Get event
// @ID getEvent
// @Description Use when debugging a specific event (e.g. why it failed or how it was aggregated). Includes processing status and debug info.
// @Tags Events
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Event ID"
// @Success 200 {object} dto.GetEventByIDResponse
// @Failure 404 {object} ierr.ErrorResponse
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /events/{id} [get]
func (h *EventsHandler) GetEventByID(c *gin.Context) {
	ctx := c.Request.Context()
	eventID := c.Param("id")

	if eventID == "" {
		c.Error(ierr.NewError("event ID is required").
			WithHint("Please provide a valid event ID").
			Mark(ierr.ErrValidation))
		return
	}

	response, err := h.featureUsageTrackingService.DebugEvent(ctx, eventID)
	if err != nil {
		h.log.Error("Failed to debug event", "error", err, "event_id", eventID)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *EventsHandler) ReprocessEvents(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.ReprocessEventsRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	result, err := h.featureUsageTrackingService.TriggerReprocessEventsWorkflow(ctx, &req)
	if err != nil {
		h.log.Error("Failed to trigger reprocess events workflow", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *EventsHandler) ReprocessEventsInternal(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.InternalReprocessEventsRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	result, err := h.featureUsageTrackingService.TriggerReprocessEventsWorkflowInternal(ctx, &req)
	if err != nil {
		h.log.Error("Failed to trigger internal reprocess events workflow", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *EventsHandler) ReprocessRawEvents(c *gin.Context) {
	ctx := c.Request.Context()
	var req dto.ReprocessRawEventsRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		h.log.Error("Failed to bind JSON", "error", err)
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
		return
	}

	if err := req.Validate(); err != nil {
		h.log.Error("Failed to validate request", "error", err)
		c.Error(err)
		return
	}

	result, err := h.rawEventsReprocessingService.TriggerReprocessRawEventsWorkflow(ctx, &service.ReprocessRawEventsRequest{
		ExternalCustomerID: req.ExternalCustomerID,
		EventName:          req.EventName,
		StartDate:          req.StartDate,
		EndDate:            req.EndDate,
		BatchSize:          req.BatchSize,
	})
	if err != nil {
		h.log.Error("Failed to trigger reprocess raw events workflow", "error", err)
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, result)
}
