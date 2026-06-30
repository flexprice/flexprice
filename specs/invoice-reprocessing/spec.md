---
feature: invoice-reprocessing
version: v1
spec_hash: ""  # set after file is finalized
status: draft
created_at: 2026-06-09
---

# Spec — Invoice Reprocessing

Reprocess (recalculate) a draft invoice in-place: re-query usage analytics, recompute totals and line items, and update the invoice record without creating a new invoice or altering the invoice's ID or number.

## Problem
Currently, recalculating a draft invoice requires either (a) voiding + recreating (changes invoice ID/number, breaking external references) or (b) manually triggering compute and hoping state is clean. Neither path is safe to call in bulk during billing cycle corrections.

## Scope
**In scope:** Reprocessing draft invoices only. Updating line item amounts. Re-applying credits, coupons, and usage-based pricing from current data.

**Out of scope:** Finalized/paid/void invoices. Changing the billing period. Re-issuing invoice numbers.

## Acceptance criteria (EARS notation)
> WHEN = trigger condition · THE SYSTEM SHALL = required behavior

**CR-01 — Idempotent compute**
WHEN `ComputeInvoice` is called on a draft invoice that has already been computed with identical usage data, THE SYSTEM SHALL return success without altering any stored values.

**CR-02 — Stale usage refresh**
WHEN `ComputeInvoice` is called on a draft invoice, THE SYSTEM SHALL re-query ClickHouse for usage events within the invoice's billing period and recompute all usage-based line items from the fresh data.

**CR-03 — Non-usage line items preserved**
WHEN reprocessing a draft invoice that contains fixed-fee line items, THE SYSTEM SHALL NOT alter those line items' amounts.

**CR-04 — Tenant isolation**
WHEN reprocessing any invoice, THE SYSTEM SHALL only read usage events belonging to the same `tenant_id` + `environment_id` as the invoice, regardless of the invoking user's permissions scope.

**CR-05 — Concurrent safety**
WHEN two requests attempt to reprocess the same invoice concurrently, THE SYSTEM SHALL ensure exactly one reprocess completes successfully and the other returns a conflict error or waits; the invoice total MUST NOT be corrupted.

**CR-06 — Audit trail**
WHEN an invoice is reprocessed, THE SYSTEM SHALL record the reprocess timestamp and the invoking actor in the invoice's metadata.

**CR-07 — Batch reprocessing**
WHEN the batch reprocess endpoint is called with a list of invoice IDs, THE SYSTEM SHALL enqueue a Temporal workflow for each invoice independently so that failure of one does not block others.

**CR-08 — Backfill correctness**
WHEN historical usage events for a billing period are inserted retroactively and then `ComputeInvoice` is called, THE SYSTEM SHALL include those events in the recomputed totals.

## Failure-mode gate (mandatory for billing features)
All five must have a corresponding test in `verification.md`:

| Failure mode | How this spec addresses it |
|---|---|
| **Idempotency** | CR-01: same input → same output, no double-write |
| **Event ordering** | CR-08: backfilled events included; ordering in ClickHouse query by event timestamp |
| **Retries** | CR-07: each Temporal activity idempotent; workflow safe to replay |
| **Tenant isolation** | CR-04: ClickHouse queries always scoped to tenant+environment |
| **Backfill** | CR-08: late-arriving events picked up on next compute |

## Non-requirements
- Does NOT support reprocessing finalized invoices (use void + recreate for that).
- Does NOT change invoice number on reprocess.
- Does NOT notify the customer on reprocess (notification is a separate concern on finalize).
