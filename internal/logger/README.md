# internal/logger

Structured, context-aware logging for Flexprice. Every log line automatically carries `tenant_id`, `environment_id`, `request_id`, `user_id`, and OTel trace/span IDs — no per-call effort required.

**Full design rationale:** `docs/superpowers/specs/2026-05-28-logging-design.md`

---

## Quick start

Inject `*logger.Logger` via FX (already wired in `cmd/server/main.go`). Never call `NewLogger` directly in application code.

```go
type MyService struct {
    logger *logger.Logger
}

func (s *MyService) Finalize(ctx context.Context, inv *invoice.Invoice) error {
    s.logger.Info(ctx, "invoice.finalized",
        logger.Op("invoice.finalize"),
        logger.Entity("invoice", inv.ID),
        "amount", inv.Amount,
    )
    return nil
}
```

---

## API

Five methods, always `ctx` first:

```go
func (l *Logger) Debug(ctx context.Context, msg string, fields ...any)
func (l *Logger) Info (ctx context.Context, msg string, fields ...any)
func (l *Logger) Warn (ctx context.Context, msg string, fields ...any)  // bootstrap/cmd only — see Level Policy
func (l *Logger) Error(ctx context.Context, msg string, fields ...any)
func (l *Logger) Fatal(ctx context.Context, msg string, fields ...any)  // cmd/main only
```

`fields` is zap-style alternating `"key", value` pairs. Use the helpers below to build them.

---

## Helpers

```go
logger.Err(err)                      // → "error", err.Error(), "error_type", "*pkg.Type"
logger.Op("invoice.finalized")       // → "operation", "invoice.finalized"
logger.Event("invoice","finalized")  // → "entity","invoice","action","finalized","operation","invoice.finalized"
logger.Entity("invoice", id)         // → "invoice_id", id
```

Helpers return `[]any` — spread them at the call site:

```go
s.logger.Error(ctx, "invoice finalize failed",
    logger.Op("invoice.finalize"),
    logger.Entity("invoice", inv.ID),
    logger.Err(err),
)
```

---

## Level policy

| Level | When to use | Allowed where |
|---|---|---|
| **Debug** | SQL, kafka offsets, retry attempts, cache hits, verbose internals | Anywhere |
| **Info** | A business event completed. Format: `noun.verb_past`. Must include entity id. | Anywhere |
| **Warn** | System continues with reduced functionality OR config missing at startup | Bootstrap only: `cmd/`, `main.go`, `init()`, `internal/config/`, `New*` constructors |
| **Error** | Operation failed; caller will see consequences. Must include `"error"` field + entity id. | Anywhere |
| **Fatal** | Process cannot continue. | `cmd/`, `main.go` only |

**Info = business events only.** Delete or demote to Debug:
- `Info("entering X")` / `Info("starting Y")` / `Info("validating Z")` — dev checkpoints
- `Info("got %d items", n)` with no entity context

**Error = boundary only.** Log at the handler/consumer/activity that owns the error. Intermediate functions return errors — they do not log and return.

---

## Operation naming

Use `entity.verb_past_tense` with dots:

```
invoice.finalized       subscription.cancelled    event.ingested
webhook.delivered       credit_grant.applied      stripe.customer.synced
```

This aligns with `StartRepositorySpan` and webhook event names.

---

## Field naming conventions

All keys: `snake_case`, singular noun.

| Category | Keys |
|---|---|
| Context (auto-bound) | `request_id`, `tenant_id`, `environment_id`, `user_id`, `trace_id`, `span_id` |
| Operation | `operation`, `entity`, `action` |
| IDs | `customer_id`, `subscription_id`, `invoice_id`, `payment_id`, `plan_id`, `price_id`, `meter_id`, `feature_id`, `event_id`, `wallet_id`, `credit_grant_id`, `coupon_id`, `addon_id`, `webhook_id` |
| Metadata | `duration_ms`, `count`, `attempt`, `idempotency_key` |
| Errors | `error`, `error_type`, `error_code`, `http_status` |
| External | `provider`, `provider_resource_id`, `provider_resource_type` |
| Kafka | `kafka_topic`, `kafka_partition`, `kafka_offset`, `kafka_consumer_group` |
| Temporal | `workflow_id`, `run_id`, `activity_name`, `task_queue` |

---

## Framework adapters

### Gin
Use `c.Request.Context()` — the auth middleware already injects tenant/request IDs:

```go
func (h *Handler) Create(c *gin.Context) {
    ctx := c.Request.Context()
    h.logger.Info(ctx, "invoice.created", ...)
}
```

### Temporal workflows
Workflows receive `workflow.Context` — pass it directly (implements `context.Context`):

```go
func MyWorkflow(ctx workflow.Context, req Request) error {
    s.logger.Info(ctx, "workflow.started", logger.Op("my_workflow.started"))
}
```

### Temporal activities
Use `activity.GetContext(ctx)` — the activity context carries trace metadata:

```go
func (a *Activities) DoThing(ctx context.Context, req Request) error {
    a.logger.Debug(ctx, "activity step", "attempt", activity.GetInfo(ctx).Attempt)
}
```

### Ent (database)
`GetEntLogger` is wired in `internal/postgres/client.go` — no action needed.

### go-retryablehttp
`GetRetryableHTTPLogger` is used in HTTP client construction — no action needed.

---

## Lint enforcement (`tools/loglint`)

A custom `go/analysis` analyzer (`go vet -vettool=./bin/loglint`) enforces these rules in CI:

| Rule | Description |
|---|---|
| LL001 | Deprecated method names on Logger (Infow, Errorf, etc.) |
| LL002 | `logger.L` / `GetLogger()` outside `cmd/`+`scripts/` |
| LL003 | `Warn` called outside bootstrap allowlist |
| LL004 | `fmt.Print*` / builtin `print`/`println` outside `cmd/`+`scripts/` |
| LL006 | `log.Error(...)` with no `"error"` key in fields |
| LL007 | `log.Info/Error/...` with no `"operation"` key (warning) |
| LL008 | Info message starts with dev-checkpoint phrase (warning) |
| LL009 | `ctx` passed in fields position instead of as first arg |

Run locally: `make lint`  
CI gate: `make lint-ci` (fails on any violation)

---

## What was removed

The following are **deleted** — do not use them:

- `logger.L` (global var)
- `GetLogger()` / `GetLoggerWithContext(ctx)`
- `Debugf/Infof/Warnf/Errorf/Fatalf` (printf-style)
- `Debugw/Infow/Warnw/Errorw/Fatalw` (no-ctx structured)
- `DebugwCtx/InfowCtx/WarnwCtx/ErrorwCtx` (ctx-chaining variants)
- `DebugfCtx/InfofCtx/WarnfCtx/ErrorfCtx`
- `WithContext(ctx)` / `Ctx(ctx)` (public — now private `withContext`)

If you see any of these in code, delete or replace with the ctx-first API.
