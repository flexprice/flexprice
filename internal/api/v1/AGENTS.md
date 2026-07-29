---
layer: api/v1
owns:
  - "internal/api/v1/**"
synced_sha: 8a1b776e6230d469e02f453f16cc54b5d7596a1a
synced_at: 2026-06-09T00:00:00Z
---

# API Layer — v1 Handlers

> HTTP only. No business logic. Parse → validate → delegate → respond.
> Critique / improvement ideas → `.context/findings/api-v1.md`.

## Purpose
Gin HTTP handlers: deserialize requests, validate input, call service layer, serialize responses. Nothing else.

## Key files (invoice path)
| File | Role |
|---|---|
| `invoice.go` | Invoice CRUD, compute, finalize, void, recalculate, preview, PDF |
| `router.go` | Route registration for all handlers |

## Invoice handler surface
| Handler | Method + Path | Notes |
|---|---|---|
| `ComputeInvoice` | `POST /invoices/:id/compute` | Triggers compute; async via workflow |
| `RecalculateInvoice` | `POST /invoices/:id/recalculate` | Voids + recreates; starts Temporal workflow |
| `RecalculateInvoiceV2` | `POST /invoices/:id/recalculate/v2` | Newer recalculate path |
| `FinalizeInvoice` | `POST /invoices/:id}/finalize` | `@x-scope "delete"` (irreversible) |
| `GetPreviewInvoice` | `POST /invoices/preview` | Read-only; `@x-scope "read"` override |
| `TriggerFinalizeDraftInvoiceWorkflow` | `POST /invoices/finalize-drafts` | Batch finalize; admin only |

## Patterns to follow
- Extract body: `c.ShouldBindJSON(&req)` → return 400 on error.
- Extract path params: `c.Param("id")`.
- Call service: `h.invoiceService.SomeMethod(ctx, ...)`.
- On error: `c.JSON(http.StatusXxx, gin.H{"error": err.Error()})` — use `ierr` package for typed errors.
- On success: `c.JSON(http.StatusOK, response)`.
- Swagger annotations required on every handler; include `@x-scope` for non-default scopes.

## Invariants (must hold)
- Zero business logic in handlers. If you find yourself computing totals or applying discounts here, stop and move to service layer.
- No direct DB / repository calls from handlers.
- Every new route registered in `router.go` and annotated for Swagger (`make swagger` after changes).
- Auth middleware already applied at router level — do not re-implement auth in handlers.

## Common pitfalls
- `GetPreviewInvoice` is a POST but read-only — must carry `@x-scope "read"` to avoid MCP treating it as a write.
- `FinalizeInvoice` and `RecalculateInvoice` are irreversible — use `@x-scope "delete"`.
- Don't log sensitive fields (PII, amounts) at DEBUG level without redaction.

## Related layers
- `internal/service/invoice.go` — all logic delegated here
- `internal/api/router.go` — route registration
- `docs/swagger/` — generated spec (run `make swagger` after handler changes)
