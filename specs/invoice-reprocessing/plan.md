---
derived_from_spec: specs/invoice-reprocessing/spec.md
derived_from_sha: ""  # set to spec_hash once spec is finalized
created_at: 2026-06-09
---

# Plan — Invoice Reprocessing

## Architecture

The existing `ComputeInvoice` service method already handles the core re-query + recompute loop. The gaps are:

1. **No idempotency lock** — concurrent calls can corrupt totals (violates CR-05).
2. **No audit trail** — reprocess actor/timestamp not recorded (violates CR-06).
3. **No batch endpoint** — bulk reprocess requires calling compute N times externally (violates CR-07).
4. **Backfill ordering** — ClickHouse query must use `ORDER BY event_timestamp` not insertion order (CR-08, partially addressed, needs verification).

## Affected modules

| Module | Change |
|---|---|
| `internal/service/invoice.go` | Add distributed lock around compute; write audit metadata on reprocess |
| `internal/api/v1/invoice.go` | Add `POST /invoices/batch-reprocess` endpoint |
| `internal/temporal/workflows/invoice/compute_invoice_workflow.go` | Wrap activity in retry-safe idempotency check |
| `internal/domain/invoice/model.go` | Add `ReprocessedAt *time.Time` + `ReprocessedBy string` to `Invoice` |
| `ent/schema/invoice.go` | Add corresponding Ent fields |
| `migrations/postgres/` | Migration for new columns |
| `internal/repository/` | Update invoice update method to persist new fields |

## Distributed lock approach (CR-05)
Use Redis-based lock keyed on `invoice_id` with TTL = max expected compute duration (30s). Acquire before compute, release after. If acquire fails → return 409 Conflict.

Existing infrastructure: `internal/redis/` has a Redis client. Need to add a `Lock(ctx, key, ttl)` helper if not present.

## Batch endpoint design (CR-07)
```
POST /invoices/batch-reprocess
Body: { "invoice_ids": ["inv_xxx", ...] }
Response: { "enqueued": [...], "failed": [...] }
```
For each ID: validate it exists + is draft, start `ComputeInvoiceWorkflow` independently. Max batch size: 100.

## Risks
- **Redis unavailability** — lock acquisition fails → fall back to DB-level optimistic locking (invoice `updated_at` check) as secondary guard.
- **ClickHouse query cost** — reprocessing many invoices simultaneously could spike memory; batch endpoint should throttle workflow starts (max concurrency via Temporal batch semaphore or rate limiter).
- **Large invoice line items** — invoices with 1000+ line items may time out; activity `StartToCloseTimeout` should be set to 5 minutes minimum.

## Decision: in-place vs. void+recreate
Deliberate choice to stay in-place for draft invoices. Reasons: preserves invoice ID referenced in external systems; avoids N invoice records accumulating for the same billing period. This decision applies **only** to drafts — finalized invoices must void+recreate.
