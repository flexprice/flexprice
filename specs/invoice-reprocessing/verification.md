---
derived_from_spec: specs/invoice-reprocessing/spec.md
derived_from_sha: ""  # set to spec_hash once spec finalized
created_at: 2026-06-09
---

# Verification — Invoice Reprocessing

Each acceptance criterion maps to at least one test. If a criterion has no test, the feature is not done.

---

## CR-01 — Idempotent compute

**Test:** `TestComputeInvoice_Idempotent`
- Setup: draft invoice with known usage events; call `ComputeInvoice` → record totals.
- Action: call `ComputeInvoice` again with identical usage data.
- Assert: totals unchanged, no duplicate line items, no error.
- Type: service integration test (real DB, mock ClickHouse returning same data).

---

## CR-02 — Stale usage refresh

**Test:** `TestComputeInvoice_RefreshesUsage`
- Setup: draft invoice computed with usage = 100 units.
- Action: insert new ClickHouse events (total now 150 units); call `ComputeInvoice`.
- Assert: line item amount updated to reflect 150 units; invoice total updated.
- Type: service integration test.

---

## CR-03 — Non-usage line items preserved

**Test:** `TestComputeInvoice_PreservesFixedFees`
- Setup: draft invoice with one fixed-fee line item ($50) + one usage-based line item.
- Action: call `ComputeInvoice`.
- Assert: fixed-fee line item amount = $50 (unchanged); usage-based line item may change.
- Type: service unit test.

---

## CR-04 — Tenant isolation

**Test:** `TestComputeInvoice_TenantIsolation`
- Setup: two tenants (T1, T2) each with usage events; T1 has an invoice.
- Action: call `ComputeInvoice` for T1's invoice.
- Assert: ClickHouse query contains `tenant_id = T1` and `environment_id = T1_env`; T2 events not included in totals.
- Type: service integration test; inspect generated ClickHouse SQL or use a recording mock.

---

## CR-05 — Concurrent safety

**Test:** `TestComputeInvoice_ConcurrentCalls`
- Setup: single draft invoice; two goroutines both call `ComputeInvoice` simultaneously.
- Action: run both, collect results.
- Assert: exactly one returns success; the other returns 409 or waits and succeeds after; invoice total is consistent (not a sum of both runs).
- Type: service integration test with real Redis lock.

---

## CR-06 — Audit trail

**Test:** `TestComputeInvoice_WritesAuditFields`
- Setup: draft invoice, known actor in ctx.
- Action: call `ComputeInvoice`.
- Assert: invoice record in DB has `reprocessed_at` set (within last 5s) and `reprocessed_by = actor`.
- Type: service integration test.

---

## CR-07 — Batch reprocessing

**Test:** `TestBatchReprocessEndpoint_EnqueuesWorkflows`
- Setup: 3 draft invoices, 1 finalized invoice.
- Action: POST `/invoices/batch-reprocess` with all 4 IDs.
- Assert: response `enqueued` contains 3 draft IDs; `failed` contains the finalized ID; 3 Temporal workflows started (verify via mock Temporal client).
- Type: handler test (mock service layer).

**Test:** `TestBatchReprocessEndpoint_MaxBatchSize`
- Action: POST with 101 IDs.
- Assert: 400 Bad Request.

---

## CR-08 — Backfill correctness

**Test:** `TestComputeInvoice_IncludesBackfilledEvents`
- Setup: draft invoice for billing period Jan 1–31; initial compute with 100 events.
- Action: insert 20 additional events with timestamps within Jan 1–31 (simulating late arrival); call `ComputeInvoice`.
- Assert: recomputed total reflects 120 events.
- Type: service integration test with ClickHouse testcontainer.

---

## Replay / golden-master tests

**Test:** `TestComputeInvoice_GoldenMaster`
- Setup: fixed event set in testdata fixture.
- Action: run `ComputeInvoice`.
- Assert: output totals match golden file `testdata/invoice_compute_golden.json`.
- Purpose: catch silent pricing regressions on refactor.
- Type: unit test, deterministic; update golden file intentionally and review in PR.

---

## Coverage gate
Before merge, all tests above must be green (`make test`). Missing test = block merge.
