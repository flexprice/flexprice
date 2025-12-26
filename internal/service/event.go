package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/publisher"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/sourcegraph/conc/pool"
)

type EventService interface {
	CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error
	BulkCreateEvents(ctx context.Context, createEventRequest *dto.BulkIngestEventRequest) error
	GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error)
	GetUsageByMeter(ctx context.Context, getUsageByMeterRequest *dto.GetUsageByMeterRequest) (*events.AggregationResult, error)
	BulkGetUsageByMeter(ctx context.Context, req []*dto.GetUsageByMeterRequest) (map[string]*events.AggregationResult, error)
	GetUsageByMeterWithFilters(ctx context.Context, req *dto.GetUsageByMeterRequest, filterGroups map[string]map[string][]string) ([]*events.AggregationResult, error)
	GetEvents(ctx context.Context, req *dto.GetEventsRequest) (*dto.GetEventsResponse, error)
	GetMonitoringData(ctx context.Context, req *dto.GetMonitoringDataRequest) (*dto.GetMonitoringDataResponse, error)
	MonitorKafkaLag(ctx context.Context) error
}

type eventService struct {
	eventRepo events.Repository
	meterRepo meter.Repository
	publisher publisher.EventPublisher
	logger    *logger.Logger
	config    *config.Configuration
}

func NewEventService(
	eventRepo events.Repository,
	meterRepo meter.Repository,
	publisher publisher.EventPublisher,
	logger *logger.Logger,
	config *config.Configuration,
) EventService {
	return &eventService{
		eventRepo: eventRepo,
		meterRepo: meterRepo,
		publisher: publisher,
		logger:    logger,
		config:    config,
	}
}

func (s *eventService) CreateEvent(ctx context.Context, createEventRequest *dto.IngestEventRequest) error {
	if err := createEventRequest.Validate(); err != nil {
		return err
	}

	event := createEventRequest.ToEvent(ctx)

	if err := s.publisher.Publish(ctx, event); err != nil {
		// Log the error but don't fail the request
		s.logger.With(
			"event_id", event.ID,
			"error", err,
		).Error("failed to publish event")
	}

	createEventRequest.EventID = event.ID
	return nil
}

// CreateBulkEvents creates multiple events in a single operation
func (s *eventService) BulkCreateEvents(ctx context.Context, events *dto.BulkIngestEventRequest) error {
	if len(events.Events) == 0 {
		return nil
	}

	// publish events to Kafka for downstream processing
	for _, event := range events.Events {
		if err := s.CreateEvent(ctx, event); err != nil {
			return err
		}
	}

	return nil
}

func (s *eventService) GetUsage(ctx context.Context, getUsageRequest *dto.GetUsageRequest) (*events.AggregationResult, error) {
	if err := getUsageRequest.Validate(); err != nil {
		return nil, err
	}

	result, err := s.eventRepo.GetUsage(ctx, getUsageRequest.ToUsageParams())
	if err != nil {
		return nil, err
	}

	result.PriceID = getUsageRequest.PriceID
	result.MeterID = getUsageRequest.MeterID
	return result, nil
}

