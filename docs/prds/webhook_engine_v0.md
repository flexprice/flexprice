# Webhook Engine V0 PRD

## Overview
A simple webhook engine to deliver system events to tenant-specific endpoints with configurable authentication and event filtering.

## Goals
- Enable tenants to receive real-time system events via webhooks
- Allow tenant-specific configuration for webhook endpoints and authentication
- Support event filtering per tenant
- Establish foundation for future enhancements

## Non-Goals (V0)
- UI-based configuration management
- Automatic retries
- Webhook delivery guarantees
- Webhook response handling
- Rate limiting
- Event payload versioning
- Webhook signature verification

## System Components

### 1. Configuration Management
- **Location**: YAML-based configuration in `internal/config`
- **Structure**:
```go
// in internal/config/webhook.go
type Webhook struct {
    Enabled bool                            `mapstructure:"enabled"`
    Topic   string                          `mapstructure:"topic" default:"webhooks"`
    Tenants map[string]TenantWebhookConfig  `mapstructure:"tenants"`
}

type TenantWebhookConfig struct {
    Endpoint       string            `mapstructure:"endpoint"`
    Headers        map[string]string `mapstructure:"headers"`
    Enabled        bool              `mapstructure:"enabled"`
    ExcludedEvents []string          `mapstructure:"excluded_events"`
}
```

### 2. Event Queue & Message Processing
- Utilizing `github.com/ThreeDotsLabs/watermill` for message handling
- V0: Using watermill's in-memory/channel-based pub/sub for simplicity
- V1: Will migrate to Kafka-based implementation (reusing existing infrastructure)
- Message structure will follow our existing event patterns:
```go
// in internal/types/webhook.go
type WebhookEvent struct {
    ID        string          `json:"id"`
    EventName string          `json:"event_name"`
    TenantID  string          `json:"tenant_id"`
    Timestamp time.Time       `json:"timestamp"`
    Payload   json.RawMessage `json:"payload"`
}
```

### 3. Event Types
Initial supported events:
- `price.updated`
- `price.deleted`
- `usage.recorded`
- `billing.generated`

### 4. Webhook Payload Structure
```json
{
  "id": "uuid-v4",
  "event_name": "price.updated",
  "timestamp": "2024-01-21T15:30:00Z",
  "tenant_id": "tenant1",
  "payload": {
    // Event specific payload
  }
}
```

### 5. Core Components and Interfaces

#### Directory Structure
```
internal/
  config/
    webhook.go       # webhook configuration structures
  types/
    webhook.go       # webhook types and constants
  httpclient/
    client.go       # generic HTTP client interface
    default.go      # default HTTP client implementation
  webhook/
    publisher/
      publisher.go    # webhook event publisher interface
      memory/
        publisher.go  # V0: memory-based implementation
      kafka/
        publisher.go  # V1: kafka implementation
    handler/
      handler.go      # webhook event handler interface
      memory/
        handler.go    # V0: memory-based implementation
      kafka/
        handler.go    # V1: kafka implementation
    payload/
      builder.go      # payload builder interface
      factory.go     # payload builder factory interface
      price.go        # price event payload builder
      usage.go        # usage event payload builder
```

#### Core Interfaces
```go
// httpclient/client.go
type Client interface {
    Send(ctx context.Context, req *Request) (*Response, error)
}

type Request struct {
    Method  string
    URL     string
    Headers map[string]string
    Body    []byte
}

type Response struct {
    StatusCode int
    Body       []byte
    Headers    map[string]string
}

// webhook/publisher/publisher.go
type Publisher interface {
    PublishWebhook(ctx context.Context, event *types.WebhookEvent) error
    Close() error
}

// webhook/handler/handler.go
type Handler interface {
    HandleWebhookEvents(ctx context.Context) error
    Close() error
}

// webhook/payload/builder.go
type PayloadBuilder interface {
    BuildPayload(ctx context.Context, eventType string, data interface{}) (json.RawMessage, error)
}

// webhook/payload/factory.go
type PayloadBuilderFactory interface {
    GetBuilder(eventType string) (PayloadBuilder, error)
}

// webhook/service.go
type WebhookService struct {
    config    *config.Configuration
    publisher Publisher
    handler   Handler
    factory   PayloadBuilderFactory
    client    httpclient.Client
    logger    *logger.Logger
}
```

#### Message Flow
1. System events trigger `Publisher.PublishWebhook()`
2. V0: Watermill routes through in-memory channels
   V1: Watermill routes through Kafka
3. `Handler` processes messages:
   - Validates tenant configuration
   - Filters excluded events
   - Gets appropriate builder from `PayloadBuilderFactory`
   - Uses builder to construct event payload
   - Delivers via `httpclient.Client`

#### Dependency Injection
```go
// V0: Memory-based implementation
fx.Provide(
    webhook.NewWebhookService,
    publisher.NewMemoryPublisher,
    handler.NewMemoryHandler,
    httpclient.NewDefaultClient,
    payload.NewPayloadBuilderFactory,
)

// V1: Kafka-based implementation
fx.Provide(
    webhook.NewWebhookService,
    publisher.NewKafkaPublisher,
    handler.NewKafkaHandler,
    httpclient.NewDefaultClient,
    payload.NewPayloadBuilderFactory,
)
```

## Future Enhancements (V1+)

### Reliability & Monitoring
- Automatic retries with exponential backoff
- Dead letter queue for failed deliveries
- Delivery status tracking
- Prometheus metrics for webhook delivery stats

### Security
- Webhook signature verification (HMAC)
- IP whitelisting
- TLS certificate pinning

### Configuration & Management
- UI-based webhook configuration
- API endpoints for webhook CRUD operations
- Webhook testing tools
- Event payload schema validation

### Performance & Scaling
- Rate limiting per tenant
- Batch delivery options
- Kafka-based event queue
- Horizontal scaling support

### Audit & Debugging
- Webhook delivery history
- Request/Response logging
- Debug webhooks (development endpoints)

## Implementation Plan V0

### Phase 1: Core Structure
1. Implement interfaces and types
2. Setup configuration loading
3. Create memory-based publisher implementation

### Phase 2: Event Processing
1. Implement memory-based event handler
2. Create payload builders for each event type
3. Setup HTTP client with retries

### Phase 3: Integration
1. Add webhook trigger points in codebase
2. Implement logging and monitoring
3. Add health checks

## Success Metrics
- Webhook delivery success rate
- Event processing latency
- System resource usage

## Risks & Mitigations
1. **Memory Usage**
   - Risk: Channel buffer overflow
   - Mitigation: Configurable buffer size, monitoring

2. **Security**
   - Risk: API key exposure
   - Mitigation: Secure configuration management, encrypted storage

3. **Performance**
   - Risk: Slow webhook endpoints affecting system
   - Mitigation: Timeouts, separate goroutines for delivery
