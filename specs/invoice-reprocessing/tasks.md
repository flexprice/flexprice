---
derived_from_spec: specs/invoice-reprocessing/spec.md
status: pending
created_at: 2026-06-09
---

# Tasks — Invoice Reprocessing

Each task is one implementable unit — one agent loop or one PR.

---

## T-01: Domain model — add reprocess audit fields
**Files:** `internal/domain/invoice/model.go`, `ent/schema/invoice.go`
**What:** Add `ReprocessedAt *time.Time` and `ReprocessedBy string` to `Invoice` struct and Ent schema.
**Done when:** `make generate-ent` succeeds; new fields present in generated Ent client.
**Covers:** CR-06

---

## T-02: Migration — reprocess audit columns
**Files:** `migrations/postgres/`
**What:** `make generate-migration` after T-01; verify SQL adds nullable `reprocessed_at` (timestamptz) and `reprocessed_by` (varchar) to invoices table.
**Done when:** `make migrate-ent-dry-run` shows correct ALTER TABLE; migration file committed.
**Covers:** CR-06

---

## T-03: Repository — persist reprocess audit fields
**Files:** `internal/repository/invoice.go` (or equivalent update method)
**What:** Ensure the invoice update path persists `ReprocessedAt` and `ReprocessedBy` when set.
**Done when:** Unit test verifies fields are written to DB after update call.
**Covers:** CR-06

---

## T-04: Redis lock helper (if not present)
**Files:** `internal/redis/` or `internal/idempotency/`
**What:** Add `AcquireLock(ctx, key, ttl) (unlock func(), err error)` using existing Redis client. Return error if lock already held.
**Done when:** Unit test: two concurrent goroutines, only one acquires lock.
**Covers:** CR-05

---

## T-05: Service — idempotency lock + audit in ComputeInvoice
**Files:** `internal/service/invoice.go`
**What:** Wrap the compute body with Redis lock (T-04). After successful compute, set `ReprocessedAt = now()` and `ReprocessedBy = actor from ctx`. On lock failure, return typed conflict error.
**Done when:** Integration test: concurrent calls to `ComputeInvoice` on same invoice → only one succeeds, no total corruption.
**Covers:** CR-01, CR-05, CR-06

---

## T-06: Temporal activity — idempotency check
**Files:** `internal/temporal/workflows/invoice/compute_invoice_workflow.go` (activity wrapper)
**What:** In the compute activity, fetch invoice status before compute. If already computing (status flag or lock held), return early — do not fail.
**Done when:** Workflow replay test passes (same input → same output).
**Covers:** CR-01, retry safety

---

## T-07: Batch reprocess API endpoint
**Files:** `internal/api/v1/invoice.go`, `internal/api/router.go`
**What:** `POST /invoices/batch-reprocess` — validate IDs (exist + draft), start `ComputeInvoiceWorkflow` per ID, return `{enqueued, failed}`. Max 100 IDs per call.
**Done when:** Handler test: 3 valid IDs → 3 workflows started; 1 non-draft ID → appears in `failed`.
**Covers:** CR-07

---

## T-08: Swagger + SDK regeneration
**Files:** auto-generated
**What:** `make swagger && make sdk-all` after T-07. Verify new endpoint appears in OpenAPI spec.
**Done when:** `docs/swagger/swagger-3-0.json` contains `/invoices/batch-reprocess`.
**Covers:** developer experience

---

## T-09: Verification — write tests per verification.md
**Files:** `internal/service/invoice_test.go`, `internal/temporal/workflows/invoice/` tests
**What:** Implement all test cases from `verification.md`.
**Done when:** `make test` green; all CR-* criteria have a passing test.
**Covers:** all criteria
