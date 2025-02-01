# Watermill Implementation Improvements - Phase 1

## Current Limitations

1. **Basic Error Handling**
   - No retry mechanism for failed webhook deliveries
   - Limited error recovery options
   - Basic error logging without context

2. **Limited Middleware Usage**
   - Not utilizing Watermill's middleware capabilities
   - Missing correlation tracking
   - Basic error handling

3. **Monitoring Gaps**
   - Limited error tracking in Sentry
   - Basic logging without correlation
   - Difficult to trace message flow

## Core Improvements

### 1. Enhanced Message Router Configuration

```go
// Configure router with essential middleware
func NewWebhookRouter(cfg *config.Configuration, logger *logger.Logger) (*message.Router, error) {
    router, err := message.NewRouter(message.RouterConfig{
        CloseTimeout: time.Second * 30,
    }, logger)
    if err != nil {
        return nil, err
    }

    // Add essential middleware
    router.AddMiddleware(
        // Recover from panics
        middleware.Recoverer,

        // Add retry capability
        middleware.Retry{
            MaxRetries:      3,
            InitialInterval: time.Second,
            MaxInterval:     time.Second * 10,
            Multiplier:      2.0,
            Logger:          logger,
        }.Middleware,

        // Add correlation ID for tracing
        middleware.CorrelationID,
    )

    return router, nil
}
```

### 2. Improved Error Handling with Sentry Integration

```go
// Enhanced handler with Sentry error reporting
func (h *handler) processMessage(ctx context.Context, msg *message.Message) error {
    // Add correlation ID to context
    ctx = correlation.ContextWithCorrelationID(ctx, msg.UUID)

    // Structured logging with correlation ID
    logger := h.logger.With(
        "correlation_id", correlation.FromContext(ctx),
        "message_uuid", msg.UUID,
    )

    // Process message with error handling
    if err := h.doProcessMessage(ctx, msg); err != nil {
        // Capture error in Sentry with context
        sentry.WithScope(func(scope *sentry.Scope) {
            scope.SetTag("correlation_id", correlation.FromContext(ctx))
            scope.SetTag("message_uuid", msg.UUID)
            scope.SetTag("tenant_id", msg.Metadata.Get("tenant_id"))
            scope.SetTag("event_type", msg.Metadata.Get("event_type"))
            scope.SetContext("message", map[string]interface{}{
                "metadata": msg.Metadata,
                "payload": string(msg.Payload),
            })
            h.sentry.CaptureException(err)
        })

        // Log error with context
        logger.Errorw("failed to process message", 
            "error", err,
            "retry_count", getRetryCount(msg),
        )

        // Return error for retry if appropriate
        if shouldRetry(err) {
            return err // Will be retried by middleware
        }

        // Ack message if we don't want to retry
        msg.Ack()
        return nil
    }

    return nil
}
```

### 3. Retry Helper Functions

```go
// Helper functions for retry logic
func getRetryCount(msg *message.Message) int {
    count, _ := strconv.Atoi(msg.Metadata.Get("retry_count"))
    return count
}

func shouldRetry(err error) bool {
    // Retry on temporary errors like network issues
    if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
        return true
    }

    // Retry on specific HTTP status codes
    if httpErr, ok := err.(*httpclient.Error); ok {
        switch httpErr.StatusCode {
        case http.StatusTooManyRequests,
             http.StatusBadGateway,
             http.StatusServiceUnavailable,
             http.StatusGatewayTimeout:
            return true
        }
    }

    return false
}
```

## Implementation Plan

### Phase 1: Core Structure (Current Focus)
1. Implement router with basic middleware
   - Panic recovery
   - Retry mechanism
   - Correlation ID tracking
2. Add Sentry integration for error tracking
3. Enhance error handling and logging

### Future Phases (Deferred)
1. Dead letter queue implementation
2. Prometheus metrics
3. Advanced routing patterns
4. Throttling implementation

## Benefits of Phase 1

1. **Improved Reliability**
   - Automatic retry on failures
   - Better error recovery
   - Panic protection

2. **Better Debugging**
   - Correlation ID tracking
   - Detailed Sentry error reporting
   - Structured logging

3. **Maintainability**
   - Cleaner error handling
   - Better message tracing
   - Consistent retry behavior

## Implementation Notes

1. **Configuration**
   - Retry settings should be configurable per environment
   - Correlation ID format should be standardized
   - Sentry tags should be consistent

2. **Error Categories**
   - Network errors (temporary)
   - HTTP status codes (retryable vs non-retryable)
   - Business logic errors (non-retryable)

3. **Logging Strategy**
   - Always include correlation ID
   - Log at appropriate levels (debug, info, error)
   - Include relevant context for debugging 