func (s *eventService) GetUsageByMeter(ctx context.Context, req *dto.GetUsageByMeterRequest) (*events.AggregationResult, error) {
	var m *meter.Meter
	var err error
	if req.Meter == nil {
		m, err = s.meterRepo.GetMeter(ctx, req.MeterID)
		if err != nil {
			return nil, err
		}
	} else {
		m = req.Meter
	}

	getUsageRequest := dto.GetUsageRequest{
		ExternalCustomerID: req.ExternalCustomerID,
		CustomerID:         req.CustomerID,
		EventName:          m.EventName,
		PropertyName:       m.Aggregation.Field,
		AggregationType:    m.Aggregation.Type,
		StartTime:          req.StartTime,
		WindowSize:         req.WindowSize,
		EndTime:            req.EndTime,
		Filters:            req.Filters,
		PriceID:            req.PriceID,
		MeterID:            req.MeterID,
		BillingAnchor:      req.BillingAnchor,
	}

	// Pass the multiplier from meter configuration if it's a SUM_WITH_MULTIPLIER aggregation
	if m.Aggregation.Type == types.AggregationSumWithMultiplier {
		getUsageRequest.Multiplier = m.Aggregation.Multiplier
	}

	// Pass the bucket_size from meter configuration if it's a MAX aggregation with bucket_size set
	if m.IsBucketedMaxMeter() {
		getUsageRequest.BucketSize = m.Aggregation.BucketSize
	}

	// Pass the bucket_size from meter configuration if it's a SUM aggregation with bucket_size set
	if m.IsBucketedSumMeter() {
		getUsageRequest.BucketSize = m.Aggregation.BucketSize
	}

	usage, err := s.GetUsage(ctx, &getUsageRequest)
	if err != nil {
		return nil, err
	}

	if m.ResetUsage == types.ResetUsageNever {
		getHistoricUsageRequest := getUsageRequest
		getHistoricUsageRequest.StartTime = time.Time{}
		getHistoricUsageRequest.EndTime = req.StartTime
		getHistoricUsageRequest.WindowSize = ""

		historicUsage, err := s.GetUsage(ctx, &getHistoricUsageRequest)
		if err != nil {
			return nil, err
		}

		return s.combineResults(historicUsage, usage, m), nil
	}
	return usage, nil
}

// BulkGetUsageByMeter gets usage for multiple meters in parallel using the conc library
func (s *eventService) BulkGetUsageByMeter(ctx context.Context, req []*dto.GetUsageByMeterRequest) (map[string]*events.AggregationResult, error) {
	if len(req) == 0 {
		return make(map[string]*events.AggregationResult), nil
	}
	sentrySvc := sentry.NewSentryService(s.config, s.logger)

	// Get configuration values or use defaults
	maxWorkers := 5
	timeoutDuration := 3000 * time.Millisecond

	// Log the configuration being used
	s.logger.With(
		"max_workers", maxWorkers,
		"per_meter_timeout_ms", timeoutDuration.Milliseconds(),
		"request_count", len(req),
	).Info("starting parallel meter usage processing")

	// Create result map with mutex for safe concurrent access
	results := make(map[string]*events.AggregationResult)
	var resultsMu sync.Mutex

	// Track which meters succeeded and which failed
	successCount := 0
	failureCount := 0
	var countMu sync.Mutex

	// Create a pool with maximum concurrency and error handling
	p := pool.New().
		WithContext(ctx).
		WithMaxGoroutines(maxWorkers)

	// Process each meter request in parallel
	for i, r := range req {
		r := r // Capture for goroutine
		meterIdx := i
		meterID := r.MeterID

		p.Go(func(ctx context.Context) error {
			// For Sentry, follow concurrency best practices
			// 1. Clone the hub for this goroutine
			// 2. Create a transaction specifically for this meter

			// Create a new transaction for this specific meter
			var meterSpanFinisher *sentry.SpanFinisher

			// Create a description for this operation that includes meter details
			operationName := "BulkGetUsageByMeter"
			params := map[string]interface{}{
				"meter_id":    meterID,
				"meter_index": meterIdx,
			}

			// Start a repository span for this meter operation
			span, spanCtx := sentrySvc.StartRepositorySpan(ctx, "GetUsageByMeter", operationName, params)
			if span != nil {
				ctx = spanCtx
				meterSpanFinisher = &sentry.SpanFinisher{Span: span}
				defer meterSpanFinisher.Finish()
			}

			// Check if context is already canceled before making the request
			if ctx.Err() != nil {
				return ctx.Err()
			}

			// Record the start time for this meter
			processingStart := time.Now()

			// Create a timeout context for this specific request
			reqCtx, reqCancel := context.WithTimeout(ctx, timeoutDuration)
			defer reqCancel()

			s.logger.With(
				"meter_id", meterID,
				"price_id", r.PriceID,
				"meter_index", meterIdx,
			).Debug("starting meter usage request")

			result, err := s.GetUsageByMeter(reqCtx, r)

			// Record processing time
			processingDuration := time.Since(processingStart)

			if err != nil {
				// Track failure count
				countMu.Lock()
				failureCount++
				countMu.Unlock()

				s.logger.With(
					"meter_id", meterID,
					"price_id", r.PriceID,
					"meter_index", meterIdx,
					"error", err,
					"processing_time_ms", processingDuration.Milliseconds(),
				).Warn("failed to get meter usage")

				// Capture the error in Sentry if enabled
				if sentrySvc != nil && sentrySvc.IsEnabled() {
					sentrySvc.CaptureException(err)

					// Add breadcrumb about the failure
					sentrySvc.AddBreadcrumb("meter_error", fmt.Sprintf("Failed to get usage for meter %s", meterID), map[string]interface{}{
						"meter_id": meterID,
						"price_id": r.PriceID,
						"error":    err.Error(),
					})
				}

				return err
			}

			// Safely store result in map
			resultsMu.Lock()
			results[r.PriceID] = result
			resultsMu.Unlock()

			countMu.Lock()
			successCount++
			countMu.Unlock()

			s.logger.With(
				"meter_id", meterID,
				"price_id", result.PriceID,
				"meter_index", meterIdx,
				"processing_time_ms", processingDuration.Milliseconds(),
			).Debug("completed meter usage request")

			return nil
		})
	}

	// Wait for all tasks to complete or context to timeout
	err := p.Wait()

	// Log statistics about the operation
	s.logger.With(
		"success_count", successCount,
		"failure_count", failureCount,
		"total_meters", len(req),
	).Debug("completed parallel meter usage processing")

	// If any goroutine failed or timed out, return an error
	if err != nil {
		return results, fmt.Errorf("one or more meter usage requests failed: %w", err)
	}

	// Defensive check: if failureCount > 0, return error even if p.Wait() didn't return one
	if failureCount > 0 {
		return results, fmt.Errorf("one or more meter usage requests failed: %d out of %d meters failed", failureCount, len(req))
	}

	return results, nil
}

