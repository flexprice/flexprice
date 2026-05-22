# Flow: API request lifecycle (Gin)

## Trigger

- HTTP client issues request to **`/health`**, **`/v1/...`** (public or authenticated branches), cron triggers under **`/v1/cron/...`** (internal ops pattern).

## Execution path

1. **Router construction** [`internal/api/NewRouter`](internal/api/router.go)
   - `gin.New()` + custom middleware ordering (recovery → request id → structured logging → CORS → Sentry → Pyroscope).
   - Dynamic swagger host shim.
2. Route groups:
   - `/health`
   - `public/` using `GuestAuthenticateMiddleware`
   - `private/` using `AuthenticateMiddleware` + `EnvAccessMiddleware` + Sentry tenant context middleware
   - Subgroup `v1Private` attaches standardized `middleware.ErrorHandler()`.
3. Handler executes service call with context carrying tenant/environment/user identifiers.
4. DTO marshaller returns JSON responses; centralized error handler translates errors when configured.

## Modules touched

- `internal/api/v1/*.go`, `internal/api/cron/*.go`, `internal/api/dto/**`
- `internal/service/**`
- Middleware: `internal/rest/middleware/**`

## Database operations

- Entirely **handler/service-dependent** — no persistence at framework layer besides middleware-driven secret verification.

## External systems

- Optional Sentry profiling hooks (HTTP middleware emits breadcrumbs / transactions).

## Async operations

By default synchronous; services may enqueue **Kafka** publishes or **Temporal workflows** asynchronously relative to caller (see sibling flow docs).

## Failure points

- Panics swallowed by Gin recovery wired to FlexPrice logger adapter.
- Auth failures short-circuit with JSON error bodies.
- Service errors surfaced via `ErrorHandler()` patterns (implementation per handler idioms).

## Retry behavior

- HTTP layer has **no automated retry**.
- Temporal / Kafka retries apply only if triggered downstream.

## State transitions

- Request-scoped mutations to `gin.Context.Request.Context()` (`context.WithValue` for tenant/environment/roles).

## Related flows

- [authentication.md](authentication.md)
- [event-processing.md](event-processing.md) (when hitting `/v1/events`)
