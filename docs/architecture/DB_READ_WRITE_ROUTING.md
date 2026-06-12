# PostgreSQL Reader/Writer Routing & Read-After-Write Consistency

## Problem

Flexprice runs PostgreSQL on AWS Aurora, which exposes separate **writer** and
**reader** endpoints. The reader endpoint serves from replicas with replication
lag (typically tens of milliseconds, occasionally more). Many flows read an
entity immediately after writing it; if that read lands on a lagging replica it
fails with not-found or returns stale data. Historically this forced both
endpoints to point at a single instance.

## Solution: automatic writer pinning per unit of work

A **unit of work** is one HTTP request, one Kafka message, one Temporal
activity, or one background job. Each unit of work gets a mutable *writer pin*
installed on its root context (`types.WithWriterPinning`). The pin is a shared
`atomic.Bool`, so flipping it deep inside a call stack is visible to every
context derived from the same root — including the parent context after a
nested call returns.

Routing rules, resolved in `postgres.Client` (`internal/postgres/client.go`):

| Priority | Condition | Reads go to |
|---|---|---|
| 1 | Inside a transaction (`WithTx`) | transaction client (writer) |
| 2 | `types.WithForceWriter` set on context | writer |
| 3 | Writer pin flipped (a write already happened in this unit of work) | writer |
| 4 | Otherwise | reader (replica) |

The pin is flipped automatically by `Client.Writer(ctx)` — any repository
mutation pins the rest of the flow. This covers transactions too: a write
inside `WithTx` calls `Writer(txCtx)`, and because the pin holder is shared
with the parent context, reads issued **after the transaction commits** still
route to the writer. (This was the main gap: the old `ForceWriter` flag only
covered reads *inside* the transaction callback.) Read-only transactions never
flip the pin, so they don't shift later reads off the replica; a rolled-back
write leaves the pin set, which is conservative but always correct.

Pure-read flows never flip the pin and keep using the replica, so read
scalability is preserved.

### Where pins are installed

| Entrypoint | Location |
|---|---|
| All HTTP routes (incl. cron handlers) | `middleware.DBWriterPinMiddleware` in `internal/rest/middleware/db_routing.go`, registered early in `internal/api/router.go` |
| Kafka message handlers (11) | each `processMessage` in `internal/service/*` (event consumption, feature/meter/costsheet usage tracking, post-processing, wallet alerts, raw events, onboarding, usage benchmark), `internal/webhook/handler`, `internal/integration/events` |
| Temporal activities (all task queues) | `WriterPinInterceptor` in `internal/temporal/interceptor/writer_pin_interceptor.go`, registered in `buildWorkerOptions` |

A context **without** a pin holder behaves exactly as before (reads → replica,
no pinning), so untouched code paths cannot regress. When adding a new
entrypoint that creates a fresh `context.Background()`, wrap it with
`types.WithWriterPinning(...)`.

### Observability

The tracing wrapper (`internal/postgres/monitoring.go`) tags every read span
with `db.resolved_target`: `reader`, `writer_via_tx`, `writer_forced`, or
`writer_pinned`. Use this in SigNoz to watch the reader/writer traffic mix and
verify pinning behaves as expected after enabling the separate reader endpoint
(`postgres.reader_host` / `FLEXPRICE_POSTGRES_READER_HOST`).

## Known remaining exposure (by design)

1. **Cross-request reads** — client POSTs an entity, then issues a separate GET
   a few ms later. The GET is a new unit of work and reads the replica. The
   write response already contains the full entity; clients should use it.
   If this bites in practice, options are: short-TTL tenant-level pinning via
   Redis, or session-consistent reads at the API gateway.
2. **HTTP write → Kafka consumer read** — the consumer is a separate unit of
   work; if it reads an entity the producer just wrote, it can see a stale
   replica until it performs its own first write. In practice Kafka delivery
   latency almost always exceeds replica lag, and consumers retry/nack on
   failure. If a specific consumer proves sensitive, apply
   `types.WithForceWriter` to its message context.

## Audit results (2026-06-12)

A full scan of the repository and service layers found:

- **No misrouted operations**: all mutations go through `Writer(ctx)`, all
  queries through `Reader(ctx)`, across all repository files.
- **No raw-SQL bypasses in production code**; only one-off migration scripts
  under `scripts/internal/` open their own connections (acceptable).
- **No goroutines doing DB work on bare `context.Background()`** from request
  flows (background contexts found were logging-only).
- **~20 avoidable read-after-write patterns** in services: update/create
  followed by an immediate `Get` of the same entity just to build the response
  (e.g. `plan.go UpdatePlan`, `subscription.go UpdateSubscription`,
  `entitlement.go UpdateEntitlement`, `feature.go UpdateFeature`,
  `tax.go UpdateTaxRate`, `coupon.go UpdateCoupon`,
  `subscription_line_item.go`, `subscription_schedule.go`,
  `priceunit.go`, `task.go`, `scheduled_task.go`,
  `entityintegrationmapping.go`). These are now **correct** (the pin routes the
  re-read to the writer) but add avoidable writer load. Follow-up: have
  repository `Create`/`Update` return the persisted entity (Ent's `Save`
  already does) and drop the re-read. Prioritize the subscription and invoice
  paths.
