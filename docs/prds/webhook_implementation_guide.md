# Webhook Implementation Guide

## Overview

This guide provides a comprehensive walkthrough for implementing new webhooks in the FlexPrice system. It covers the architecture, implementation steps, best practices, and testing guidelines.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Implementation Steps](#implementation-steps)
3. [Best Practices](#best-practices)
4. [Testing Guidelines](#testing-guidelines)
5. [Examples](#examples)

## Architecture Overview

The webhook system in FlexPrice uses a modular architecture with the following key components:

```
internal/
  webhook/
    ├── service.go       # Main webhook service
    ├── module.go        # Dependency injection setup
    ├── publisher/       # Event publishing
    ├── handler/         # Event handling
    └── payload/         # Payload building
```

### Key Components

1. **WebhookService**: Orchestrates webhook operations
2. **Publisher**: Handles event publishing
3. **Handler**: Processes webhook events
4. **PayloadBuilder**: Builds event-specific payloads

## Implementation Steps

### 1. Define Event Type

Location: `internal/types/webhook.go`

```go
const (
    // Follow the pattern: resource.action[.state]
    WebhookEventResourceCreated = "resource.created"
    WebhookEventResourceUpdated = "resource.updated"
    WebhookEventResourceDeleted = "resource.deleted"
)
```

### 2. Create Payload Builder

Location: `internal/webhook/payload/resource.go`

```go
package payload

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/flexprice/flexprice/internal/types"
)

type ResourcePayloadBuilder struct {
    services *Services
}

func NewResourcePayloadBuilder(services *Services) *ResourcePayloadBuilder {
    return &ResourcePayloadBuilder{
        services: services,
    }
}

func (b *ResourcePayloadBuilder) BuildPayload(ctx context.Context, eventType string, data interface{}) (json.RawMessage, error) {
    switch eventType {
    case types.WebhookEventResourceCreated:
        return b.buildCreatedPayload(ctx, data)
    case types.WebhookEventResourceUpdated:
        return b.buildUpdatedPayload(ctx, data)
    case types.WebhookEventResourceDeleted:
        return b.buildDeletedPayload(ctx, data)
    default:
        return nil, fmt.Errorf("unsupported event type: %s", eventType)
    }
}

func (b *ResourcePayloadBuilder) buildCreatedPayload(ctx context.Context, data interface{}) (json.RawMessage, error) {
    payload := map[string]interface{}{
        "event_type": types.WebhookEventResourceCreated,
        "data":       data,
        // Add other relevant fields
    }
    return json.Marshal(payload)
}
```

### 3. Register Payload Builder

Location: `internal/webhook/payload/factory.go`

```go
func (f *payloadBuilderFactory) GetBuilder(eventType string) (PayloadBuilder, error) {
    switch {
    case strings.HasPrefix(eventType, "resource."):
        return NewResourcePayloadBuilder(f.services), nil
    default:
        return nil, fmt.Errorf("unsupported event type: %s", eventType)
    }
}
```

### 4. Implement Tests

Location: `internal/webhook/payload/resource_test.go`

```go
func TestResourcePayloadBuilder_BuildPayload(t *testing.T) {
    tests := []struct {
        name      string
        eventType string
        data      interface{}
        want      json.RawMessage
        wantErr   bool
    }{
        {
            name:      "created event",
            eventType: types.WebhookEventResourceCreated,
            data:      mockData,
            want:      expectedPayload,
            wantErr:   false,
        },
        // Add more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            builder := NewResourcePayloadBuilder(mockServices)
            got, err := builder.BuildPayload(context.Background(), tt.eventType, tt.data)
            // Add assertions
        })
    }
}
```

## Best Practices

### Event Naming

- Use lowercase with dots as separators
- Follow the pattern: `resource.action[.state]`
- Examples:
  - `subscription.created`
  - `invoice.update.finalized`
  - `payment.failed`

### Payload Structure

1. **Consistency**

   - Maintain consistent field names across events
   - Use standard date/time formats (ISO 8601)
   - Include common metadata fields

2. **Versioning**

   - Consider future compatibility
   - Document any breaking changes
   - Use versioned event types if needed

3. **Data Inclusion**
   - Include all necessary data for consumers
   - Avoid sensitive information
   - Consider payload size

### Error Handling

1. **Validation**

   ```go
   if data == nil {
       return nil, errors.New("data cannot be nil")
   }
   ```

2. **Logging**

   ```go
   logger.Errorw("failed to build payload",
       "error", err,
       "event_type", eventType,
       "tenant_id", tenantID,
   )
   ```

3. **Context**
   ```go
   if ctx.Err() != nil {
       return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
   }
   ```

## Testing Guidelines

### Unit Tests

1. **Payload Building**

   - Test all supported event types
   - Test error cases
   - Test data validation

2. **Factory Registration**
   - Test builder resolution
   - Test unknown event types

### Integration Tests

1. **End-to-End Flow**

   ```go
   func TestWebhookIntegration_Resource(t *testing.T) {
       // Setup test environment
       service := setupTestService(t)

       // Trigger webhook
       event := &types.WebhookEvent{
           ID:        uuid.New().String(),
           EventName: types.WebhookEventResourceCreated,
           TenantID:  "test-tenant",
           Timestamp: time.Now(),
       }

       err := service.PublishWebhook(context.Background(), event)
       require.NoError(t, err)

       // Assert webhook delivery
       // Check payload structure
       // Verify timing
   }
   ```

## Examples

### Basic Webhook Implementation

```go
// Trigger webhook
func TriggerResourceWebhook(ctx context.Context, publisher WebhookPublisher, tenantID string, data interface{}) error {
    event := &types.WebhookEvent{
        ID:        uuid.New().String(),
        EventName: types.WebhookEventResourceCreated,
        TenantID:  tenantID,
        Timestamp: time.Now(),
    }

    return publisher.PublishWebhook(ctx, event)
}

// Usage in service
func (s *resourceService) CreateResource(ctx context.Context, input CreateResourceInput) (*Resource, error) {
    // Create resource
    resource, err := s.repository.Create(ctx, input)
    if err != nil {
        return nil, err
    }

    // Trigger webhook
    if err := TriggerResourceWebhook(ctx, s.webhookPublisher, input.TenantID, resource); err != nil {
        s.logger.Errorw("failed to publish webhook", "error", err)
        // Consider if this should be returned to caller
    }

    return resource, nil
}
```

### Advanced Usage: Custom Payload

```go
type ResourcePayload struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    CreatedAt   time.Time `json:"created_at"`
    CreatedBy   string    `json:"created_by"`
    Environment string    `json:"environment"`
}

func (b *ResourcePayloadBuilder) buildCreatedPayload(ctx context.Context, data interface{}) (json.RawMessage, error) {
    resource, ok := data.(*Resource)
    if !ok {
        return nil, fmt.Errorf("invalid data type: expected *Resource, got %T", data)
    }

    payload := ResourcePayload{
        ID:          resource.ID,
        Name:        resource.Name,
        CreatedAt:   resource.CreatedAt,
        CreatedBy:   resource.CreatedBy,
        Environment: resource.Environment,
    }

    return json.Marshal(payload)
}
```

## Monitoring and Debugging

### Logging

```go
logger.Infow("webhook event published",
    "event_id", event.ID,
    "event_name", event.EventName,
    "tenant_id", event.TenantID,
)
```

### Metrics

- Track webhook delivery success/failure
- Monitor payload size
- Measure processing time

## Security Considerations

1. **Data Protection**

   - Never include sensitive data in payloads
   - Use HTTPS for delivery
   - Implement webhook signatures

2. **Rate Limiting**

   - Implement per-tenant limits
   - Handle backpressure

3. **Authentication**
   - Use secure headers
   - Rotate credentials regularly

## Troubleshooting Guide

### Common Issues

1. **Event Not Delivered**

   - Check tenant configuration
   - Verify endpoint availability
   - Check event filtering rules

2. **Invalid Payload**

   - Validate input data
   - Check payload builder implementation
   - Verify JSON serialization

3. **Performance Issues**
   - Monitor payload size
   - Check processing time
   - Review concurrent deliveries

## Conclusion

Following this guide will help ensure consistent and reliable webhook implementations across the FlexPrice system. Remember to:

1. Follow naming conventions
2. Implement comprehensive tests
3. Handle errors appropriately
4. Consider security implications
5. Monitor and log effectively

For additional support or questions, refer to the team's technical documentation or reach out to the platform team.