// GetUsageByMeterWithFilters returns usage for a meter with specific filters on top of the meter as defined in the price
// TODO : deprecate this flow completely as we now allow only one meter per price without filters
func (s *eventService) GetUsageByMeterWithFilters(ctx context.Context, req *dto.GetUsageByMeterRequest, filterGroups map[string]map[string][]string) ([]*events.AggregationResult, error) {
	m, err := s.meterRepo.GetMeter(ctx, req.MeterID)
	if err != nil {
		return nil, err
	}

	meterFilters := make(map[string][]string)
	for _, filter := range m.Filters {
		meterFilters[filter.Key] = filter.Values
	}

	// Extract and sort priceIDs for stable ordering
	priceIDs := make([]string, 0, len(filterGroups))
	for priceID := range filterGroups {
		priceIDs = append(priceIDs, priceID)
	}
	sort.Strings(priceIDs)

	prioritizedGroups := make([]events.FilterGroup, 0, len(filterGroups))
	// Assign priority deterministically
	for i, priceID := range priceIDs {
		priority := len(priceIDs) - i
		prioritizedGroups = append(prioritizedGroups, events.FilterGroup{
			ID:       priceID,
			Priority: priority,
			Filters:  filterGroups[priceID],
		})
	}

	params := &events.UsageWithFiltersParams{
		UsageParams: &events.UsageParams{
			EventName:          m.EventName,
			PropertyName:       m.Aggregation.Field,
			AggregationType:    m.Aggregation.Type,
			ExternalCustomerID: req.ExternalCustomerID,
			StartTime:          req.StartTime,
			EndTime:            req.EndTime,
			Filters:            meterFilters,
		},
		FilterGroups: prioritizedGroups,
	}

	results, err := s.eventRepo.GetUsageWithFilters(ctx, params)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		s.logger.Debugw("no usage found for meter with filters",
			"meter_id", m.ID,
			"filter_groups", len(filterGroups))
		return results, nil
	}

	return results, nil
}

