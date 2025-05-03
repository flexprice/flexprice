# Event Post-Processing and Pre-Computation Design

## Overview

This document outlines the design for a new approach to event processing in the Flexprice system. The goal is to optimize real-time usage calculation and wallet balance computation by pre-computing costs at the time of event ingestion, rather than calculating them on-demand.

## Current System Limitations

As identified in the [usage-metering-challenges.md](./usage-metering-challenges.md) document, the current system faces several challenges:

1. **Real-Time Balance Calculation Bottlenecks**:
   - Each wallet balance check requires complex queries and calculations
   - Multiple database calls per balance check
   - No pre-computation or caching strategy

2. **Query Efficiency Issues**:
   - Complex ClickHouse queries are constructed on-demand
   - Multi-level CTEs and nested queries
   - Resource-intensive aggregations

3. **Tight Coupling**:
   - Wallet balance directly dependent on usage calculations
   - Changes in one component affect others

## Proposed Solution

We propose a post-processing pipeline that processes events after they're ingested into ClickHouse, pre-computes their financial impact, and stores the results for efficient retrieval.

### High-Level Flow

1. Event ingestion remains as-is, but we add a secondary Kafka topic for post-processing
2. A dedicated consumer reads from this topic and processes events
3. Processing includes customer lookup, subscription identification, meter matching, and cost calculation
4. Results are stored in a new `events_processed` ClickHouse table, which maintains the original event data plus cost information
5. Usage and wallet balance queries are simplified to use pre-computed costs

## Detailed Implementation Plan

### 1. ProcessedEvent Schema

We'll create a new ClickHouse table that maintains all the original event fields plus adds cost computation context:

```sql
CREATE TABLE IF NOT EXISTS events_processed (
    -- Original event fields
    id String,
    tenant_id String,
    external_customer_id String,
    customer_id String,
    event_name String,
    source String,
    timestamp DateTime64(3),
    ingested_at DateTime64(3),
    properties String,
    
    -- Additional processing fields
    processed_at DateTime64(3) DEFAULT now(),
    environment_id String,
    subscription_id String,
    price_id String,
    meter_id String,
    aggregation_field String,
    aggregation_field_value String,
    quantity Decimal(18,9),
    cost Decimal(18,9),
    currency String,
    version UInt32 DEFAULT 1,
    
    -- Ensure required fields
    CONSTRAINT check_event_id CHECK (id != '')
) ENGINE = ReplacingMergeTree(version)
PARTITION BY toYYYYMM(timestamp)
ORDER BY (id, tenant_id, external_customer_id, customer_id, event_name, timestamp)
SETTINGS index_granularity = 8192;
```

### 2. EventPostProcessing Service

Follow the same approach as the onboarding system, creating a dedicated service for event post-processing:

```go
// EventPostProcessingService handles post-processing operations for metered events
type EventPostProcessingService interface {
    // Publish an event for post-processing
    PublishEvent(ctx context.Context, event *events.Event) error
    
    // Register message handler with the router
    RegisterHandler(router *pubsubRouter.Router)
    
    // Query method for processed events with different filters
    GetUsageSummary(ctx context.Context, params *UsageSummaryParams) (decimal.Decimal, error)
    
    // Reprocess events for a specific customer or subscription
    // Used when a customer or subscription is created after events have been received
    ReprocessEvents(ctx context.Context, customerID, subscriptionID string) error
}

// Implementation
type eventPostProcessingService struct {
    ServiceParams
    pubSub pubsub.PubSub
    processedEventRepo events.ProcessedEventRepository
}

// UsageSummaryParams defines the filters for querying usage
type UsageSummaryParams struct {
    StartTime      time.Time
    EndTime        time.Time
    CustomerID     string
    SubscriptionID string
    MeterID        string
    PriceID        string
}

const (
    EventsPostProcessingTopic = "events_post_processing"
)
```

### 3. Publishing Events

After regular event ingestion, publish to the post-processing topic:

