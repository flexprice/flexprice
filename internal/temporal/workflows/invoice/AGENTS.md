---
layer: temporal/workflows/invoice
owns:
  - "internal/temporal/workflows/invoice/**"
synced_sha: 8a1b776e6230d469e02f453f16cc54b5d7596a1a
synced_at: 2026-06-09T00:00:00Z
---

# Temporal — Invoice Workflows

> Long-running, retryable invoice processing. Idempotency is the north star here.
> Critique / improvement ideas → `.context/findings/temporal-invoice.md`.

## Purpose
Wraps invoice operations that must be durable, retryable, and observable: compute, finalize, void, sync to payment providers. Temporal guarantees at-least-once execution; workflows must therefore be idempotent.

## Key files
| File | Role |
|---|---|
| `compute_invoice_workflow.go` | Recompute a draft invoice's totals from usage data |
| `draft_and_compute_subscription_invoice_workflow.go` | Create draft + compute in one durable operation |
| `finalize_draft_invoice_workflow.go` | Transition draft → open + trigger downstream actions |
| `process_invoice_workflow.go` | End-to-end: draft → compute → finalize → payment |
| `recalculate_invoice_workflow.go` | Recalculate an existing invoice (e.g., post-correction) |
| `schedule_draft_finalization_workflow.go` | Scheduled batch: finalize all ready draft invoices |

## Patterns to follow
- Workflow functions are thin orchestrators — call activities, not service methods directly.
- Activities in `internal/temporal/activities/` carry the actual logic; workflows compose them.
- Activity retries configured via `workflow.ActivityOptions` — always set `StartToCloseTimeout` and `RetryPolicy`.
- Pass only serializable types (no interfaces, no unexported fields) as workflow inputs.
- Use `workflow.GetLogger(ctx)` for logging inside workflows (not the app logger).

## Invariants (must hold)
- All activities MUST be idempotent — Temporal will retry them on failure.
- Workflow inputs and outputs must be JSON-serializable.
- Register every new workflow + activity in `internal/temporal/registration.go`.
- Never call `internal/service` directly from a workflow function — always via activity.

## Common pitfalls
- Side effects (random values, timestamps) must use `workflow.Now(ctx)` and `workflow.SideEffect()` — not `time.Now()`.
- Non-deterministic code in workflow functions breaks replay. All branching must be deterministic on the same history.
- Workflow versioning: use `workflow.GetVersion()` when changing workflow logic to preserve running instances.

## Related layers
- `internal/temporal/activities/` — activity implementations called by these workflows
- `internal/temporal/registration.go` — registration of all workflows/activities
- `internal/service/invoice.go` — `StartWorkflow` calls originate here