func (s *eventService) combineResults(historicUsage, currentUsage *events.AggregationResult, m *meter.Meter) *events.AggregationResult {
	var totalValue decimal.Decimal

	if historicUsage != nil {
		totalValue = totalValue.Add(historicUsage.Value)
	}

	if currentUsage != nil {
		totalValue = totalValue.Add(currentUsage.Value)
	}

	return &events.AggregationResult{
		Value:     totalValue,
		Results:   currentUsage.Results,
		EventName: m.EventName,
		Type:      m.Aggregation.Type,
		PriceID:   currentUsage.PriceID,
		MeterID:   currentUsage.MeterID,
	}
}

func (s *eventService) GetEvents(ctx context.Context, req *dto.GetEventsRequest) (*dto.GetEventsResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Set up pagination params
	var useOffsetPagination bool

	if req.IterFirstKey != "" || req.IterLastKey != "" {
		useOffsetPagination = false
	} else {
		useOffsetPagination = true
	}

	// Initialize pageSize from request
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	iterFirst, err := parseEventIteratorToStruct(req.IterFirstKey)
	if err != nil {
		return nil, err
	}

	iterLast, err := parseEventIteratorToStruct(req.IterLastKey)
	if err != nil {
		return nil, err
	}

	// Set up params for repository call
	params := &events.GetEventsParams{
		ExternalCustomerID: req.ExternalCustomerID,
		EventName:          req.EventName,
		EventID:            req.EventID,
		StartTime:          req.StartTime,
		EndTime:            req.EndTime,
		PropertyFilters:    req.PropertyFilters,
		Source:             req.Source,
		Sort:               req.Sort,
		Order:              req.Order,
		CountTotal:         true,
	}

	if !useOffsetPagination {
		params.IterFirst = iterFirst
		params.IterLast = iterLast
		params.PageSize = pageSize + 1 // +1 to check for more results
	} else {
		params.PageSize = pageSize
		params.Offset = offset
	}

	eventsList, totalCount, err := s.eventRepo.GetEvents(ctx, params)
	if err != nil {
		return nil, err
	}

	response := &dto.GetEventsResponse{
		Events: make([]dto.Event, 0, len(eventsList)),
	}
	response.TotalCount = totalCount

	// Handle cursor-based pagination results
	if !useOffsetPagination {
		hasMore := len(eventsList) > pageSize
		if hasMore {
			eventsList = eventsList[:pageSize]
		}
		response.HasMore = hasMore

		if len(eventsList) > 0 {
			firstEvent := eventsList[0]
			lastEvent := eventsList[len(eventsList)-1]
			response.IterFirstKey = createEventIteratorKey(firstEvent.Timestamp, firstEvent.ID)

			if hasMore {
				response.IterLastKey = createEventIteratorKey(lastEvent.Timestamp, lastEvent.ID)
			}
		}
	} else {
		if len(eventsList) > 0 {
			response.IterFirstKey = createEventIteratorKey(eventsList[0].Timestamp, eventsList[0].ID)
			response.IterLastKey = createEventIteratorKey(eventsList[len(eventsList)-1].Timestamp, eventsList[len(eventsList)-1].ID)
		}

		if totalCount > uint64(pageSize) && uint64(offset+pageSize) < totalCount {
			response.HasMore = true
		}

		response.Offset = offset + pageSize
	}

	// Add events to response
	for _, event := range eventsList {
		response.Events = append(response.Events, dto.Event{
			ID:                 event.ID,
			ExternalCustomerID: event.ExternalCustomerID,
			CustomerID:         event.CustomerID,
			EventName:          event.EventName,
			Timestamp:          event.Timestamp,
			Properties:         event.Properties,
			Source:             event.Source,
			EnvironmentID:      event.EnvironmentID,
		})
	}

	return response, nil
}

func parseEventIteratorToStruct(key string) (*events.EventIterator, error) {
	if key == "" {
		return nil, nil
	}

	parts := strings.Split(key, "::")
	if len(parts) != 2 {
		return nil, ierr.NewError("invalid cursor key format").
			WithHint("Invalid cursor key format").
			Mark(ierr.ErrValidation)
	}

	timestampNanoseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid timestamp while parsing cursor").
			Mark(ierr.ErrValidation)
	}

	timestamp := time.Unix(0, timestampNanoseconds)

	return &events.EventIterator{
		Timestamp: timestamp,
		ID:        parts[1],
	}, nil
}