```go
func (s *eventPostProcessingService) PublishEvent(ctx context.Context, event *events.Event) error {
    // Create message payload
    payload, err := json.Marshal(event)
    if err != nil {
        return ierr.WithError(err).
            WithHint("Failed to marshal event for post-processing").
            Mark(ierr.ErrValidation)
    }
    
    // Create watermill message
    messageID := watermill.NewUUID()
    msg := message.NewMessage(messageID, payload)
    
    // Set metadata
    msg.Metadata.Set("tenant_id", types.GetTenantID(ctx))
    
    s.Logger.Debugw("publishing event for post-processing",
        "event_id", event.ID, 
        "event_name", event.EventName,
        "message_id", messageID,
    )
    
    // Publish to post-processing topic
    if err := s.pubSub.Publish(ctx, EventsPostProcessingTopic, msg); err != nil {
        return ierr.WithError(err).
            WithHint("Failed to publish event for post-processing").
            Mark(ierr.ErrInternal)
    }
    
    return nil
}
```

### 4. Message Handler & Registration

Register a handler for the post-processing topic:

```go
func (s *eventPostProcessingService) RegisterHandler(router *pubsubRouter.Router) {
    router.AddNoPublishHandler(
        "events_post_processing_handler",
        EventsPostProcessingTopic,
        s.pubSub,
        s.processMessage,
    )
}

// Process a single event message
func (s *eventPostProcessingService) processMessage(msg *message.Message) error {
    s.Logger.Debugw("received event for post-processing", "message_uuid", msg.UUID)
    
    // Extract tenant ID from message metadata
    tenantID := msg.Metadata.Get("tenant_id")
    
    // Create a background context with tenant ID
    ctx := context.Background()
    if tenantID != "" {
        ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
    }
    
    // Unmarshal the event
    var event events.Event
    if err := json.Unmarshal(msg.Payload, &event); err != nil {
        s.Logger.Errorw("failed to unmarshal event for post-processing",
            "error", err,
            "message_uuid", msg.UUID,
        )
        return nil // Don't retry on unmarshal errors
    }
    
    // Process the event
    if err := s.processEvent(ctx, &event); err != nil {
        s.Logger.Errorw("failed to process event",
            "error", err,
            "event_id", event.ID,
            "event_name", event.EventName,
        )
        return err // Return error for retry
    }
    
    return nil
}
```

### 5. Event Processing Logic

The core processing function:

```go
func (s *eventPostProcessingService) processEvent(ctx context.Context, event *events.Event) error {
    // Always save the event in processed_events, even if we can't process it fully yet
    processedEvent := &events.ProcessedEvent{
        // Original event fields
        ID:                 event.ID,
        EventName:          event.EventName,
        TenantID:           event.TenantID,
        CustomerID:         event.CustomerID,
        ExternalCustomerID: event.ExternalCustomerID,
        Source:             event.Source,
        Timestamp:          event.Timestamp,
        IngestedAt:         event.IngestedAt,
        Properties:         event.Properties,
        
        // Set processed_at to nil for events we can't fully process yet
        ProcessedAt:        nil,
    }
    
    // 1. Skip processing (but still save) if no external customer ID
    if event.ExternalCustomerID == "" {
        s.Logger.Debugw("saving event without processing due to missing external customer ID", 
            "event_id", event.ID)
        return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
    }
    
    // 2. Lookup customer
    customer, err := s.CustomerService.GetCustomerByLookupKey(ctx, event.ExternalCustomerID)
    if err != nil {
        s.Logger.Warnw("customer not found for event, saving without processing",
            "event_id", event.ID,
            "external_customer_id", event.ExternalCustomerID,
            "error", err,
        )
        return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
    }
    
    // Set the customer ID in the event if it's not already set
    if event.CustomerID == "" {
        event.CustomerID = customer.ID
        processedEvent.CustomerID = customer.ID
    }
    
    // 3. Get active subscriptions
    filter := types.NewSubscriptionFilter()
    filter.CustomerID = customer.ID
    filter.SubscriptionStatusNotIn = []types.SubscriptionStatus{types.SubscriptionStatusCancelled}
    
    subscriptions, err := s.SubscriptionService.ListSubscriptions(ctx, filter)
    if err != nil {
        s.Logger.Errorw("failed to get subscriptions, saving without processing",
            "event_id", event.ID,
            "customer_id", customer.ID,
            "error", err,
        )
        return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
    }
    
    if len(subscriptions.Items) == 0 {
        s.Logger.Debugw("no active subscriptions found for customer, saving without processing",
            "event_id", event.ID,
            "customer_id", customer.ID,
        )
        return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
    }
    
    // 4. Get all meters
    meters, err := s.MeterService.GetAllMeters(ctx)
    if err != nil {
        s.Logger.Errorw("failed to get meters, saving without processing",
            "event_id", event.ID,
            "error", err,
        )
        return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
    }
    
    // 5. Process the event against each subscription
    processedEvents := make([]*events.ProcessedEvent, 0)
    
    for _, subscription := range subscriptions.Items {
        // Get prices for the subscription's plan
        prices, err := s.PriceService.GetPricesByPlanID(ctx, subscription.PlanID)
        if err != nil {
            s.Logger.Errorw("failed to get prices for plan, skipping subscription",
                "event_id", event.ID,
                "plan_id", subscription.PlanID,
                "error", err,
            )
            continue // Skip this subscription but continue with others
        }
        
        // Find meters and prices that match this event
        matches := s.findMatchingPricesForEvent(event, prices.Items, meters.Items)
        
        for _, match := range matches {
            // Only process COUNT and SUM aggregations for now
            if match.Meter.Aggregation.Type != types.AggregationCount && 
               match.Meter.Aggregation.Type != types.AggregationSum {
                s.Logger.Debugw("skipping unsupported aggregation type",
                    "event_id", event.ID,
                    "meter_id", match.Meter.ID,
                    "aggregation_type", match.Meter.Aggregation.Type,
                )
                continue
            }
            
            // Extract quantity based on meter aggregation
            quantity, fieldValue := s.extractQuantityFromEvent(event, match.Meter)
            
            // Calculate cost
            cost := s.PriceService.CalculateCost(ctx, match.Price, quantity)
            
            // Create processed event - using same event ID but creating a new instance
            // for each matching price/meter
            processedEventCopy := &events.ProcessedEvent{
                // Original event fields
                ID:                 event.ID,
                EventName:          event.EventName,
                TenantID:           event.TenantID,
                CustomerID:         event.CustomerID,
                ExternalCustomerID: event.ExternalCustomerID,
                Source:             event.Source,
                Timestamp:          event.Timestamp,
                IngestedAt:         event.IngestedAt,
                Properties:         event.Properties,
                
                // Added context
                EnvironmentID:         subscription.EnvironmentID,
                SubscriptionID:        subscription.ID,
                PriceID:               match.Price.ID,
                MeterID:               match.Meter.ID,
                AggregationField:      match.Meter.Aggregation.Field,
                AggregationFieldValue: fieldValue,
                Quantity:              quantity,
                Cost:                  cost,
                Currency:              match.Price.Currency,
                ProcessedAt:           lo.ToPtr(time.Now().UTC()),
            }
            
            processedEvents = append(processedEvents, processedEventCopy)
        }
    }
    
    // 6. Insert processed events
    if len(processedEvents) > 0 {
        if err := s.processedEventRepo.BulkInsertProcessedEvents(ctx, processedEvents); err != nil {
            s.Logger.Errorw("failed to insert processed events",
                "event_id", event.ID,
                "count", len(processedEvents),
                "error", err,
            )
            return err
        }
        
        s.Logger.Infow("successfully processed event",
            "event_id", event.ID,
            "processed_events", len(processedEvents),
        )
    } else {
        s.Logger.Debugw("no matching prices/meters found for event, saving without processing",
            "event_id", event.ID,
            "event_name", event.EventName,
        )
        return s.processedEventRepo.InsertProcessedEvent(ctx, processedEvent)
    }
    
    return nil
}
```

### 6. Helper Functions

Helper functions for event processing, simplified to only handle COUNT and SUM:

```go
// Find matching prices for an event based on meter configuration and filters
func (s *eventPostProcessingService) findMatchingPricesForEvent(
    event *events.Event, 
    prices []*dto.PriceResponse, 
    meters []*dto.MeterResponse,
) []PriceMatch {
    matches := make([]PriceMatch, 0)
    
    // Find prices with associated meters
    for _, price := range prices {
        if price.Type != types.PRICE_TYPE_USAGE || price.MeterID == "" {
            continue
        }
        
        // Find the meter for this price
        var meter *dto.MeterResponse
        for _, m := range meters {
            if m.ID == price.MeterID {
                meter = m
                break
            }
        }
        
        if meter == nil {
            continue
        }
        
        // Skip if meter doesn't match the event name
        if meter.EventName != event.EventName {
            continue
        }
        
        // Check meter filters
        if !s.checkMeterFilters(event, meter.Filters) {
            continue
        }
        
        // Check price filters with priority handling
        if !s.checkPriceFilters(event, price.FilterValues) {
            continue
        }
        
        // Add to matches
        matches = append(matches, PriceMatch{
            Price: price.Price,
            Meter: meter,
        })
    }
    
    // Sort matches by filter specificity (most specific first)
    sort.Slice(matches, func(i, j int) bool {
        // Calculate priority based on filter count
        priorityI := s.calculatePriority(matches[i].Price.FilterValues)
        priorityJ := s.calculatePriority(matches[j].Price.FilterValues)
        
        if priorityI != priorityJ {
            return priorityI > priorityJ
        }
        
        // Tie-break using price ID for deterministic ordering
        return matches[i].Price.ID < matches[j].Price.ID
    })
    
    return matches
}

// Extract quantity from event based on meter aggregation
// Returns the quantity and the string representation of the field value
func (s *eventPostProcessingService) extractQuantityFromEvent(
    event *events.Event, 
    meter *dto.MeterResponse,
) (decimal.Decimal, string) {
    switch meter.Aggregation.Type {
    case types.AggregationCount:
        // For count, always return 1 and empty string for field value
        return decimal.NewFromInt(1), ""
        
    case types.AggregationSum:
        if meter.Aggregation.Field == "" {
            return decimal.Zero, ""
        }
        
        val, ok := event.Properties[meter.Aggregation.Field]
        if !ok {
            return decimal.Zero, ""
        }
        
        // Convert value to decimal and string
        switch v := val.(type) {
        case float64:
            return decimal.NewFromFloat(v), fmt.Sprintf("%f", v)
        case int64:
            return decimal.NewFromInt(v), fmt.Sprintf("%d", v)
        case int:
            return decimal.NewFromInt(int64(v)), fmt.Sprintf("%d", v)
        case string:
            d, err := decimal.NewFromString(v)
            if err != nil {
                return decimal.Zero, v
            }
            return d, v
        default:
            // Try to convert to string
            str := fmt.Sprintf("%v", v)
            return decimal.Zero, str
        }
        
    default:
        // We're only supporting COUNT and SUM for now
        return decimal.Zero, ""
    }
}
```

### 7. ProcessedEvent Repository

Create a new repository for working with processed events in ClickHouse:

```go
// ProcessedEventRepository interface
type ProcessedEventRepository interface {
    InsertProcessedEvent(ctx context.Context, event *ProcessedEvent) error
    BulkInsertProcessedEvents(ctx context.Context, events []*ProcessedEvent) error
    GetProcessedEvents(ctx context.Context, params *GetProcessedEventsParams) ([]*ProcessedEvent, uint64, error)
    GetUsageSummary(ctx context.Context, params *UsageSummaryParams) (decimal.Decimal, error)
    FindUnprocessedEvents(ctx context.Context, customerID, subscriptionID string) ([]*ProcessedEvent, error)
}

// ProcessedEventRepository implementation
type processedEventRepository struct {
    store  *clickhouse.ClickHouseStore
    logger *logger.Logger
}

func NewProcessedEventRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) ProcessedEventRepository {
    return &processedEventRepository{store: store, logger: logger}
}

// InsertProcessedEvent inserts a single processed event
func (r *processedEventRepository) InsertProcessedEvent(ctx context.Context, event *ProcessedEvent) error {
    query := `
        INSERT INTO events_processed (
            id, tenant_id, external_customer_id, customer_id, event_name, source, 
            timestamp, ingested_at, properties, processed_at, environment_id,
            subscription_id, price_id, meter_id, aggregation_field, 
            aggregation_field_value, quantity, cost, currency
        ) VALUES (
            ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
        )
    `
    
    propertiesJSON, err := json.Marshal(event.Properties)
    if err != nil {
        return ierr.WithError(err).
            WithHint("Failed to marshal event properties").
            Mark(ierr.ErrValidation)
    }
    
    args := []interface{}{
        event.ID,
        event.TenantID,
        event.ExternalCustomerID,
        event.CustomerID,
        event.EventName,
        event.Source,
        event.Timestamp,
        event.IngestedAt,
        string(propertiesJSON),
        event.ProcessedAt,
        event.EnvironmentID,
        event.SubscriptionID,
        event.PriceID,
        event.MeterID,
        event.AggregationField,
        event.AggregationFieldValue,
        event.Quantity,
        event.Cost,
        event.Currency,
    }
    
    err = r.store.GetConn().Exec(ctx, query, args...)
    if err != nil {
        return ierr.WithError(err).
            WithHint("Failed to insert processed event").
            Mark(ierr.ErrDatabase)
    }
    
    return nil
}

// BulkInsertProcessedEvents inserts multiple processed events
func (r *processedEventRepository) BulkInsertProcessedEvents(ctx context.Context, events []*ProcessedEvent) error {
    if len(events) == 0 {
        return nil
    }
    
    // Split events in batches of 100
    eventsBatches := lo.Chunk(events, 100)
    
    for _, eventsBatch := range eventsBatches {
        // Prepare batch statement
        batch, err := r.store.GetConn().PrepareBatch(ctx, `
            INSERT INTO events_processed (
                id, tenant_id, external_customer_id, customer_id, event_name, source, 
                timestamp, ingested_at, properties, processed_at, environment_id,
                subscription_id, price_id, meter_id, aggregation_field, 
                aggregation_field_value, quantity, cost, currency
            )
        `)
        if err != nil {
            return ierr.WithError(err).
                WithHint("Failed to prepare batch for processed events").
                Mark(ierr.ErrDatabase)
        }
        
        for _, event := range eventsBatch {
            propertiesJSON, err := json.Marshal(event.Properties)
            if err != nil {
                return ierr.WithError(err).
                    WithHint("Failed to marshal event properties").
                    Mark(ierr.ErrValidation)
            }
            
            err = batch.Append(
                event.ID,
                event.TenantID,
                event.ExternalCustomerID,
                event.CustomerID,
                event.EventName,
                event.Source,
                event.Timestamp,
                event.IngestedAt,
                string(propertiesJSON),
                event.ProcessedAt,
                event.EnvironmentID,
                event.SubscriptionID,
                event.PriceID,
                event.MeterID,
                event.AggregationField,
                event.AggregationFieldValue,
                event.Quantity,
                event.Cost,
                event.Currency,
            )
            
            if err != nil {
                return ierr.WithError(err).
                    WithHint("Failed to append processed event to batch").
                    Mark(ierr.ErrDatabase)
            }
        }
        
        // Send batch
        if err := batch.Send(); err != nil {
            return ierr.WithError(err).
                WithHint("Failed to execute batch insert for processed events").
                Mark(ierr.ErrDatabase)
        }
    }
    
    return nil
}

// GetUsageSummary calculates usage summaries based on pre-computed costs
func (r *processedEventRepository) GetUsageSummary(ctx context.Context, params *UsageSummaryParams) (decimal.Decimal, error) {
    query := `
        SELECT SUM(cost) as total_cost
        FROM events_processed
        WHERE tenant_id = ?
        AND timestamp >= ?
        AND timestamp <= ?
        AND processed_at IS NOT NULL
    `
    args := []interface{}{types.GetTenantID(ctx), params.StartTime, params.EndTime}
    
    // Add filters
    if params.CustomerID != "" {
        query += " AND customer_id = ?"
        args = append(args, params.CustomerID)
    }
    
    if params.SubscriptionID != "" {
        query += " AND subscription_id = ?"
        args = append(args, params.SubscriptionID)
    }
    
    if params.PriceID != "" {
        query += " AND price_id = ?"
        args = append(args, params.PriceID)
    }
    
    if params.MeterID != "" {
        query += " AND meter_id = ?"
        args = append(args, params.MeterID)
    }
    
    // Handle duplicates by using ReplacingMergeTree version field
    // The FINAL modifier ensures that for each set of rows with the same primary key,
    // only the one with the largest version number is returned
    query += " FINAL"
    
    var totalCost decimal.Decimal
    err := r.store.GetConn().QueryRow(ctx, query, args...).Scan(&totalCost)
    if err != nil {
        return decimal.Zero, ierr.WithError(err).
            WithHint("Failed to calculate usage summary").
            Mark(ierr.ErrDatabase)
    }
    
    return totalCost, nil
}

// FindUnprocessedEvents finds events that need to be processed
func (r *processedEventRepository) FindUnprocessedEvents(ctx context.Context, customerID, subscriptionID string) ([]*ProcessedEvent, error) {
    query := `
        SELECT 
            id, tenant_id, external_customer_id, customer_id, 
            event_name, source, timestamp, ingested_at, properties
        FROM events_processed
        WHERE processed_at IS NULL
    `
    args := []interface{}{}
    
    // Add filters
    if customerID != "" {
        query += " AND customer_id = ?"
        args = append(args, customerID)
    }
    
    if len(args) == 0 {
        // We need at least one filter
        return nil, ierr.NewError("at least one filter is required").
            WithHint("At least one filter is required to find unprocessed events").
            Mark(ierr.ErrValidation)
    }
    
    // Execute query
    rows, err := r.store.GetConn().Query(ctx, query, args...)
    if err != nil {
        return nil, ierr.WithError(err).
            WithHint("Failed to query unprocessed events").
            Mark(ierr.ErrDatabase)
    }
    defer rows.Close()
    
    var events []*ProcessedEvent
    for rows.Next() {
        var event ProcessedEvent
        var propertiesJSON string
        
        err := rows.Scan(
            &event.ID,
            &event.TenantID,
            &event.ExternalCustomerID,
            &event.CustomerID,
            &event.EventName,
            &event.Source,
            &event.Timestamp,
            &event.IngestedAt,
            &propertiesJSON,
        )
        if err != nil {
            return nil, ierr.WithError(err).
                WithHint("Failed to scan event").
                Mark(ierr.ErrDatabase)
        }
        
        // Parse properties
        if err := json.Unmarshal([]byte(propertiesJSON), &event.Properties); err != nil {
            return nil, ierr.WithError(err).
                WithHint("Failed to unmarshal properties").
                Mark(ierr.ErrValidation)
        }
        
        events = append(events, &event)
    }
    
    return events, nil
}
```

