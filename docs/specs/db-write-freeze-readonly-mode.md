# Spec: DB write-freeze (read-only mode) for AlloyDB cutover

## Goal

A flag that freezes ALL Postgres writes while keeping reads, event ingestion,
and login serving — so the CloudSQL→AlloyDB cutover has write-only downtime of
seconds-to-minutes instead of a full outage. Control: env var +
rolling restart (`FLEXPRICE_POSTGRES_READONLY`), matching the fleet roll the
cutover runbook already performs.

## What must keep working during freeze (verified against code)

- **Event ingestion** — `IngestEvent`/`BulkIngestEvent` publish to Kafka and
  return 202 (`internal/ee/service/event.go`); no synchronous Postgres write.
  Untouched by a Postgres freeze.
- **Login** — `authService.Login` is 2 SELECTs + bcrypt + JWT
  (`internal/ee/service/auth.go`); no Postgres write.
- **Dashboard / list / search / analytics reads** — all `Reader(ctx)`
  (`internal/postgres/client.go:302`), served from the replica.

## What must be blocked

Every Postgres write, regardless of entry point: gin handlers, Temporal
workers, Kafka consumers, the one GET-that-writes
(`GET /v1/customer/portal/:external_id` → CreateSession,
`internal/api/router.go:305`), and `POST /auth/signup`.

## Design — gate at the ent mutation hook, not the method or the tx

Rejected alternatives:
- **HTTP-method gate** (block non-GET): wrong both ways. ~35 read endpoints
  use POST (every `/search`, `/query`, `/analytics`, `/preview`); one GET
  writes (router.go:305). Do not gate on method.
- **`WithTx()`-only gate**: misses writes. 206 direct `Writer(ctx)` calls vs
  108 `WithTx` — writes are scattered, not tx-funneled.
- **`Writer(ctx)` return-error**: signature is `*ent.Client` (no error);
  changing it touches 200+ call sites.

**Chosen — ent runtime write-blocking hook.** ent routes every mutation
(`OpCreate|OpUpdate|OpDelete`, incl. `*One`/`*Bulk`) through the hook chain on
the client (`ent/client.go:500` `Use`; `withHooks` in generated `*_create.go`/
`*_update.go`; CreateBulk runs per-builder hooks — verified). One hook
registered on BOTH writer and reader ent clients rejects mutations when the
freeze flag is set — catching all 206 direct-Writer sites, all 108 WithTx
sites, Temporal activities, and consumers, with zero caller edits.

**The ent hook is NOT a complete freeze — it does not cover raw SQL.**
`Writer(ctx)` returns `*ent.Client`; `.ExecContext`/`.QueryContext` on it are
raw `database/sql` passthrough that `Use()` never wraps. Real raw DATA writes
that bypass the hook (final review C1):
- `internal/repository/ent/invoice.go:970` — `INSERT...ON CONFLICT DO UPDATE` invoice_sequences
- `internal/repository/ent/invoice.go:~1015` — same, billing_sequences
- `internal/repository/ent/price.go:542` — bulk `UPDATE prices SET status=archived`
- `internal/repository/ent/plan_price_sync{,_v2}.go` — raw `UPDATE`s
- `internal/repository/ent/price.go:352` — `SELECT nextval(prices_sequence_seq)` (sequence advance)

Worse, `plan_price_sync*` runs as **Temporal activities** (`SyncPlanPrices`/
`SyncPlanPricesV2`) — no RBAC → no 503, AND raw SQL → no hook. Under the freeze
these would keep writing during cutover, breaking the migration's
`xact_commit delta 0` gate.

**Therefore the AUTHORITATIVE write-freeze is DB-side, not app-side:**
`ALTER ROLE <app_role> SET default_transaction_read_only = on;` on the source
DB at cutover (migration runbook step, not app code). The DB rejects every
write — ent, raw ExecContext, Temporal, consumers, anything — with no code path
able to bypass. This is the freeze.

The app-side ent hook + 503 are the **UX / fast-fail layer**: API clients get a
clean 503 "read-only, retry" and ent callers get `ErrReadOnly` instead of a
deep driver error, without waiting to hit the DB. They are defense-in-depth,
NOT the guarantee. The `locks.go` advisory-lock raw path is harmless
(session/lock ops, no data write).

Plus an **inline 503 check** for a clean early reject on write routes (so API
clients get "read-only, retry" instead of a deep ent error). The hook is the
backstop; this is UX. NOTE: `types.ActionWrite` is a closure arg inside
`RequirePermission`, NOT route metadata a global middleware can read — so the
check lives INSIDE `PermissionMiddleware.RequirePermission`
(`internal/rest/middleware/permission.go`), where `action` is in scope, NOT in
a global `router.Use` middleware. Session-token portal routes (router.go:678,
no RBAC middleware) get no early 503 and rely on the hook backstop only —
acceptable.

## Changes