func createEventIteratorKey(timestamp time.Time, id string) string {
	return fmt.Sprintf("%d::%s", timestamp.UnixNano(), id)
}

// MonitorKafkaLag monitors Kafka consumer lag for event consumption and post-processing pipelines.
// It creates Sentry monitoring spans to track lag metrics for alerting and observability.
func (s *eventService) MonitorKafkaLag(ctx context.Context) error {
	sentrySvc := sentry.NewSentryService(s.config, s.logger)
	kafkaMonitoring := kafka.NewMonitoringService(s.config, s.logger)

	// Get Kafka configuration for the current tenant
	eventConsumptionTopic, eventConsumptionConsumerGroup, eventPostProcessingTopic, eventPostProcessingConsumerGroup := s.getKafkaConsumerConfig(ctx)

	// Monitor event consumption pipeline lag
	if err := s.monitorConsumerLag(
		ctx,
		sentrySvc,
		kafkaMonitoring,
		eventConsumptionTopic,
		eventConsumptionConsumerGroup,
		"kafka.lag.event_consumption",
	); err != nil {
		s.logger.Warnw("failed to monitor event consumption lag",
			"error", err,
			"topic", eventConsumptionTopic,
			"consumer_group", eventConsumptionConsumerGroup)
	}

	// Monitor event post-processing pipeline lag
	if err := s.monitorConsumerLag(
		ctx,
		sentrySvc,
		kafkaMonitoring,
		eventPostProcessingTopic,
		eventPostProcessingConsumerGroup,
		"kafka.lag.event_post_processing",
	); err != nil {
		s.logger.Warnw("failed to monitor event post-processing lag",
			"error", err,
			"topic", eventPostProcessingTopic,
			"consumer_group", eventPostProcessingConsumerGroup)
	}

	return nil
}

// monitorConsumerLag retrieves and reports consumer lag metrics for a specific Kafka topic and consumer group.
// It creates a Sentry monitoring span with lag details for observability.
func (s *eventService) monitorConsumerLag(
	ctx context.Context,
	sentrySvc *sentry.Service,
	kafkaMonitoring *kafka.MonitoringService,
	topic string,
	consumerGroup string,
	spanName string,
) error {
	// Retrieve consumer lag metrics
	lag, err := kafkaMonitoring.GetConsumerLag(ctx, topic, consumerGroup)
	if err != nil {
		return err
	}

	// Create monitoring span for lag tracking
	spanParams := map[string]interface{}{
		"topic":          topic,
		"consumer_group": consumerGroup,
		"total_lag":      lag.TotalLag,
	}

	span, spanCtx := sentrySvc.StartKafkaLagMonitoringSpan(ctx, spanName, spanParams)
	if span != nil {
		defer span.Finish()
	}

	s.logger.Infow("kafka lag monitored",
		"topic", topic,
		"consumer_group", consumerGroup,
		"total_lag", lag.TotalLag,
		"span_name", spanName)

	// Use the spanCtx to ensure proper context propagation
	_ = spanCtx

	return nil
}