### 8. Event Reprocessing

To handle events that arrive before customer/subscription creation:

```go
// ReprocessEvents triggers reprocessing of events for a customer or subscription
func (s *eventPostProcessingService) ReprocessEvents(ctx context.Context, customerID, subscriptionID string) error {
    if customerID == "" && subscriptionID == "" {
        return ierr.NewError("either customer_id or subscription_id is required").
            WithHint("Either customer ID or subscription ID is required").
            Mark(ierr.ErrValidation)
    }
    
    // Find unprocessed events
    events, err := s.processedEventRepo.FindUnprocessedEvents(ctx, customerID, subscriptionID)
    if err != nil {
        return err
    }
    
    if len(events) == 0 {
        s.Logger.Infow("no unprocessed events found",
            "customer_id", customerID,
            "subscription_id", subscriptionID,
        )
        return nil
    }
    
    s.Logger.Infow("reprocessing events",
        "customer_id", customerID,
        "subscription_id", subscriptionID,
        "event_count", len(events),
    )
    
    // Process each event
    for _, event := range events {
        if err := s.processEvent(ctx, &events.Event{
            ID:                 event.ID,
            TenantID:           event.TenantID,
            CustomerID:         event.CustomerID,
            ExternalCustomerID: event.ExternalCustomerID,
            EventName:          event.EventName,
            Source:             event.Source,
            Timestamp:          event.Timestamp,
            IngestedAt:         event.IngestedAt,
            Properties:         event.Properties,
        }); err != nil {
            s.Logger.Errorw("failed to reprocess event",
                "event_id", event.ID,
                "error", err,
            )
            // Continue with other events
        }
    }
    
    return nil
}
```

