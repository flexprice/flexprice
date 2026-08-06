---
layer: service
owns:
  - "internal/service/**"
synced_sha: 8a1b776e6230d469e02f453f16cc54b5d7596a1a
synced_at: 2026-06-09T00:00:00Z
---

# Service Layer

> All business logic lives here. No DB calls from handlers — service → repository only.
> Critique / improvement ideas → `.context/findings/service.md`.

## Purpose
Orchestrates domain operations: coordinates repositories, manages transactions, starts Temporal workflows, publishes events. **Single source of business truth.**

## Key files (invoice path)
| File | Role |
|---|---|
| `invoice.go` | Core invoice lifecycle: create, compute, finalize, void, preview |
| `billing.go` | Subscription billing cycle: draft → compute → finalize orchestration |
| `subscription.go` | Subscription state management (feeds billing) |
| `params.go` | `ServiceParams` — Uber FX injected struct; all service deps |

## Invoice compute path (critical)
```
ComputeInvoice(ctx, invoiceID, req)
  → fetch invoice + line items
  → for each usage-based line item: query ClickHouse analytics
  → apply pricing (tiers, flat, package)
  → apply credits, coupons, discounts
  → update totals + status
```
Key idempotency point: `ComputeInvoice` checks invoice status before processing — already-computed invoices return early (no double-billing).

## Patterns to follow
- Return `(result, error)` — never panic in service methods.
- Use `ctx` propagation everywhere (tracing + tenant isolation).
- Transactions via `s.DB.WithTx(ctx, func(tx) error { ... })`.
- Start Temporal workflows via `s.TemporalService.StartWorkflow(...)` — never call workflow logic inline.
- Log with `s.Logger.Infow(ctx, "message", "key", value)`.

## Invariants (must hold)
- No HTTP types (`gin.Context`, request/response DTOs from `api/v1`) in service methods.
- All multi-step billing operations (draft → compute → finalize) must be orchestrated via Temporal, not raw goroutines.
- `ComputeInvoice` MUST be idempotent — check current invoice state before any mutations.
- Every service method that writes must handle the "already done" case gracefully.

## Common pitfalls
- `GetPreviewInvoice` vs `ComputeInvoice`: preview is read-only and does not persist; compute writes. Do not confuse.
- Credit application order: credits applied after coupons — changing order breaks billing math.
- `ProcessDraftInvoice` wraps compute + finalize + payment — use it for end-to-end flows, not piecemeal calls.

## Related layers
- `internal/domain/invoice/` — repository interfaces consumed here
- `internal/repository/` — actual implementations injected via FX
- `internal/temporal/workflows/invoice/` — workflow definitions started from service
- `internal/api/v1/invoice.go` — handlers that call into this layer