func (s *eventService) GetMonitoringData(ctx context.Context, req *dto.GetMonitoringDataRequest) (*dto.GetMonitoringDataResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Create Kafka monitoring service
	kafkaMonitoring := kafka.NewMonitoringService(s.config, s.logger)

	// Get total event count with optional windowed time-series data
	eventCountResult, err := s.eventRepo.GetTotalEventCount(ctx, req.StartTime, req.EndTime, req.WindowSize)
	if err != nil {
		s.logger.Warnw("failed to get total event count",
			"error", err,
			"start_time", req.StartTime,
			"end_time", req.EndTime,
			"window_size", req.WindowSize)
		// Continue with zero count on error
		eventCountResult = &events.EventCountResult{TotalCount: 0, Points: []events.EventCountPoint{}}
	}

	// Get tenant and environment from context
	// Note: Consumer groups might be tenant/environment specific in production
	// For now, using global consumer groups from config
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	s.logger.Infow("fetching monitoring data",
		"tenant_id", tenantID,
		"environment_id", envID,
		"event_consumption_group", s.config.EventProcessing.ConsumerGroup,
		"event_post_processing_group", s.config.EventPostProcessing.ConsumerGroup)

	// Get the appropriate consumer groups and topics based on tenant configuration
	eventConsumptionTopic, eventConsumptionConsumerGroup, eventPostProcessingTopic, eventPostProcessingConsumerGroup := s.getKafkaConsumerConfig(ctx)

	// Calculate Kafka consumer lag for event consumption
	eventConsumptionLag, err := kafkaMonitoring.GetConsumerLag(ctx,
		eventConsumptionTopic,
		eventConsumptionConsumerGroup)
	if err != nil {
		s.logger.Warnw("failed to get event consumption consumer lag",
			"error", err,
			"topic", eventConsumptionTopic,
			"consumer_group", eventConsumptionConsumerGroup)
		// Continue with zero lag on error
		eventConsumptionLag = &kafka.ConsumerLag{TotalLag: 0}
	}

	// Calculate Kafka consumer lag for event post processing
	eventPostProcessingLag, err := kafkaMonitoring.GetConsumerLag(ctx,
		eventPostProcessingTopic,
		eventPostProcessingConsumerGroup)
	if err != nil {
		s.logger.Warnw("failed to get event post processing consumer lag",
			"error", err,
			"topic", eventPostProcessingTopic,
			"consumer_group", eventPostProcessingConsumerGroup)
		// Continue with zero lag on error
		eventPostProcessingLag = &kafka.ConsumerLag{TotalLag: 0}
	}

	// Convert domain event count points to DTO points
	var dtoPoints []dto.EventCountPoint
	for _, point := range eventCountResult.Points {
		dtoPoints = append(dtoPoints, dto.EventCountPoint{
			Timestamp:  point.Timestamp,
			EventCount: point.EventCount,
		})
	}

	// Build response
	response := &dto.GetMonitoringDataResponse{
		TotalCount:        eventCountResult.TotalCount,
		ConsumptionLag:    eventConsumptionLag.TotalLag,
		PostProcessingLag: eventPostProcessingLag.TotalLag,
		Points:            dtoPoints,
	}

	return response, nil
}

// getKafkaConsumerConfig determines the appropriate Kafka consumer groups and topics
// based on whether the tenant is in the lazy tenants list
func (s *eventService) getKafkaConsumerConfig(ctx context.Context) (
	eventConsumptionTopic string,
	eventConsumptionConsumerGroup string,
	eventPostProcessingTopic string,
	eventPostProcessingConsumerGroup string,
) {
	// Get tenant ID from context
	tenantID := types.GetTenantID(ctx)

	// Check if tenant is in the lazy tenants list
	isLazyTenant := false
	for _, lazyTenantID := range s.config.Kafka.RouteTenantsOnLazyMode {
		if lazyTenantID == tenantID {
			isLazyTenant = true
			break
		}
	}

	// Set event consumption topic and consumer group
	if isLazyTenant {
		eventConsumptionTopic = s.config.EventProcessingLazy.Topic
		eventConsumptionConsumerGroup = s.config.EventProcessingLazy.ConsumerGroup
	} else {
		eventConsumptionTopic = s.config.EventProcessing.Topic
		eventConsumptionConsumerGroup = s.config.EventProcessing.ConsumerGroup
	}

	// Set event post processing topic and consumer group
	if isLazyTenant {
		eventPostProcessingTopic = s.config.FeatureUsageTrackingLazy.Topic
		eventPostProcessingConsumerGroup = s.config.FeatureUsageTrackingLazy.ConsumerGroup
	} else {
		eventPostProcessingTopic = s.config.FeatureUsageTracking.Topic
		eventPostProcessingConsumerGroup = s.config.FeatureUsageTracking.ConsumerGroup
	}

	return eventConsumptionTopic, eventConsumptionConsumerGroup, eventPostProcessingTopic, eventPostProcessingConsumerGroup
}