### 9. Updating the Event Pipeline

Modify the existing event handling in `cmd/server/main.go`:

```go
func handleEventConsumption(cfg *config.Configuration, log *logger.Logger, 
                           eventRepo events.Repository, 
                           eventPostProcessingSvc service.EventPostProcessingService,
                           payload []byte) error {
    var event events.Event
    if err := json.Unmarshal(payload, &event); err != nil {
        log.Errorf("Failed to unmarshal event: %v, payload: %s", err, string(payload))
        return err
    }

    log.Debugf("Starting to process event: %+v", event)

    // Insert into ClickHouse
    if err := eventRepo.InsertEvent(context.Background(), &event); err != nil {
        log.Errorf("Failed to insert event: %v, event: %+v", err, event)
        return err
    }

    // Skip billing tenant event logic for simplicity in the initial implementation
    // We'll handle just the main customer events first
    
    // Publish for post-processing
    ctx := context.Background()
    if cfg.Billing.TenantID != "" {
        ctx = context.WithValue(ctx, types.CtxTenantID, event.TenantID)
    }
    
    if err := eventPostProcessingSvc.PublishEvent(ctx, &event); err != nil {
        log.Errorf("Failed to publish event for post-processing: %v, event: %+v", err, event)
        // Don't fail the whole operation just because post-processing failed
    }

    log.Debugf(
        "Successfully processed event with lag : %v ms : %+v",
        time.Since(event.Timestamp).Milliseconds(), event,
    )
    return nil
}
```

### 10. Register the Handler

Add the event post-processing service handler to the router:

```go
func startMessageRouter(
    lc fx.Lifecycle,
    router *pubsubRouter.Router,
    webhookService *webhook.WebhookService,
    onboardingService service.OnboardingService,
    eventPostProcessingService service.EventPostProcessingService,
    logger *logger.Logger,
) {
    // Register handlers before starting the router
    webhookService.RegisterHandler(router)
    onboardingService.RegisterHandler(router)
    eventPostProcessingService.RegisterHandler(router)

    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            logger.Info("starting message router")
            go func() {
                if err := router.Run(); err != nil {
                    logger.Errorw("message router failed", "error", err)
                }
            }()
            return nil
        },
        OnStop: func(ctx context.Context) error {
            logger.Info("stopping message router")
            return router.Close()
        },
    })
}
```

### 11. Trigger Reprocessing

When a customer or subscription is created, trigger reprocessing of any pending events:

```go
// After creating a customer
func (s *customerService) CreateCustomer(ctx context.Context, req dto.CreateCustomerRequest) (*dto.CustomerResponse, error) {
    // Existing code to create customer...
    
    // Trigger reprocessing of events
    go func() {
        // Use a new context since the request context will be canceled
        bgCtx := context.Background()
        if tenantID := types.GetTenantID(ctx); tenantID != "" {
            bgCtx = context.WithValue(bgCtx, types.CtxTenantID, tenantID)
        }
        
        if err := s.EventPostProcessingSvc.ReprocessEvents(bgCtx, customer.ID, ""); err != nil {
            s.Logger.Errorw("failed to reprocess events for new customer",
                "customer_id", customer.ID,
                "error", err,
            )
        }
    }()
    
    return customerResponse, nil
}

// After creating a subscription
func (s *subscriptionService) CreateSubscription(ctx context.Context, req dto.CreateSubscriptionRequest) (*dto.SubscriptionResponse, error) {
    // Existing code to create subscription...
    
    // Trigger reprocessing of events
    go func() {
        // Use a new context since the request context will be canceled
        bgCtx := context.Background()
        if tenantID := types.GetTenantID(ctx); tenantID != "" {
            bgCtx = context.WithValue(bgCtx, types.CtxTenantID, tenantID)
        }
        
        if err := s.EventPostProcessingSvc.ReprocessEvents(bgCtx, subscription.CustomerID, subscription.ID); err != nil {
            s.Logger.Errorw("failed to reprocess events for new subscription",
                "subscription_id", subscription.ID,
                "customer_id", subscription.CustomerID,
                "error", err,
            )
        }
    }()
    
    return subscriptionResponse, nil
}
```

## Edge Cases and Considerations

### 1. Deduplication Strategy
- The `ReplacingMergeTree` engine with a version field ensures only the latest version of a processed event is used in queries
- The `FINAL` modifier in queries is essential for ClickHouse to show only the latest version of each row
- From ClickHouse documentation: "For ReplacingMergeTree tables, the FINAL modifier forces the query to select only one row from all the row groups with the same sorting key value"
- This is the standard ClickHouse approach to handle versioned data and is appropriate for our use case

### 2. Events Before Customer Creation
- We now save all events in the processed_events table even if we can't fully process them
- Events without customer/subscription information will have processed_at set to NULL
- When a customer or subscription is created, we trigger reprocessing of relevant events
- This ensures we don't lose events that arrive before customer/subscription creation

### 3. Limited Aggregation Support
- We're initially only supporting COUNT and SUM aggregations, which can be calculated from single events
- AVG and COUNT_UNIQUE don't make sense for single event processing - they require aggregating across multiple events
- These more complex aggregations will still need to be handled by the original on-demand query system

### 4. Processing Failures
- If post-processing fails, the system has recovery mechanisms:
   - Events are still saved to the events table
   - Retry logic in the message handler
   - Background reprocessing for unprocessed events

### 5. Monitoring
- We should track:
   - Count of unprocessed events (processed_at IS NULL)
   - Processing lag between ingestion and completion
   - Error rates in processing

## Performance Considerations

1. **Caching**:
   - Cache customer, subscription, and meter data
   - Use distributed caching with short TTL (30 seconds)
   - Consider implementing a local in-memory cache for high-frequency data

2. **Batch Processing**:
   - Process events in batches of 100 for ClickHouse inserts
   - Consider increasing batch size for very high throughput systems

3. **Parallel Processing**:
   - Scale horizontally by adding more consumer instances
   - Use Kafka partitioning by tenant_id to enable parallel processing

4. **ClickHouse Optimization**:
   - The `ReplacingMergeTree` engine with `FINAL` queries provides efficient deduplication
   - Proper partitioning by month helps with query performance
   - Consider implementing materialized views for common aggregation patterns

## Migration Strategy

1. **Phase 1: Infrastructure Setup**
   - Create Kafka topic and message handling framework
   - Implement ClickHouse schema
   - Create repositories and service interfaces

2. **Phase 2: Shadow Mode**
   - Process events but don't use results yet
   - Compare with existing calculation methods
   - Validate accuracy and performance

3. **Phase 3: Gradual Rollout**
   - Add feature flag to enable usage of pre-computed data
   - Start with specific meters or customers
   - Monitor and expand gradually

4. **Phase 4: Full Cutover**
   - Use pre-computed data for all usage calculations
   - Retain old system temporarily as fallback
   - Complete migration after stability period

## Next Steps

1. Create the ClickHouse migration for the events_processed table
2. Implement the ProcessedEvent struct and repository
3. Create the EventPostProcessingService
4. Modify event ingestion to publish to post-processing topic
5. Register the handler in the message router
6. Implement usage calculation methods using the pre-computed data
7. Add monitoring and observability
8. Create a testing strategy with validation of pre-computed vs. on-demand calculations 