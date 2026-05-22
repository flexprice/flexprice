# Flow documentation index

Narratives for critical paths through the codebase. Maintain these when observable behavior changes (routes, middleware, Kafka bindings, Temporal schedules).

| Flow | Document |
| ---- | -------- |
| API stack & Gin pipeline | [api-request-lifecycle.md](api-request-lifecycle.md) |
| AuthN (JWT / API keys / env access) | [authentication.md](authentication.md) |
| Metering ingestion & consumers | [event-processing.md](event-processing.md) |
| Consolidated billing orchestration | [billing.md](billing.md) |
| Invoice states & external sync hooks | [invoice-lifecycle.md](invoice-lifecycle.md) |
| Subscription states & Temporal alignment | [subscription-lifecycle.md](subscription-lifecycle.md) |
| Inbound PSP + outbound Svix pipelines | [webhook-processing.md](webhook-processing.md) |
| Watermill retry/DLQ stack | [retry-handling.md](retry-handling.md) |
