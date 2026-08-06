---
layer: domain/invoice
owns:
  - "internal/domain/invoice/**"
synced_sha: 8a1b776e6230d469e02f453f16cc54b5d7596a1a
synced_at: 2026-06-09T00:00:00Z
---

# Domain — Invoice Layer

> What a senior needs before touching this directory.
> Critique / improvement ideas → `.context/findings/domain-invoice.md`.

## Purpose
Core invoice models, repository interface, and domain logic (pure Go — zero external DB deps).

## Key files
| File | Role |
|---|---|
| `model.go` | `Invoice` struct + `InvoiceLineItem`; all monetary fields use `shopspring/decimal` |
| `repository.go` | `Repository` interface — the contract the repo layer must satisfy |
| `line_item.go` | `InvoiceLineItem` model |
| `line_item_repository.go` | `LineItemRepository` interface |
| `revenue.go` | Revenue recognition helpers |
| `sequence.go` | Invoice number sequence logic |

## Patterns to follow
- **No DB calls here.** Domain models are plain structs. Repository interface methods only.
- All monetary amounts: `decimal.Decimal` (never `float64`).
- Invoice status lifecycle: `draft` → `open` → `paid` / `void`. See `types.InvoiceStatus`.
- `tenant_id` + `environment_id` are on the `Invoice` struct — do not omit from any new model.

## Invariants (must hold)
- Repository interface changes require coordinated update to `internal/repository/` implementation.
- No imports of `internal/service/`, `internal/api/`, or any infrastructure package.
- Adding a field to `Invoice` struct → also update `ent/schema/invoice.go` + run `make generate-ent`.

## Common pitfalls
- Forgetting `SubscriptionCustomerID` vs `CustomerID` distinction: subscription invoices may use a different invoicing customer.
- Decimal precision: use `decimal.NewFromString()` not float literals.

## Related layers
- `internal/service/invoice.go` — business orchestration consuming this interface
- `internal/repository/` — `InvoiceRepository` implementation (Ent + PostgreSQL)
- `ent/schema/invoice.go` — DB schema (must stay in sync with model.go)
