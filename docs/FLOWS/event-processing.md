# Flow: Event processing (metering pipeline)

## Trigger

1. Authenticated ingestion via **`POST /v1/events`** (+ bulk/query variants gated by RBAC `'event':'write'` in router grouping).
2. Internal republish/transform jobs (staging-oriented topics referenced in [`internal/config/config.yaml`](internal/config/config.yaml)).

## Execution path — produce

1. `EventsHandler` (see `internal/api/v1/events*.go`) calls **`EventService`**.
2. Service validates metering payload and persists or stages according to architectural branch.
3. **`publisher.EventPublisher.Publish`** (see [`internal/publisher/event_publisher.go`](internal/publisher/event_publisher.go)):
   - `PublishToKafka` path → `kafka.EventPublisher`.
   - Optional Dynamo publication when configured (`PublishToDynamoDB` / ALL mode—both must succeed-ish policy encoded in publisher).

## Execution path — consume

Controlled by **`FLEXPRICE_DEPLOYMENT_MODE`** registrations in `cmd/server/main.go`:

- **`EventConsumptionService.RegisterHandler|Lazy|Replay`** attach subscribers with distinct **consumer groups** from config (`EventProcessing*` sections vs lazy/replay).
- Routing via **`internal/pubsub/router.Router`** with global middleware ordering:
  1. **Poison/DLQ** (Kafka-backed if `kafka.topic_dlq`, else ephemeral gochannel).
  2. Recover panics → correlation IDs → Retry (max retries & backoff coded in router).
  3. Per-handler **`middleware.NewThrottle`** for primary handler(s) guards overload.
4. Consumers transform incoming messages and persist through **`events.Repository`** ClickHouse-backed implementation (`internal/service/event_consumption.go`).
5. **Post-processing pathway** historically toggled/instrumented separately (`events_post_processing` topics + config blocks). Review yaml + service `EventPostProcessingService` registrations when auditing.

### Additional trackers (parallel registrations)

Registered alongside ingestion in **`registerRouterHandlers`** when `includeProcessingHandlers` true:

- `feature_usage_tracking` (feature/cost-sheet meters)
- `cost_sheet_usage_tracking`
- `wallet_balance_alert`
- `raw_event_consumption`
- `meter_usage_tracking`
- `usage_benchmark`

## Modules touched

- API: `internal/api/v1/events*.go`
- Services: `internal/service/event.go`, `event_consumption.go`, `feature_usage_tracking.go`, `costsheet_usage_tracking.go`, `meter_usage_tracking.go`, `raw_event_consumption.go`, `wallet_balance_alert.go`
- Messaging: `internal/pubsub/**`, `internal/kafka/**`
- Storage: ClickHouse repos under `internal/repository/clickhouse`

## Database operations

- ClickHouse inserts/merges depending on repositories (heavy write path vs PostgreSQL sparingly for ingestion metadata scenarios—verify caller).
- Operational metrics may query processed/lag monitors (`internal/kafka/monitoring.go`).

## External systems

- Kafka cluster (producer + consumer-side Watermill adapters).
- Optional Dynamo ingestion destination when enabled.

## Async operations

Whole pipeline asynchronous beyond initial HTTP acknowledgement semantics (consult handler/service for transactional guarantees vs fire-and-forward).

## Failure points

| Stage | Symptoms | Mitigations |
| ----- | --------- | ----------- |
| Publish | API error / backlog | Inspect producer config |
| Consume | Repeated retries → DLQ | Router logs + Sentry captures in handler wrappers |
| Throttle shedding | Temporary drops | Tune `cfg.EventProcessing.RateLimit` |
| Schema incompatibility | poison messages | Version events + migrations |

## Retry behavior

- Watermill Retry middleware exponential backoff capped (`internal/pubsub/router/router.go`).
- DLQ ingestion when kafka DLQ publisher configured — otherwise ephemeral memory queue (risk of silent loss under process crash unless acknowledged).

## State transitions

- Raw events progressing to **`processed`/aggregates** tracked in ClickHouse tables (see migrations `migrations/clickhouse`).
- Replay handlers re-drive historical partitions when configured separately.

## Related flows

- [billing.md](billing.md) — consumption drives rating windows
- [retry-handling.md](retry-handling.md)