### 1. Config — `internal/config/config.go`
Add to `PostgresConfig` (line ~592):
```go
ReadOnly bool `mapstructure:"readonly" default:"false"`
```
Env binds automatically via viper autobind: `FLEXPRICE_POSTGRES_READONLY=true`.

Env `default:"false"` tag is inert in this repo (defaults live in config.yaml,
per config.go comment); `bool` zero-values to false anyway. Keep it or drop it.

### 2. Write-blocking ent hook — `internal/postgres/readonly_hook.go` (new)
Imports the ROOT ent package (`.../flexprice/ent`) — ent lives at repo-root
`./ent/`, NOT `internal/postgres/ent/`.
```go
// ErrReadOnly is returned for every mutation while the DB is frozen.
var ErrReadOnly = errors.New("database is in read-only mode (cutover in progress)")

// newReadOnlyHook rejects all Create/Update/Delete mutations when enabled.
func newReadOnlyHook(enabled bool) ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if enabled {
				return nil, ErrReadOnly
			}
			return next.Mutate(ctx, m)
		})
	}
}
```
Register in `NewEntClients` (`internal/postgres/client.go`, at the writer/reader
client build, ~client.go:169-170) on both clients:
`writer.Use(newReadOnlyHook(cfg.Postgres.ReadOnly))` (and reader — a mutation
on the reader client should also fail, not silently hit the replica). No ent
hook exists today, so this is greenfield (no ordering conflict).

**Do NOT let the readonly flag reach `cmd/migrate`.** If the hook is wired into
the shared `NewEntClients` that `cmd/migrate/postgres.go` also uses,
`FLEXPRICE_POSTGRES_READONLY=true` in a migrate job would break `Schema.Create`.
Server boot does NOT run migrations (`cmd/server/main.go` has no `Schema.Create`
— verified), so a readonly server pod boots fine. Runbook: never set the
readonly env on migrate jobs.

`enabled` is captured at construction. Toggling requires a pod restart — which
is exactly the cutover roll. No runtime toggle by design (chosen: env var +
rolling restart).

### 3. Inline 503 in `RequirePermission` — `internal/rest/middleware/permission.go`
NOT a new global middleware (ActionWrite isn't visible globally). At the top of
the `RequirePermission(entity, action, ...)` returned closure, where `action`
is in scope:
```go
if pm.readOnly && action == types.ActionWrite {
    c.AbortWithStatusJSON(503, gin.H{"error":"read_only",
        "message":"database read-only (cutover in progress)","retry_after":30})
    return
}
```
`pm.readOnly` is `cfg.Postgres.ReadOnly`, injected into `PermissionMiddleware`
at construction. Covers every RBAC-gated write route incl. the GET-that-writes
(router.go:305). Session-token portal routes (router.go:678, no RBAC) get no
early 503 — hook backstop catches them. Reads, ingestion, login, `/health`
untouched (no `ActionWrite`).

## Cutover interaction

```
freeze on : DB-side ALTER ROLE <app> SET default_transaction_read_only=on  ← THE freeze (covers raw SQL + Temporal)
          + set FLEXPRICE_POSTGRES_READONLY=true (ESO) → roll fleets   ← UX: 503/ErrReadOnly fast-fail
  → reads + ingestion + login keep serving
  → confirm xact_commit delta 0 (writes truly stopped — the DB-side flag guarantees this)
  → resync sequences, parity, promote AlloyDB
  → swap FLEXPRICE_POSTGRES_HOST → AlloyDB (same ESO roll)
freeze off: DB-side ALTER ROLE ... default_transaction_read_only=off (on AlloyDB target)
          + set FLEXPRICE_POSTGRES_READONLY=false → roll fleets → writes resume on AlloyDB
```
The DB-side flag is what makes `xact_commit delta 0` true — the app flag alone
cannot (raw SQL + Temporal bypass it). freeze-off roll and endpoint-swap roll
are the SAME roll — one restart flips readonly=false AND host=AlloyDB together.

## Tests (TDD, per repo convention)

- `readonly_hook_test.go`: hook enabled → Create/Update/Delete return
  `ErrReadOnly`; a Query (read) still succeeds; hook disabled → mutation passes.
- `permission_test.go` (extend): `readOnly=true` + `ActionWrite` → 503;
  `ActionRead` POST (e.g. `/search`) → passes; `readOnly=false` → passes.
- One integration assertion: with readonly=true, `IngestEvent` still returns
  202 (ingestion is Kafka-only, unaffected).

Note: usage-record writes (`internal/repository/ent/usagerecord.go:35`
`Writer(ctx)`) ARE a synchronous Postgres write — they WILL 503/error under
freeze. Intended (it's a write), distinct from event ingestion. Runbook aware.

## Out of scope

Runtime (no-restart) toggle; app-code interception of raw-SQL/Temporal writes
(the DB-side `default_transaction_read_only` covers those — it is the
authoritative freeze, specified above in the runbook, not app code); ClickHouse
(separate store, ingestion target, never frozen).
