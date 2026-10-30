# Phase 1 — Suppression log and never-reuse registry

Linear: [FLE-1883](https://linear.app/flexprice/issue/FLE-1883/phase-1-suppression-log-and-never-reuse-registry)
Parent: [FLE-1836](https://linear.app/flexprice/issue/FLE-1836/implement-customer-offboarding-and-verified-data-erasure) · PRD: [FLE-1475](https://linear.app/flexprice/issue/FLE-1475/prd-offboarding-of-users-and-complete-data-removal-basis-configured) (flexprice/flexprice#2990)
Date: 2026-10-29

> **File references corrected 2026-10-29.** The first draft was written against a branch based on an older `develop`, where the service layer lived at `internal/service/`. On current `develop` it is `internal/ee/service/` — 113 files moved, 2 remain. Every service path and line number below was re-verified against `upstream/develop`; two factual claims changed materially and are flagged inline in §5.1. Re-verify before trusting any line number here: this document has already been wrong once for exactly this reason.

## 1. What this phase ships, and what it does not

Phase 1 ships the durable record of a deletion request plus immediate read suppression. It deletes nothing.

Shipped:

- Two Postgres tables: one deletion request per customer, N category rows per request.
- A permanent external-ID registry that retires an external ID forever.
- Read-path suppression across all five customer read methods — the two single-row getters plus the query-builder status filter that backs list, count and export (§5.1).
- An authenticated HTTP endpoint to record a deletion request and read it back. Without it the tables are unreachable and nothing can be recorded.

Not shipped, deliberately:

- No deletion or redaction of any customer data. All data still exists in Postgres, ClickHouse and object storage.
- No ingest suppression. Events for a suppressed external ID are still accepted and still land in ClickHouse (Phase 2, FLE-1840).
- No *validation* API. The Phase 1 read endpoint returns the recorded request and its category rows as stored; it attests to nothing, because every category is `pending` and no store has been verified. The endpoint that answers "is this erasure complete" is Phase 4 (FLE-1886).
- No orchestration, no Temporal (Phase 3, FLE-1885).
- No cross-role traversal. Suppressing a payer does not suppress a usage owner.
- Hosted invoice links, checkout sessions and invoice PDFs stay live and reachable (Phase 2, FLE-1846 / FLE-1844).

What this phase can honestly be called externally is **suppression** — stage (a) of the two-stage contract language. It is not deletion, and the validation API that attests to deletion is Phase 4. Any external communication about Phase 1 must say so.

## 2. Why this is the right first slice

Three reasons, in order of weight.

**It depends on nothing.** The tables and the registry need no new primitives, no ClickHouse mutation support, no Temporal worker. Everything in Phase 2 can be blocked on the tables; the tables can be blocked on nothing. Every other candidate first slice inverts that.

**It is the thing every later phase writes into.** Each Phase 2 touchpoint flips its own category row. Phase 3 transitions request state. Phase 4's release gate is a query over category rows. Building the record last would mean every operation lands with nowhere to record its outcome and then gets retrofitted.

**It closes a real hole on its own.** `CustomerRepo.Get` does not filter on status at all, so an archived customer is still fully readable by internal ID today. One predicate fixes that for all 51 call sites.

## 3. Key decision: the external ID is retained permanently

External-ID reuse is never permitted. Once an external ID has been subject to a deletion request, it can never again identify a customer in that tenant and environment.

This reverses the earlier design, which snapshotted the external ID temporarily and cleared it after dependent purges completed. Consequences:

- `external_id` on the request table is `NOT NULL` and permanent. It is the tenant-facing display key: "what was deleted for external ID X" must stay answerable indefinitely.
- The registry's unique index is the enforcement point. Customer creation consults it and rejects retired IDs.
- `subject_ref` pseudonymisation and per-tenant salt/secret design are **dropped from scope entirely**. Their only purpose was letting the audit row survive the clearing of a temporary identifier. Nothing is cleared, so there is nothing to pseudonymise. This removes an enablement prerequisite that FLE-1837 and FLE-1840 previously carried.

Compliance note that must reach the retention annex: an external ID is frequently itself personal data — email-shaped, phone-shaped, or a customer name. Retaining it permanently needs its own lawful basis under GDPR Article 6 and a storage-limitation rationale under Article 5(1)(e); Article 5(2) accountability obliges us to demonstrate compliance but does not by itself supply that basis. The supporting argument is that an erasure cannot be evidenced without identifying its subject, and that a suppression list is the recognised mechanism for preventing re-creation of erased data. It is nonetheless a deliberate retention of one identifier and must appear in the annex as its own line with its own stated legal basis. Do not describe the resulting record as anonymised; it is not.

## 4. Data model

Follow `ent/schema/scheduledtask.go`: `BaseMixin` + `EnvironmentMixin`, `varchar(50)` IDs, `SchemaType` on every string field, field comments for non-obvious columns.

`BaseMixin` supplies `tenant_id`, `status`, `created_at`, `updated_at`, `created_by`, `updated_by`. `EnvironmentMixin` supplies `environment_id`. Do not redeclare them.

Note the collision: `BaseMixin.status` is the row lifecycle (`published` / `archived` / `deleted`), unrelated to the deletion request's own state. Name the request's own field `state`, never `status`, or every query becomes ambiguous.

### 4.1 `customer_deletion_requests`

One row per deletion request per customer.

| Field | Postgres type | Null | Notes |
|---|---|---|---|
| `id` | varchar(50) | no | immutable, unique |
| `customer_id` | varchar(50) | no | internal ID; stable across later redaction |
| `external_id` | varchar(255) | no | permanent, never cleared; tenant-facing key and never-reuse value |
| `state` | varchar(30) | no | `suppressed` / `redacting` / `completed` / `failed` |
| `requested_at` | timestamptz | no | SLA clock start |
| `suppressed_at` | timestamptz | yes | stage (a) proof |
| `redacted_at` | timestamptz | yes | stage (b) proof; Phase 3 writes |
| `cooldown_until` | timestamptz | yes | dispute/chargeback hold; unused in Phase 1 |
| `irreversible_started_at` | timestamptz | yes | Phase 3 writes; cancellation forbidden from here on |
| `requested_via` | varchar(20) | no | `api` / `written` |
| `retention_policy` | jsonb | yes | policy **as applied**, snapshotted at request time |
| `failure_reason` | text | yes | |

Indexes:

- Unique `(tenant_id, environment_id, customer_id)` — idempotency. A repeat request returns the existing row rather than opening a second.
- Unique `(tenant_id, environment_id, external_id)` — this is the never-reuse registry (see §4.3).
- `(tenant_id, environment_id, state)` — for finding work to do.

Field notes:

- **`state` carries all four values from day 1** even though Phase 1 only ever writes `suppressed`. Phase 2 and 3 then need no migration. Reject unknown values at the type boundary, not in the database.
- **`requested_via`** exists on day 1 because the DPA commits to honouring written instructions as well as API calls. The channel must be recordable from the first row, or early requests are unattributable.
- **`retention_policy` is a snapshot, not a pointer.** A request audited in 2031 must show the rules that governed it in 2026. Storing a config reference means the audit answer changes when config changes, which defeats the purpose.
- **`irreversible_started_at` is present but unused in Phase 1.** It exists now so cancellation rules have a home and Phase 3 is purely additive.

### 4.2 `customer_deletion_categories`

One row per touchpoint per request.

| Field | Postgres type | Null | Notes |
|---|---|---|---|
| `id` | varchar(50) | no | immutable, unique |
| `request_id` | varchar(50) | no | FK to request |
| `category` | varchar(60) | no | `customer_pg`, `events_ch`, `meter_usage_ch`, `invoice_pdfs`, … |
| `disposition` | varchar(30) | no | `pending` / `purged` / `redacted` / `retained` |
| `legal_basis` | text | yes | required when `retained`, otherwise null |
| `verified_at` | timestamptz | yes | |
| `verification_result` | jsonb | yes | actual store counts |

Indexes: unique `(request_id, category)`; `(tenant_id, environment_id, disposition)` for the Phase 4 release gate.

Why two tables rather than one status column on the request: partial outcomes must be representable. "ClickHouse purged, PDFs retained under tax law, exports still pending" is exactly the sentence the Phase 4 validation API has to return, and a single row cannot express it. `legal_basis` per retained category is what Article 17(3)(b) requires us to be able to state when refusing erasure of something.

Phase 1 writes exactly one category row per request: `customer_pg` / `pending`.

### 4.3 The registry is an index, not a table

The never-reuse registry is the unique index on `(tenant_id, environment_id, external_id)` over `customer_deletion_requests`. No separate table.

A separate registry table would be a second source of truth for the same fact and could drift from the requests it is supposed to summarise. The request row already holds the external ID permanently, already exists for exactly the set of retired IDs, and already must be looked up to answer "what was deleted for X". One index over it does the enforcement.

The index must have **no partial predicate**. Both existing customer indexes are `WHERE status = 'published'`; copying that habit here would let a soft-deleted request row release its external ID, which is precisely the failure this prevents.

## 5. Read suppression

### 5.1 The approach and why

Customer reads reach Postgres through two distinct mechanisms, and **both** must be guarded. An earlier draft of this spec claimed all reads funnel through the two single-row getters; that is false and the error is worth stating plainly, because the acceptance criteria written against it would have passed while suppressed customers were still being served.

**Mechanism one — single-row getters.** **73** non-test call sites (re-counted on current `develop`; the figure of 51 came from the pre-move layout), all through:

- `CustomerRepo.Get` — `internal/repository/ent/customer.go:120`
- `CustomerRepo.GetByLookupKey` — `internal/repository/ent/customer.go:168`

Both already return `ierr.ErrNotFound` on a miss, and every caller already handles that path, because it is the existing "customer does not exist" behaviour. One predicate each covers all 73.

**Mechanism two — list/count via the query builder.** `List`, `Count` and `ListAll` bypass both getters entirely. Status handling on current `develop`:

```go
// internal/repository/ent/customer.go:464
func (o CustomerQueryOptions) ApplyStatusFilter(query CustomerQuery, status string) CustomerQuery {
	if status == "" {
		return query.Where(customer.StatusIn(
			string(types.StatusPublished),
			string(types.StatusArchived),
		))
	}
	return query.Where(customer.Status(status))
}
```

**This changed since the first draft, in our favour.** The filter now *allow-lists* `published` and `archived` rather than excluding only `deleted`. A new `suppressed` status is therefore absent from every default-filtered list automatically, by construction. The leak the first draft described — a scheduled S3 export writing fresh copies of suppressed PII — **does not exist on current `develop`**.

What remains is narrower but still required: **refuse an explicit `status=suppressed`.** The `else` branch honours whatever status a caller passes, so a caller can opt back into seeing suppressed customers and reduce the guard to a suggestion. Suppression must not be a selectable filter value.

Re-verify both facts before implementing. If `ApplyStatusFilter` is ever rewritten back to a deny-list the default-list leak returns, and these consumers matter again:

- `internal/ee/service/sync/export/credit_usage_report.go:88`, `usage_analytics_export.go:272`, `:290` — scheduled S3 exports.
- `internal/ee/service/subscription.go:7942`, `subscription_modification.go:749`, `event_deletion.go:479`, `:508`.

**So the fix is in three places:** the two getters, plus the explicit-status branch of `ApplyStatusFilter`.

Guarding at individual call sites instead would be a 73-file diff and would leave every future caller unguarded by default. The shared-function guard is both smaller and the one that stays correct.

### 5.2 New status value

Add `StatusSuppressed Status = "suppressed"` to `internal/types/status.go`.

Do not overload `StatusArchived`. `internal/types/status.go:17-20` documents archived as "might be returned in queries and might be visible to users in some cases only" — deliberately soft. Suppression is a hard guarantee. Overloading also makes "churned but readable" and "erasure requested, must not be readable" permanently indistinguishable, which breaks both the audit story and any future archived-customer feature.

### 5.3 Changes

In `Get`, add the suppressed-status exclusion. Note `Get` currently filters only on ID, tenant and environment — no status predicate at all — so an archived customer is fully readable by internal ID today. This is the actual hole.

In `GetByLookupKey`, the query already filters `customer.Status(string(types.StatusPublished))` at `:180`, so the external-ID path already excludes anything non-published. Verify rather than assume; add the explicit exclusion only if the published filter is ever relaxed.

In `ApplyStatusFilter` (`:443`), exclude `suppressed` in the empty-status branch and refuse an explicit `status=suppressed`. See §5.1.

### 5.4 Cache: suppression is not immediate, and the spec must say so

The deployed cache is **in-process, not Redis**. `internal/cache/init.go:29-33` falls through to `InitializeInMemoryCache` for anything that is not explicitly `redis`, and `internal/config/config.yaml:514-519` ships `cache.enabled: false` with both `inmemory.enabled` and `redis.enabled` false.

The TTL is **30 minutes regardless of backend**, because `customerRepository.SetCache` (`internal/repository/ent/customer.go:578`) passes `cache.ExpiryDefaultRedis` explicitly rather than a backend-appropriate default. Note that `ExpiryDefaultInMemory` is 2 minutes (`internal/cache/expiry.go:6`), so the in-process path is carrying the Redis TTL — worth a separate fix, but the 30-minute window below is what ships today.

Consequences, both of which must be designed for rather than hidden:

**Per-pod eviction does not propagate.** `DeleteCache` (`:579`) clears the local process map only. With N replicas, the pod handling the suppression request evicts its own entry; the other N-1 keep serving the pre-suppression customer for up to 30 minutes. A single `DeleteCache` call cannot make suppression platform-immediate.

**Concurrent repopulation.** A reader that passes `GetCache` (`:124`) before the status flip reaches `SetCache` (`:570-571`) after it, writing the pre-suppression customer back under both the ID and lookup-key keys. `Get` has no re-read-after-write and no version check, so eviction-after-commit does not close this either.

Pick one of these and record the choice in the PR; do not leave it implicit:

1. **Accept a bounded window, documented.** Suppression is effective in Postgres immediately and in caches within the TTL. Then the external claim must be "within 30 minutes", not "immediate", and §1 and any contract language must match. This is the honest cheap option.
2. **Bypass cache on the customer read path** when a suppression check is involved — correctness over the cache hit. Simplest correct option; measure the hit-rate cost first.
3. **Shared cache or explicit invalidation fan-out.** Correct and immediate, but it is infrastructure work well beyond this phase.

Option 1 is the recommendation for Phase 1, because the contract commitment is 24 hours for suppression and a 30-minute cache window sits comfortably inside it. What is not acceptable is asserting immediate suppression while shipping option 1 — that is the gap this section exists to prevent.

### 5.5 Interaction with existing indexes

`idx_tenant_environment_external_id_unique` (`ent/schema/customer.go:99-102`) is unique and partial on `status = 'published'`. A suppressed customer leaves it, which **releases the customer-table external-ID uniqueness**. Only the registry index of §4.3 keeps the ID retired.

`idx_customer_tenant_environment_email` (`:105-107`) carries the same `published` predicate but is **not unique**, so it releases no constraint. It matters only for query planning.

Consequence for ordering: the release of external-ID uniqueness means FLE-1888 must precede FLE-1889 — and per §6.2 the window does not fully close even then, because `UpdateCustomer` can rename onto a retired ID through a path the index never guarded. "Ship 1888 first" and "accept and document the window" are therefore not equal options; the rename path has to be guarded regardless of order.

## 6. Service and API surface

### 6.0 Endpoints

Three routes, on the existing `/customers` group in `internal/api/router.go:199`. The resource is customer-scoped, so it belongs there; a separate `/privacy` namespace can come later if wanted, since nothing depends on the path.

```
POST   /v1/customers/:id/deletion-request    → 201, the recorded request + categories
GET    /v1/customers/:id/deletion-request    → 200 request + categories, 404 if none
GET    /v1/customers/deletion-requests?external_id=…  → 200, answers "what was deleted for X"
```

The third route is the one that justifies permanent external-ID retention, and it must keep working after the customer row itself is redacted in Phase 2.

On route ordering: **registration order does not matter here.** Gin 1.12.0 (`go.mod:28`) uses a radix tree that prefers a static segment over a wildcard sibling regardless of order, so `/customers/deletion-requests` wins against `/customers/:id` either way. The comment at `internal/api/router.go:216` ("must come first!") is stale folklore from an older Gin and is disproved by its own route — `GET /usage` is registered *after* `GET /:id` at `:208` and works. Do not propagate it; fixing that comment is a welcome drive-by.

The real Gin hazard in this group is different: a **panic at startup** if two different wildcard names occupy the same position, e.g. adding `/customers/:slug` beside the existing `/customers/:id`. Reordering does not fix that; using the same param name does. The group already holds `/:id`, `/lookup/:lookup_key`, `/external/:external_id`, `/portal/:external_id` and `/usage`, so keep any new wildcard consistent with `:id`.

`POST` body: `requested_via` (`api` / `written`), defaulting to `api` when absent. **No `reason` field.** The schema's only free-text column is `failure_reason`, which records why an erasure failed rather than why it was requested; overloading it would corrupt the one field an operator reads when diagnosing a stuck request. A requester-supplied justification needs its own column, which Phase 1 does not add.

Response states plainly what has happened — `state: suppressed`, one category `customer_pg: pending` — and must not include any field implying deletion is complete. There is no `completed_at` in a Phase 1 response because nothing is complete.

Status codes: 201 on create; 200 on a repeat create, returning the existing request, since the operation is idempotent rather than conflicting; 404 when no request exists; 403 per existing auth middleware.

Authorization is the open item: this endpoint suppresses a customer across the whole platform, which is a destructive, externally-visible action. Phase 4 (FLE-1847) specifies super-admin authorization and feature gating. Phase 1 must not ship a weaker gate than Phase 4 will require, so settle this with FLE-1847's author before implementing rather than defaulting to ordinary tenant auth. If a decision is not available, gate the route behind a feature flag defaulted off and record that in the PR.

### 6.1 Service methods

```
CreateDeletionRequest(ctx, customerID, requestedVia) (*DeletionRequest, error)
GetDeletionRequest(ctx, customerID) (*DeletionRequest, []*DeletionCategory, error)
GetDeletionRequestByExternalID(ctx, externalID) (*DeletionRequest, []*DeletionCategory, error)
IsExternalIDRetired(ctx, externalID) (bool, error)
```

Use `s.DB.WithTx(ctx, func(txCtx) error {...})` — `internal/postgres/client.go:202`, nestable and writer-forcing. `internal/ee/service/customer.go:87-186` is the multi-write precedent to follow.

Inside the transaction:

1. Load the customer (bypassing the suppression filter — it is not suppressed yet).
2. Insert the request row with `state = suppressed`, `suppressed_at = now`, `external_id` from the customer, `retention_policy` snapshotted.
3. Insert one category row: `customer_pg` / `pending`.
4. Set the customer's status to `suppressed`.

**After the transaction commits**, drop both cache keys — ID and lookup-key, since `SetCache` writes both (`internal/repository/ent/customer.go:570-571`).

The cache drop must not be inside the transaction. Two reasons: it is not a database operation, so `WithTx` gives it no atomicity and a rollback would leave the cache evicted for a change that never happened; and more importantly, evicting before commit means a concurrent `Get` in the gap reads the still-published row from Postgres and repopulates both keys — suppression then silently does not take effect. Per §5.4, after-commit eviction is still not sufficient on its own across replicas; it is the floor, not the fix.

Idempotency: a second call for the same customer returns the existing request unchanged rather than erroring. The unique index on `(tenant_id, environment_id, customer_id)` is the backstop; handle the conflict rather than letting it surface as a 500.

### 6.2 Guarding external-ID writes

> **Implemented in FLE-1888.** Verified locations on current `develop`, superseding every `internal/service/...` path in this section:
>
> | What | Where |
> |---|---|
> | `CreateCustomer` guard | `internal/ee/service/customer.go:38` (func at `:31`) |
> | `UpdateCustomer` rename guard | `internal/ee/service/customer.go:255` (func at `:236`) |
> | Shared helper | `internal/ee/service/customer.go:121` `rejectRetiredExternalID` |
> | Event-path skip | `internal/ee/service/meter_usage_tracking_post_insert.go:223`, inside `ResolveCustomerForUsageEvent` (func at `:204`) |
> | Partial index on customers | `ent/schema/customer.go:138` |
>
> Two findings that differ from what this section predicted. **The event path has one caller, not two** — `handleMissingCustomer`, `prepareProcessedEvents` and `prepareProcessedEventsV2` do not exist on `develop`; the equivalent is `ResolveCustomerForUsageEvent` with a single caller at `:27`. And **`SetExternalID` has exactly two call sites**, both in `internal/repository/ent/customer.go` (`:63` create, `:341` update); three further `CustomerRepo.Update` callers (`entityintegrationmapping.go:370`, `chargebee/customer.go:251`/`:301`, `paddle/sync_service.go:90`) rewrite the same external ID while mutating metadata only, so they cannot introduce a retired one and are deliberately unguarded.
>
> Guards no-op when `CustomerDeletionRepo` is nil, so the existing `ServiceParams` literals in tests keep working.

Every path that can mint a customer must consult the registry, keyed on `(tenant_id, environment_id, external_id)`, before creating. `internal/ee/service/customer.go` is the obvious one; it is not the only one. Import, integration sync and the event-path auto-create workflow can all create customers, and a registry that only guards the public endpoint is not an enforcement mechanism.

The implementer must grep every creation call site and list them in the PR description. Missed creation paths are how a retired ID comes back.

**Two different behaviours, by caller type.**

*Explicit creation* — the public create endpoint, imports, anything where a caller asked for a customer to exist. Reject with `ierr.ErrAlreadyExists` (maps to HTTP 409 per `statusCodeMap`), with a hint that distinguishes **retired** from **currently in use**. A caller seeing a bare 409 cannot tell whether to pick a new ID or to stop. There is no dedicated error code for this and adding one is not warranted; the hint and reportable details carry the distinction.

*Renaming an existing customer's external ID* — this is a creation path in effect and was missed by the earlier draft. `internal/ee/service/customer.go:378-379` lets `UpdateCustomer` assign a new `ExternalID`, reaching `CustomerRepo.Update` (`:514` → `internal/repository/ent/customer.go:332`, `SetExternalID`). Exposed on `PUT /v1/customers` and `PUT /v1/customers/:id` (`internal/api/router.go:207`, `:209`).

The customer-table unique index is partial on `status = 'published'` (`ent/schema/customer.go:99-102`), so a suppressed customer's external ID is free in that index, and nothing consults the registry on update. A retired ID is therefore reusable through rename even with every creation path guarded.

**So the guard belongs on external-ID *writes*, not on creations.** Any code path that sets `customer.external_id` — create or update — consults the registry first. An acceptance criterion phrased as "no creation path can reuse it" is satisfiable while the ID is in fact reusable; phrase it as "no path can reuse it".

Unrelated bug noticed while reading this code, worth a separate ticket: `:380-387` appends `cust.ExternalID` to `old_external_ids` metadata *after* overwriting it at `:378`, so the trail records the new ID rather than the old one. `old_external_ids` is therefore not a reliable audit trail today and must not be relied on by this feature.

*Automatic creation on the event path* — **superseded, see the correction in §6.2 above.** On current `develop` the function is `ResolveCustomerForUsageEvent` with a single caller; `handleMissingCustomer`, `prepareProcessedEvents` and `prepareProcessedEventsV2` do not exist, and the line numbers in the rest of this subsection and in §9 come from the older layout. Treat the shape of the argument as current and every symbol name and line number in it as stale.

Here the correct behaviour is **skip, not error**. Check the registry first and return `(nil, nil)` for a retired external ID.

Returning an error would be wrong in a specific way: `:512-519` (and `:853-860`) log the error and return it to retry event processing. A retired external ID is retired permanently, so the retry can never succeed — the event retries forever.

The skip path already exists and needs no new machinery. `(nil, nil)` is already handled at `:521-528`, which logs and returns `results, nil` — skip the event, no error, no retry:

```
lookup by external ID fails
  → registry says retired     → return (nil, nil)  → event skipped, no retry
  → registry says not retired → existing auto-create workflow runs
```

Log the compliance skip distinctly from the existing "no auto-creation workflow configured" message at `:523`. These are operationally different — one is unconfigured, the other a deliberate block — and must be distinguishable.

### 6.3 Event paths that retry on not-found: the poison-pill risk

Adding a status predicate to `Get` makes a previously-succeeding read fail, and some event paths treat that failure as retryable. Suppression then produces an unbounded retry backlog. Two confirmed sites:

**`handleMissingCustomer`'s own tail.** After the workflow creates the customer, `:1325` does `CustomerRepo.Get(ctx, customerID)` and on error returns `Mark(ierr.ErrDatabase)` — an error, so both callers retry. The registry check does not cover this, because the registry is keyed on the deletion request's external ID while this `Get` uses the internal ID the workflow just returned. Any sequence where the workflow succeeds but the subsequent `Get` refuses the row retries forever.

**`costsheet_usage_tracking.go:328-336`** performs the same external-ID lookup and returns the error unconditionally (`return results, err`). It does not auto-create, so it needs no registry guard — but not-creating is not the same as not-retrying. Once a customer is suppressed, every cost-sheet event for them retries forever. The earlier draft dismissed this file as needing no guard without examining what it does with the error.

Required of FLE-1889: audit **every** event-processing path that reads a customer and classify its not-found behaviour as retry or skip. Any path that retries must be changed to skip for the suppressed case specifically — a suppressed customer is a permanent condition, and permanent conditions must not be retried. A generic "customer not found" may legitimately retry, since the customer may be created a moment later; these two cases now need distinguishing rather than sharing a branch.

## 7. Work breakdown

### FLE-1887 — suppression log tables (S)

Unblocked. Start here.

1. `ent/schema/customerdeletionrequest.go` and `ent/schema/customerdeletioncategory.go` per §4.
2. `make generate-ent` (runs `ent generate --feature sql/execquery ./ent/schema`).
3. `make generate-migration` to emit SQL into `migrations/ent/`, then review the generated DDL before committing it. Confirm the unique indexes appear without partial predicates.
4. `internal/domain/customerdeletion/` — model, `FromEnt`, `Repository` interface. Follow `internal/domain/scheduledtask/repository.go` for interface shape.
5. `internal/repository/ent/customerdeletion.go` — implement it. Tenant and environment scoping on every query, no exceptions.
6. `internal/repository/factory.go` — add the constructor next to `NewScheduledTaskRepository` (`:233`).
7. `cmd/server/main.go` — register in the DI provider list near `:175`.
8. Tests: add an in-memory store in `internal/testutil/` following the existing `inmemory_*_store.go` pattern. There is no mockery config and no `make mocks` target in this repo — mocks are hand-written in-memory stores. Cover idempotent creation under concurrent duplicate requests, and category disposition readback.

### FLE-1888 — permanent external-ID registry (S)

Blocked by FLE-1887.

1. Confirm the unique index from §4.3 exists with no partial predicate.
2. `IsExternalIDRetired` + `GetDeletionRequestByExternalID` on the repository.
3. Wire rejection into every path that **writes** `customer.external_id`, create or update. Reject with `ierr.ErrAlreadyExists` (→ 409 per `internal/errors/errors.go:29`) plus a hint distinguishing retired from in-use.
   - Creation is less fragmented than it looks: `CustomerRepo.Create` has exactly one caller, `internal/ee/service/customer.go:89`, and every surface routes through `customerService.CreateCustomer` — public endpoint (`internal/api/v1/customer.go:54`), temporal activity (`internal/temporal/activities/customer/customer_activities.go:71`), task/import (`internal/ee/service/task.go:716`), onboarding (`:758`), tenant (`internal/ee/service/tenant.go:90`), stripe (`integration/stripe/customer.go:272`, `subscription.go:318`), hubspot (`integration/hubspot/customer.go:139`). Nothing writes a customer via ent directly. One guard in `CreateCustomer` covers creation.
   - **The rename path is the one that actually leaks**: `internal/ee/service/customer.go:378` in `UpdateCustomer`. Guard it. Still enumerate creation paths in the PR to prove the single-chokepoint claim holds.
4. Wire the **skip** into `handleMissingCustomer` (`internal/ee/service/meter_usage_tracking_post_insert.go:1171`): check the registry on `(tenant_id, environment_id, external_id)` before the auto-create workflow, return `(nil, nil)` when retired. Do not return an error — `:512-519` would retry the event forever against a permanently retired ID. The `(nil, nil)` skip path already exists at `:521-528`. Log the compliance skip distinctly from the existing "no auto-creation workflow configured" message at `:523`.
5. Tests: retired ID cannot be re-created through any explicit path; an event for a retired external ID is skipped rather than retried and creates no customer; lookup by external ID returns request state plus category dispositions after suppression.

### FLE-1889 — read suppression (M — re-estimated after review, was S)

Blocked by FLE-1887 **and FLE-1888**. FLE-1888 is a hard blocker, not a preference: read suppression is what makes the customer lookup fail, and the retirement guard is what stops the resulting miss from entering the create-on-miss event path. Shipping 1889 first lets suppression's own 404 trigger the recreation of a suppressed customer.

1. `StatusSuppressed` in `internal/types/status.go`. No validator or enum to update — `Status` has no `Validate()`, and `varchar(20)` fits.
2. Predicate in `Get`; verify `GetByLookupKey` (already filters published at `:180`).
3. **`ApplyStatusFilter` at `:443`** — exclude `suppressed` in the empty-status branch, refuse explicit `status=suppressed`. This covers `List`/`Count`/`ListAll`; without it the S3 export at `sync/export/credit_usage_report.go:76` writes suppressed PII to object storage (§5.1).
4. Cache invalidation after commit, both keys. Decide and record the §5.4 option — the deployed cache is per-pod in-process with a 30-minute TTL, so "immediate" is not achievable by eviction alone.
5. Audit every event-processing path that reads a customer and classify not-found as retry or skip (§6.3). At minimum fix `handleMissingCustomer`'s tail `Get` at `:1325` and `costsheet_usage_tracking.go:328-336`, both of which currently retry forever on a permanent condition.
6. Service method to suppress, transactional with the request insert per §6.1.
7. Tests: suppressed customer returns not-found via both getters **and is absent from list/count/export**; a warm cache behaves as the chosen §5.4 option documents; an event for a suppressed customer is skipped, not retried; archived-customer behaviour unchanged.
8. PR description enumerates the consumers reached through all five read methods and states the cache decision explicitly.

### FLE-1890 — record-and-read endpoint (S)

Blocked by FLE-1889. Last in the phase: the endpoint is what makes suppression reachable, so it ships once suppression is actually safe.

1. DTOs in `internal/api/dto/` for request and category, following the existing dto conventions.
2. Handler in `internal/api/v1/customer.go` alongside the existing customer handlers, with swagger annotations matching the file's existing style — the repo generates SDKs from them (`make swagger`).
3. Three routes per §6.0, with the `external_id` query route registered before the `/:id` routes.
4. Settle authorization with FLE-1847's author before implementing; do not default to ordinary tenant auth for a platform-wide destructive action. Feature-flag off if undecided.
5. `make swagger` and commit the regenerated spec.
6. Tests: create returns 201 with `state: suppressed` and one `pending` category; repeat create returns 200 with the same request ID; lookup by external ID works; no response field implies completion.

### FLE-1837 — contracts and data disposition (M)

Unblocked, runs in parallel with FLE-1887. Already updated for the permanent-external-ID decision. Produces the retention annex, with the retained external ID as its own line.

### Sequence

```
FLE-1887 (tables) ── FLE-1888 (registry + skip) ── FLE-1889 (read suppression) ── endpoint
        │
FLE-1837 (contracts, parallel from the start)
```

FLE-1887 and FLE-1837 start together. Then strictly in order: **FLE-1889 follows 1888** because read suppression without the registry guard is self-defeating on the event path (§9), and the endpoint comes last because it is what first makes suppression reachable from outside.

## 8. Acceptance criteria for the phase

- A deletion request can be recorded over HTTP, returns `state: suppressed`, and is idempotent under repeat and concurrent calls.
- The suppressed customer returns not-found through `Get` and `GetByLookupKey`, **and is absent from `List`, `Count` and `ListAll`** including the scheduled S3 export, and cannot be opted back into via an explicit `status=suppressed` filter.
- Suppression takes effect within the window documented per §5.4, and the documented window matches what any external communication claims.
- The external ID is permanently retired: **no path can reuse it**, including the `UpdateCustomer` rename path, not merely no creation path.
- An event arriving for a suppressed or retired external ID is skipped, creates no customer, and **does not retry**. No event-processing path treats suppression as a retryable error.
- Lookup by external ID returns the request and its category dispositions, and keeps doing so indefinitely.
- No response field implies deletion is complete.
- Exactly one category row per request, `customer_pg` / `pending`.
- No customer data is deleted or redacted anywhere.
- Archived-customer behaviour is unchanged.
- Build and tests pass.

## 9. Risks and open items

**Create-on-miss on the event path is a confirmed resurrection hole, and it sets the ticket order.** The hole and the ordering it forces are current; the symbol names and line numbers below are from the pre-move layout and are superseded by §6.2's correction. Originally verified 2026-10-29:

- `internal/ee/service/meter_usage_tracking_post_insert.go:501` looks the customer up by external ID during event processing. On failure, `:510` calls `handleMissingCustomer`, which runs a customer-onboarding **workflow that creates the customer** and then loads it at `:1328`.
- `internal/ee/service/costsheet_usage_tracking.go:328` does the same lookup but returns the error without creating.

So on the feature-usage path, suppression is self-defeating if shipped alone: suppress a customer, receive one more event, lookup 404s, auto-create workflow fires, customer re-created — with suppression's own not-found as the trigger. The customer comes back with a fresh row and the same external ID.

Resolution (§6.2): `handleMissingCustomer` checks the registry on `(tenant_id, environment_id, external_id)` before auto-creating and returns `(nil, nil)` for a retired ID, which the existing skip path at `:521-528` turns into a skipped event with no error and no retry. Returning an error instead would retry forever, since `:518` retries on error and a retired ID is retired permanently.

The registry check alone is **not** the whole fix. §6.3 covers the two places where a suppression-induced not-found is still treated as retryable: `handleMissingCustomer`'s own tail `Get` at `:1325`, which the registry check cannot catch because it keys on the internal ID the workflow returned, and `costsheet_usage_tracking.go:328-336`, which returns the lookup error unconditionally. Both must be handled in FLE-1889 or suppression ships a poison-pill event backlog.

Consequences for sequencing:

- **FLE-1888 must ship before FLE-1889, not after or in parallel.** The registry check is what makes `handleMissingCustomer` skip, and read suppression is what makes the lookup fail in the first place.
- The auto-create workflow is itself a customer-creation path, so it belongs in the FLE-1888 enumeration. This is exactly the class of path that a registry guarding only the public create endpoint would miss.

**Suppression releases customer-table external-ID uniqueness.** Per §5.5, a suppressed customer drops out of `idx_tenant_environment_external_id_unique`, so only the registry keeps the ID retired. Ship FLE-1888 before FLE-1889. Note this is necessary but not sufficient — the `UpdateCustomer` rename path (§6.2) bypasses the index regardless of ticket order and must be guarded on its own.

**Suppression is not immediate, and the cache is the reason.** Per §5.4 the deployed cache is per-pod in-process with a 30-minute TTL, so eviction on the suppressing pod leaves other replicas serving the pre-suppression customer. A decision is required, not an assumption. The recommendation is to accept and document a bounded window, which fits inside the 24-hour suppression commitment — but then nothing may claim immediacy.

**Scope grew during review.** FLE-1889 is no longer an S: it now covers three read paths rather than two, a cache decision, and an audit of event-path retry semantics. Re-estimate it to M before planning, or split the event-path audit out as its own ticket.

**`retention_policy` has no content yet.** FLE-1837 defines what goes in it. Phase 1 can write an empty object, but the column must exist from the first migration or every existing request row is unauditable.

**The external ID retention needs a lawyer's sign-off**, not just this document. §3 sets out the argument but deliberately does not assert a lawful basis: that needs an Article 6 basis and an Article 5(1)(e) storage-limitation rationale, both of which are counsel's to confirm. The annex line is a contractual commitment and this is engineering shape, not legal advice.

**Authorization is unsettled and must not be defaulted.** Per §6.0, this endpoint suppresses a customer platform-wide. FLE-1847 specifies super-admin authorization and feature gating for Phase 4; Phase 1 must not ship weaker. Settle it or flag the route off.

**This is a public repository.** Keep client names, contract terms and commercial detail out of specs, commit messages, PR bodies and code comments here. Note `work/` is untracked but **not** git-ignored, so a `git add -A` would commit whatever is in it — check `git status` before staging rather than relying on the directory name.

**Nothing here supports a claim of deletion.** If Phase 1 is communicated externally, it must be described as suppression with redaction and verification to follow, or we have overstated what shipped.
