# Flow: Retry handling (async messaging)

FlexPrice centralizes **consumer-side** retry semantics in the Watermill-based message router—not in Gin HTTP.

## Primary mechanism

Configured in [`internal/pubsub/router/router.go`](internal/pubsub/router/router.go):

1. **PoisonQueue middleware** (first)
   - Publishes irrecoverably failed handling attempts to **`cfg.Kafka.TopicDLQ`** when configured (real Kafka DLQ publisher).
   - Fallback: ephemeral in-memory **`gochannel`** topic named `poison_queue` (lost on crash—operational caveat).
2. **Recoverer** — isolates handler panics without killing entire router.
3. **Correlation ID** propagation for cross-log stitching.
4. **Retry middleware** (`middleware.Retry`)
   - `MaxRetries: 3` (hard-coded)
   - Exponential backoff with jitter (`InitialInterval`, `Multipler`, capped `MaxInterval`, bounded `MaxElapsedTime` ~2 minutes in current code).
   - OnRetry hooks log structured attempts via FlexPrice logger.

## Handler-specific throttles

Select services add **`middleware.NewThrottle`** when registering ingestion handlers (`EventConsumptionService` primary path)—rate limits local processing concurrency independent of Retry timing.

## What this does NOT cover

| Concern | Where handled |
| ------- | ------------- |
| HTTP request retries | Client responsibility |
| Kafka producer acknowledgment failures | Producer config & caller error propagation |
| Temporal activity retries | Activity options in workflow registrations |
| Database transaction retries (serialization failures) | Service-level bespoke handling (pattern varies) |

## Failure points after retries exhausted

- Message routed to DLQ / poison sink for offline inspection/replay tooling (operational playbook required).
- Sentry captures for handler-returned errors in router wrapper layers.

## Observable signals

Structured logs emitting `retrying message` increments.

DLQ lag monitoring should mirror general Kafka lag checks (`kafka/monitoring` utilities).

## Related flows

- [event-processing.md](event-processing.md)
- [webhook-processing.md](webhook-processing.md)
