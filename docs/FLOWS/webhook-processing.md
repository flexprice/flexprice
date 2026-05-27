# Flow: Webhook processing

Two distinct webhook families coexist:

(A) **Inbound provider webhooks** (Stripe/Razorpay/Paddle/etc.)  
(B) **Outbound customer webhooks** (FlexPrice → subscriber endpoints via Svix/Kafka-backed pipeline)

## A — Inbound payment/provider webhooks

### Trigger

Provider posts signed HTTP callbacks to **`/v1/webhooks/...`** (route groups declared in router; multiplexed through `WebhookHandler` in `internal/api/v1/webhook.go`).

### Execution path

1. Handler authenticates signatures per integration package (`integration/*/webhook`).
2. Event normalized into internal DTO/domain operations (payments, invoices, subscription mirrors).
3. Services persist authoritative changes in Postgres and may enqueue async follow-ups.

### Modules touched

- `internal/api/v1/webhook.go`
- Integration packages (`internal/integration/*/webhook`, `paddle`, `stripe`, `quickbooks`, `zoho`, `razorpay`, `nomod`, `moyasar`, `chargebee`, `hubspot` where applicable).

### Failure points & retries

Depends on PSP expectations—typically return non-200 on transient failure so remote side retries exponential backoff automatically.

Malformed signatures → rejection before service layer.

---

## B — Outbound system/customer webhooks

### Trigger

Internal domain milestones enqueue messages onto **system-events Kafka topics** configured under webhook sections (`cfg.Webhook`). Consumer **`internal/webhook`** (Fx module wired before generic services ordering) listens & processes.

### Execution path

1. **WebhookService.RegisterHandler** hooks into **`pubsub.Router`** when processing mode allows (`includeProcessingHandlers` true in deployment modes exercising consumers).
2. **Payload factories** hydrate JSON from many read-services (see [`internal/webhook/module.go`](internal/webhook/module.go) injecting invoice/plan/feature/subscription/etc. services).
3. **WebhookPublisher** (backed by shared Kafka producer) emits final envelope (potential persistence of `systemevent` aggregates — follow `publisher` package interplay with `repository/ent/SystemEvent`).
4. **Svix integration** optionally delivers routed events (see Svix handlers in webhook API endpoints for dashboard onboarding).

### Async operations

Entire outbound path asynchronous — HTTP API returns before delivery confirmation semantics unless awaiting internal publish only.

### Failure points

Poison queue / DLQ if repeated processing failure at Watermill middleware layer.

Stale payload blueprint if parallel API evolution not mirrored in builder tests.

Misconfigured consumer groups causing duplicate webhook fan-out.

---

## Retry behavior references

Outbound side inherits global Watermill Retry + DLQ semantics documented in [`retry-handling.md`](retry-handling.md).

Inbound side primarily relies on PSP resend semantics.

---

## Related flows

- [event-processing.md](event-processing.md) (shared router infrastructure)
- [api-request-lifecycle.md](api-request-lifecycle.md)
