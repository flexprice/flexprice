# FLE-1837 — Cleanup operation contracts and data disposition

Status: draft for review. Owner: platform. Parent: FLE-1883 (Phase 1), FLE-1836.

This document is the field-level counterpart to the store-level coverage table in the
Customer Data Deletion PRD. It defines what each cleanup operation receives, what it
must return, and for every field carrying customer identity whether that field is
deleted, redacted or retained — and on what basis.

Nothing here ships code. It is the contract Phase 2 implements against, one touchpoint
per ticket, and the retention annex that the DPA references.

Claims about schema and storage were verified against the tree; where one could not be,
it says "unverified" rather than guessing. Re-verify line numbers before relying on them.

---

## 1. Scope

Covers the erasure of **one end customer of one tenant, in one environment**. Out of
scope: whole-tenant termination, tenant users and auth, and the shared product catalog
(plans, prices, features, meters, coupons), none of which belong to an end customer.

Two stages, from the Phase 1 spec:

- **Suppression** — within 24 hours. The record stops being readable. Nothing is
  destroyed, so the window is reversible during cooldown.
- **Redaction** — within 30 days. Personal data is overwritten or deleted. Irreversible.

Phase 1 ships suppression only. This document describes the end state both stages aim
at, because a disposition decided per ticket drifts.

---

## 2. Operation contract

### 2.1 Input

Every cleanup operation receives the same value. It is deliberately not "the customer
row": by the time a late operation runs, the row may already be redacted.

| Field | Source |
|---|---|
| `tenant_id` | request context |
| `environment_id` | request context |
| `request_id` | deletion request row |
| `customer_id` | deletion request row |
| `external_id` | deletion request row |

Both identifiers are required. The internal ID is the join key across Postgres; the
external ID is the only customer key in ClickHouse, which never stores `customer_id`
except as a nullable column on `events`. An operation given only one of the two cannot
cover both stores.

### 2.2 Output

One row in `customer_deletion_categories` per touchpoint per request. That table exists
rather than a status column because a single request legitimately ends with one store
purged, another retained under tax law, and a third still pending.

The columns are the shipped ones, not an idealised shape:

| Column | Type | Rule |
|---|---|---|
| `category` | `varchar(60)` | One of §2.2.1. Never a free string |
| `disposition` | `varchar(30)` | One of §2.2.2 |
| `legal_basis` | text, `Optional()` | **Required when `disposition = retained`.** The schema cannot enforce this, so the service must; a retained row with an empty basis is the annex line that cannot be answered |
| `verified_at` | time, nillable | When the §2.4 evidence was taken |
| `verification_result` | JSON | The §2.4 evidence. Also carries in-flight state, §2.2.3 |

There is **no `failure_reason` on the category row.** It exists only on the request
(`ent/schema/customerdeletionrequest.go`). A failing touchpoint therefore records its
reason on the parent request and leaves its own disposition `pending` — see §2.2.2.

#### 2.2.1 Touchpoint registry

Phase 2 is one ticket per touchpoint, so the names are a shared namespace and must be
fixed here. Inventing them per ticket produces `events_ch`, `ch_events` and
`clickhouse_events` for one store, and because the unique index is on
`(request_id, category)` the duplicates are accepted and the §2.4 release gate counts
them as two satisfied categories.

Only `customer_pg` exists in code today
(`internal/domain/customerdeletion/model.go`). The rest are reserved here, following
the convention the column comment already sets:

| Constant | Value | Store |
|---|---|---|
| `CategoryCustomerPG` | `customer_pg` | `customers` row and its JSON fields |
| `CategorySubscriptionsPG` | `subscriptions_pg` | Subscriptions, line items, phases, pauses, schedules |
| `CategoryInvoicesPG` | `invoices_pg` | Invoices and line items |
| `CategoryPaymentsPG` | `payments_pg` | Payments, attempts, refunds, payment methods |
| `CategoryWalletsPG` | `wallets_pg` | Wallets and ledger |
| `CategoryGrantsPG` | `grants_pg` | Grants, commitments, associations, snapshots |
| `CategoryUsageDerivedPG` | `usage_derived_pg` | Usage records, meter checkpoints, revenue facts |
| `CategoryIntegrationsPG` | `integrations_pg` | Mappings, outbox, webhooks, alerts, tasks |
| `CategoryEventsCH` | `events_ch` | ClickHouse `events` |
| `CategoryRawEventsCH` | `raw_events_ch` | ClickHouse `raw_events` |
| `CategoryMeterUsageCH` | `meter_usage_ch` | ClickHouse `meter_usage` |
| `CategoryActivityLogsCH` | `activity_logs_ch` | ClickHouse `activity_logs` |
| `CategoryDeletedEventsCH` | `deleted_events_ch` | ClickHouse `deleted_events_logs` |
| `CategoryInvoicePDFs` | `invoice_pdfs` | Object storage |
| `CategoryExportArtifacts` | `export_artifacts` | CSV exports and import files |
| `CategoryCustomerCache` | `customer_cache` | Redis |

Adding a touchpoint means adding a constant here first. The longest value above is 20
characters, well inside `varchar(60)`.

#### 2.2.2 Dispositions

Four exist in code: `pending`, `purged`, `redacted`, `retained`
(`internal/domain/customerdeletion/model.go`). The column comment lists the same four.
**There is no `failed` disposition**, and an earlier draft of this document specified one.

A touchpoint that cannot complete stays `pending` and writes its reason to the request's
`failure_reason`. `pending` therefore means "not finished", covering both "not started"
and "failed, retryable" — which is correct for an orchestrator that retries, since both
require the same action. What `pending` must **not** do is hide work that is already
running; that is §2.2.3.

#### 2.2.3 In-flight work

A ClickHouse mutation outlives the call that submitted it (§6.2), so "submitted and
still running" is a normal state across an orchestration boundary, not an error. It is
distinguished from "not started" by `verification_result`, not by a separate
disposition:

- `disposition = pending`, `verification_result` empty → not started.
- `disposition = pending`, `verification_result` carrying `{"mutation_marker": "..."}`
  → submitted, in flight. A retry **looks that marker up in `system.mutations`** rather
  than submitting again (§6.2).

The request's `state` is **derived from its category dispositions**, not set
independently: a request is complete when every category has a terminal disposition.
Without that rule an orchestrator will set request state on its own and the two will
disagree.

### 2.3 Retry safety

Operations are retried by the orchestrator, so each must be idempotent in the strict
sense: running it twice must leave the same state and return the same disposition.

Three rules follow, and all three are already violable in the current code:

1. **Never key an operation on "rows that still exist".** A redaction that overwrites
   `name` to `[redacted]` must treat an already-redacted row as success, not as a
   missing target.
2. **A permanent condition must not be returned as a retryable error.** This is the
   poison-pill class: the usage-event path retries on error, and a retired external ID
   never becomes valid, so returning an error there retries forever. FLE-1888 returns
   `(nil, nil)` for exactly this reason.
3. **Verification is a separate read, after the write.** A disposition of `purged`
   backed by "the delete returned no error" is not evidence; §2.4 defines what is.
4. **A retry must find a prior attempt's in-flight work before starting new work.**
   For ClickHouse this is mandatory, not an optimisation: `mutate` embeds a
   freshly-generated marker per call, so a naive retry cannot see its predecessor's
   mutation and submits a second one over the same rows. Persist the marker per §2.2.3
   and check `system.mutations` first.

### 2.4 Verification evidence

`disposition` is a claim; `verification_result` is what backs it.

**Rule 1 — no status predicate, anywhere in an erasure path.** Every erasure read and
write queries by `(tenant_id, environment_id, customer_id)` with **no `status` clause
at all**, and does not go through the repositories' `ApplyStatusFilter` query builders.

This is the rule that prevents the most likely false `purged` in the whole design, so
the reason is worth stating. `ApplyStatusFilter` is not one function; it is a per-repository
interface method (`internal/repository/ent/query.go`) with ~39 implementations, and they
disagree: roughly 33 filter to `published` only, 5 allow-list `published` + `archived`
(`customer.go`, `wallet.go`, `meter.go`, `feature.go`, `creditgrant.go`), and the rest
vary. An earlier draft of this document claimed the default allows archived rows — that
is true of `customers` and false of most stores.

Two consequences compound:

- Suppression itself works by setting `customers.status` to a fourth value
  (`suppressed`, which does not exist in `internal/types/status.go` yet and arrives with
  FLE-1889). **A suppressed customer is therefore invisible to the default-filtered read
  path** — which is the point of suppression, and fatal for verification.
- A redaction operation that reads through the same filtered path sees **0 rows for the
  customer it is erasing**, concludes there is nothing to do, and reports success.

The deletion and its verification would share one blind spot, so the evidence confirms a
purge that never happened. The in-repo precedent for doing this correctly is the shipped
deletion repository itself: `GetByCustomerID` and `GetByExternalID` in
`internal/repository/ent/customerdeletion.go` filter on ID, tenant and environment and
carry no status predicate. Follow that.

**Rule 2 — evidence per store.**

| Store | Evidence required |
|---|---|
| Postgres, purge | Row count by `(tenant_id, environment_id, customer_id)`, no status predicate, taken after commit, expected 0 |
| Postgres, redact | The sentinel is present **and** `customers.updated_at` is not later than the recorded `redacted_at`. Sentinel presence alone is not evidence — see rule 3 |
| ClickHouse | Row count by **that table's own customer key** (`external_customer_id` for `events`, `raw_events`, `meter_usage`, `deleted_events_logs`; **`customer_id` for `activity_logs`**, which has no `external_customer_id` column), with `FINAL`, taken after the mutation reports done — not after it is submitted (§6.2) |
| File storage | Object listing under the customer's prefix, expected empty |
| Cache | Not verifiable by count; verified by the absence of the key, with the TTL and the repopulation race stated (§7) |

`FINAL` is required because `events`, `raw_events`, `meter_usage` and `activity_logs` are
all `ReplacingMergeTree`: a count without it is a deduplicated view, not a statement
about what is on disk. `deleted_events_logs` is plain `MergeTree` and needs none. The
existing code already does this — `event_deletion.go` counts `FROM meter_usage FINAL`
with a comment saying the count must match what the mutation removed.

**Rule 3 — redaction evidence must not be circular.** The sentinel is `[redacted]`
(§5.1), and `name` is `NotEmpty()`, so `[redacted]` is a value a tenant can set through
the normal API. Asserting `name = '[redacted]'` therefore cannot distinguish a redacted
row from a tenant who named a customer that, nor from a row that was redacted and then
overwritten with fresh PII.

Nothing currently prevents the overwrite: `rejectRetiredExternalID`
(`internal/ee/service/customer.go`) guards the **external ID** only, so `UpdateCustomer`
can write a new `name` and `email` onto a redacted row. Two requirements follow:

- Record the redaction timestamp in `verification_result` and compare it against
  `customers.updated_at`, so a later write is detectable.
- Reject writes to the PII fields of a customer whose deletion request has `redacted_at`
  set. The request row already carries `redacted_at` and `irreversible_started_at`
  (`ent/schema/customerdeletionrequest.go`), so this is one check, not a new mechanism.

A count of 0 taken while an asynchronous mutation is still running is the other likely
false `purged`. See §6.2.

### 2.5 Stage, prerequisites and fences

Phase 2 is often described as "one ticket per touchpoint, parallelizable". That is only
true with this table. Without it the dependencies surface during implementation and the
tickets get reworked.

| Category | Stage | Prerequisites | Ingest fence |
|---|---|---|---|
| `customer_pg` | suppression, then redaction | **Runs last in redaction.** Every traversal-based touchpoint resolves ownership through the customer and its related rows | no |
| `subscriptions_pg` | redaction | — | no |
| `invoices_pg` | redaction | — | no |
| `payments_pg` | redaction | **`invoices_pg` must not have removed invoices.** `payment` has no `customer_id` (§3); it is reachable only via `destination_id` → invoice | no |
| `wallets_pg` | redaction | — | no |
| `grants_pg` | redaction | — | no |
| `usage_derived_pg` | redaction | Blocked on §9.2 — retain or delete is undecided | no |
| `integrations_pg` | suppression | Must stop **future** disclosure before redaction runs (§7, webhooks) | no |
| `events_ch` | redaction | — | **yes** |
| `raw_events_ch` | redaction | — | **yes** |
| `meter_usage_ch` | redaction | — | **yes** |
| `activity_logs_ch` | redaction | Blocked on §6.3 — the minimal retained record must be defined first | no |
| `deleted_events_ch` | redaction | — | no |
| `invoice_pdfs` | suppression (revoke), redaction (delete) | Dispute window must be quantified (§8 line 7) | no |
| `export_artifacts` | redaction | Blocked on §9.3 | no |
| `customer_cache` | suppression | Runs **after** the `customers` status flip commits, never inside the transaction | no |

**"Ingest fence" means the touchpoint cannot be verified while ingest continues.** The
sibling Phase 1 spec is explicit that there is no ingest suppression: events for a
suppressed external ID are still accepted and still land in ClickHouse. The retirement
check in `meter_usage_tracking_post_insert.go` short-circuits *customer onboarding* and
returns `(nil, nil)`; it does not stop the event being written. So a CH deletion races
live ingest for the same `external_customer_id`, and §2.4's "count expected 0" is not
satisfiable while that continues.

**The fence has no owner and no ticket.** It is a prerequisite for the three `*_ch`
redaction touchpoints, not an implementation detail of them, and it is tracked as §9.8.

**`customer_cache` is not simply "evict after commit".** The cache is shared Redis keyed
by customer ID, so eviction does propagate across replicas — but a concurrent read that
misses can repopulate it afterwards, including through `GetByLookupKey`, which is itself
uncached but warms the ID-keyed entry on a successful database read. The exposure is a
read already in flight across the commit, bounded by the 30-minute TTL. Caching also
ships disabled by default. The sibling spec documents this race and recommends accepting
the bounded window; this document does not get to state a flat "evicted" and leave the
race to be rediscovered.

---

## 3. Entity ownership rules

Which rows belong to the customer is not obvious, and getting it wrong either misses
data or destroys another customer's.

**Direct ownership** — the row carries `customer_id`. 20 ent schemas do:
`alertlogs`, `checkoutsession`, `commitment`, `contract`, `creditnote`,
`customerdeletionrequest`, `entitlement_grant`, `event_deletion_request`,
`invoice`, `invoice_line_item`, `journal_entry`, `meter_usage_checkpoint`,
`paymentmethod`, `revenue_fact`, `subscription`, `subscription_line_item`,
`usagerecord`, `wallet`, `wallettransaction`, `workflow_execution`.

**Indirect ownership** — the row belongs to the customer through a parent and must be
reached by traversal. Invoice line items, credit note line items, subscription line
items, phases, pauses and schedules, wallet transactions.

**`payment` is indirect, and this is easy to get wrong.** `ent/schema/payment.go` has
**no `customer_id` column at all**. A payment is reachable only through
`destination_id` → invoice → `invoice.customer_id`, and `payment_attempt` only through
`payment`. Two consequences: `payments_pg` cannot be implemented independently of
invoices (§2.5), and the traversal lands on an invoice that **may be payer-owned rather
than subject-owned**, so the payer-versus-usage-owner hazard below applies to `payment`
through a path that does not mention customers anywhere.

**Payer versus usage owner.** These are different customers in the same billing
relationship, and the distinction is load-bearing. `invoice` carries both `customer_id`
and `subscription_customer_id`. A deletion that filters only on `customer_id` misses
invoices where the subject is the usage owner but another customer is the payer — and
a deletion that filters on both without care reaches into the other customer's data.
**Every operation must state which role it is erasing.** This is the most likely source
of a cross-customer data loss incident in Phase 2.

**Archived rows are in scope, and the default filter hides them.** `status` is a
lifecycle column for most purposes — but suppression also uses it, which makes it a
deletion marker for the customer row specifically. Both statements are true of different
tables and an earlier draft of this document asserted only the first.

What matters operationally: most repositories' `ApplyStatusFilter` default to
`published` only, so an operation written against the default filter sees **neither**
archived rows **nor** the suppressed customer it is erasing. §2.4 rule 1 is the
mitigation and it is mandatory, not advisory.

**Shared configuration is preserved.** Plans, prices, features, meters and coupons are
tenant-level catalog. A customer-specific *snapshot* of shared configuration is the
customer's; the catalog entry is not.

---

## 4. The permanently retained external ID

The external ID is **retained permanently and never cleared**. This is a deliberate
exception to erasure, and the one most likely to be challenged, so it is stated here in
full rather than left implicit.

**What is retained.** One string per erased customer, in
`customer_deletion_requests.external_id`, with the unique index on
`(tenant_id, environment_id, external_id)` carrying no partial predicate so that a
soft-deleted row cannot release a retired identifier.

**Why.** Two reasons, and they are different:

1. **Anti-recreation.** Without a suppression list, the next import recreates the
   customer and the erasure is undone. A suppression list is the standard mechanism and
   requires keeping the identifier.
2. **Evidence.** A tenant must be able to ask, indefinitely, what was erased for a
   given external ID. An erasure that cannot be evidenced cannot be demonstrated.

**The legal position, stated honestly.** Permanent retention needs a lawful basis under
Article 6 and a storage-limitation rationale under Article 5(1)(e). **Article 5(2)
accountability does not supply that basis** — it obliges the controller to demonstrate
compliance with Article 5(1); it is not itself a ground for processing. An earlier draft
of the Phase 1 spec asserted 5(2) as the basis and was wrong. Counsel must confirm both
the Article 6 basis and the 5(1)(e) rationale before this reaches a DPA.

**What it is not.** The retained record is **not anonymised**. An external ID is
frequently itself personal data — an email, a phone number, a person's name. Describing
the post-erasure record as anonymous would be false, and would also remove the basis for
retaining it, since anonymous data needs none.

**Why plaintext rather than a hash.** Article 5(1)(c) minimisation asks whether the
purpose can be met with less, so this has to be answered rather than skipped.

It could be. Both registry methods are equality-only — `IsExternalIDRetired` is an
`Exist` on an equality predicate and `GetByExternalID` looks up a caller-supplied value
— so **neither reads the stored string back**, and both would work unchanged over a
keyed hash. Anti-recreation needs only a membership test; the evidence purpose needs
only comparison against an ID the tenant supplies.

Plaintext is chosen anyway, on operability: a hash cannot answer "which identifiers are
retired for this tenant", which is the question that arises during an incident or a DPA
audit, and a hash of a low-entropy identifier such as an email is weakly protected
regardless. Article 5(1)(c) requires justifying the choice, not taking the most minimal
option — this paragraph is that justification, and counsel should confirm it is enough.

**Cross-environment is a bypass, and it is deliberate.** Retirement is environment
scoped, so erasing a customer in production leaves that external ID freely creatable in
**any other environment of the same tenant**. The sandbox rationale above is sound in one
direction only; a data subject's erasure right is not environment scoped. Phase 2 must
state which environment classes share a retirement namespace — the likely answer is that
all production-grade environments share one, with sandbox carved out. Tracked as §9.9.

**The registry is the only control, and it fails open.** The `customers` unique index is
partial on `status = 'published'` (`ent/schema/customer.go`), so a suppressed or archived
customer leaves that index and its external ID becomes free there. Only the deletion
request's non-partial unique index refuses reuse, enforced by `rejectRetiredExternalID`
(`internal/ee/service/customer.go`). That guard returns nil when the deletion repository
is not wired, which keeps ~200 existing test fixtures working but means **an unwired
repository silently permits recreation of every retired identifier**. Any deployment
path that constructs the customer service without it loses the control with no error.

**Scope of retirement is per environment.** The same external ID can be retired in
sandbox and remain usable in production. A sandbox request erased no production data, so
it makes no promise about production, and a sandbox test must not permanently burn an
identifier in the tenant's live namespace.

The temporary-identifier lifecycle and per-tenant salt design are out of scope: they
existed only so an audit row could survive clearing of a temporary identifier, and
nothing is cleared.

---

## 5. Field-level disposition — Postgres

### 5.1 `customers`

The full field list, every one classified. Fields come from `ent/schema/customer.go`
plus `BaseMixin`, `EnvironmentMixin` and `MetadataMixin`.

| Field | Disposition | Basis / note |
|---|---|---|
| `id` | **retained** | Internal ID, not personal data; the join key that makes the remaining financial rows attributable without re-identifying a person |
| `external_id` | **retained permanently** | §4 — anti-recreation and evidence. Needs the Article 6 + 5(1)(e) sign-off |
| `name` | **redacted** | Personal data, no retention duty |
| `email` | **redacted** | Personal data |
| `contact` | **redacted** | Personal data (phone) |
| `address_line1`, `address_line2` | **redacted** | Personal data |
| `address_city`, `address_state`, `address_postal_code` | **redacted** | Personal data at this granularity |
| `address_country` | **open — see §9.1** | Tax determination may require the jurisdiction after the person is gone. Country alone is weakly identifying; city/postal are not retained either way |
| `contact_persons` (JSON) | **purged** | A JSON array of `{first_name, last_name, email, mobile, designation, department, salutation}`. Whole-field purge, not per-key redaction: it is a list of *other* identified people |
| `billing_currency` | retained | Financial attribute, not personal |
| `timezone` | **redacted** | Weakly identifying and serves no post-erasure purpose |
| `tax_treatment` | retained | Required to interpret historical invoices |
| `metadata` (JSON) | **purged** | Tenant-controlled free text; must be assumed to contain anything |
| `tenant_id`, `environment_id` | retained | Scoping, not personal |
| `status` | retained, set to the suppressed value | The suppression mechanism itself |
| `created_at`, `updated_at` | retained | Not personal to the end customer |
| `created_by`, `updated_by` | retained | The acting tenant user, not the end customer (§1) |

**Redaction sentinel.** Redacted string fields are overwritten with a constant
(`[redacted]`), not nulled and not randomised. Randomising makes the result untestable,
and a single sentinel is what lets verification assert redaction happened.

Nulling is not uniformly available: `name` is `NotEmpty()`, so it cannot be cleared,
while `email` is `Optional()` and `contact` is `Optional().Nillable()` and could be.
Using one sentinel everywhere rather than nulling where permitted keeps the verification
query uniform. Check the column width before choosing the sentinel —
`contact` is `varchar(20)`, which fits `[redacted]` but not a longer marker.

### 5.2 Denormalized customer identity elsewhere

Two tables copy the external ID rather than referencing it, so they do not follow the
customer row:

| Table | Field | Disposition |
|---|---|---|
| `usagerecord` | `customer_external_id` | **redacted or purged with the record** — classify in Phase 2 with the usage-derivative decision (§9.2) |
| `event_deletion_request` | `external_customer_id` | **redacted** — the request row is the other team's audit evidence and should survive; the identifier in it should not |

That list is complete for the *identifier-copy* pattern: a search of `ent/schema` for
`external_customer_id` / `customer_external_id` / `external_id` returns only
`customer.go`, `customerdeletionrequest.go` and those two. The third copy is outside
Postgres — `activity_logs.entity_label` (§6.3).

**Unclassified free-form columns on directly-owned rows.** These are not
`MetadataMixin` fields, so §5.4's `metadata` purge rule does not reach them, and each
can carry customer identity:

| Table | Column(s) | Note |
|---|---|---|
| `alertlogs` | `alert_info` (JSON) | Row carries `customer_id`; alert bodies embed identifying text |
| `checkoutsession` | `payment_provider_config`, `provider_result` (JSON), `success_url`, `failure_url`, `cancel_url`, `failure_reason` | Row carries `customer_id`; provider results hold gateway-side customer records and return URLs routinely carry identifiers in query strings |
| `paymentmethod` | `method_details` (JSON) | Row carries `customer_id` |
| `payment` | `gateway_metadata` (JSON) | Reached via invoice (§3) |
| `taxapplied` | `external_tax_details` (JSON) | A tax identifier is personal data for a sole trader |
| `commitment` | `invoice_label`, `invoice_description` | Row carries `customer_id` |
| `coupon_application` | `coupon_snapshot` (JSON) | Reached via subscription |

**Outbound and derived copies**, which leave the database entirely:

| Path | What it carries | Disposition |
|---|---|---|
| Webhook delivery | `internal/webhook/dto/customer.go` ships `ExternalID`, `Name`, `Email` and the full address to a tenant-configured endpoint on customer create/update | Past deliveries **cannot be recalled**. Suppression must stop future customer webhooks for a suppressed customer — that is why `integrations_pg` is a suppression-stage touchpoint (§2.5) |
| `systemevent.payload` | JSONB that can hold a serialized webhook body | **purged** |
| CSV exports | Six writers under `internal/ee/service/sync/export/` emit a `customer_name` column; `internal/repository/ent/wallet.go` selects `c.name AS customer_name` in raw SQL | §9.3 |
| `task.file_url`, `task.file_name` | Pointer to the export artifact above | **purged** with the artifact |

This second and third set were built by grep as well, so the same caveat applies: Phase 2
must re-derive per touchpoint rather than treat these tables as exhaustive. The first
set was complete; these are not guaranteed to be.

### 5.3 Financial records

Invoices, payments, refunds, credit notes, wallet transactions and journal entries are
**retained**, and this is the core tension of the whole feature: an erasure duty against
a retention duty.

It resolves by **redacting identity and keeping the transaction**. The financial row
keeps its amounts, dates, currency and `customer_id`; it loses nothing that makes it a
valid record and gains nothing that identifies a person once `customers` is redacted.

Verified: `invoice` carries **no PII snapshot** — no customer name, no billing address.
Only `customer_id`, `subscription_customer_id` and a `metadata` JSON blob. So retaining
an invoice retains no personal data beyond the pointer, provided `metadata` is purged.

Retention bases go in the annex (§8), one line each, because the periods differ by
jurisdiction and a single "7 years" claim would be wrong somewhere.

### 5.4 `metadata` everywhere

`MetadataMixin` puts a tenant-controlled `map[string]string` on many tables. Verified
present on `subscription`, `invoice`, `wallet`, `payment`, `creditnote`,
`checkoutsession` among others.

**Every `metadata` blob on a row owned by the subject is purged**, not inspected. The
same rule extends to the non-`MetadataMixin` JSON columns listed in §5.2 — notably
`paymentmethod.method_details`, `payment.gateway_metadata` and
`checkoutsession.provider_result`, none of which the word "metadata" reaches.
Inspecting means deciding per key whether a tenant's arbitrary string is personal, for
every tenant, forever. Purging is the only defensible default. Tenants who put
load-bearing configuration in customer-scoped metadata lose it on erasure; that is the
correct outcome and belongs in the contract.

---

## 6. Field-level disposition — ClickHouse

### 6.1 Tables and keys

Five tables carry customer identity. The sort key decides whether per-customer deletion
can seek or must skip-scan, so it is listed alongside the skip index:

| Table | Customer key | In sort key? | Personal payload |
|---|---|---|---|
| `events` | `external_customer_id`, nullable `customer_id` | **No** — `(tenant_id, environment_id, timestamp, id)` | `properties` (free-form) |
| `raw_events` | `external_customer_id` | Yes | `payload` (free-form, the unparsed request) |
| `meter_usage` | `external_customer_id` | Yes | `properties` |
| `deleted_events_logs` | `external_customer_id` | No — `(tenant_id, environment_id, request_id, event_id, meter_id)` | `properties`, `deleted_by` |
| `activity_logs` | `customer_id` | **No** — `(tenant_id, environment_id, occurred_at, id)` | `ip`, `user_agent`, `actor_id`, `entity_label`, `state` — see §6.3 |

**Disposition: delete, not redact**, for all five. These are append-only analytical
tables with no update path; a redaction would be a rewrite of every matching row, which
costs the same as a delete and leaves a row that proves the customer existed.

**`events` and `activity_logs` have the customer key outside the sort key**, so neither
can seek straight to the customer's rows. But both carry a **bloom filter on exactly the
erasure predicate** — `events` has `external_customer_id_idx … TYPE bloom_filter
GRANULARITY 8192` and `activity_logs` has `idx_customer_id … TYPE bloom_filter(0.01)
GRANULARITY 1`, whose DDL comment says it exists to skip granules without that customer.
So finding the rows is a granule-skipping scan, not a full read of every partition. An
earlier draft of this section claimed a full scan and presented it as the reason the
redaction window is 30 days. **That reasoning was wrong.**

The real cost driver is the mutation, not the search: `ALTER TABLE ... DELETE` **rewrites
every part that contains a matching row**, however quickly it found them. A customer with
sparse rows across a year of daily partitions forces a rewrite of a part per partition.

**Mitigation: scope the mutation to the partitions where the customer actually has
rows.** `events` is `PARTITION BY toYYYYMMDD(timestamp)` and `activity_logs` by
`toYYYYMM(occurred_at)`, so resolving the customer's partition set first and issuing one
bounded mutation per partition is dramatically cheaper than one unbounded mutation, and
it is what makes the deadline below survivable.

**The 10-minute deadline must be reconciled before `events_ch` is implemented.**
`mutate` waits on a `mutationDeadline` of 10 minutes and, on expiry, **kills its own
mutation** (`internal/repository/clickhouse/event_deletion.go`). A single unbounded
mutation over a year of `events` will not finish inside that, so reusing the mechanism
unchanged means every large erasure aborts the work it just started. Per-partition
scoping is what brings each mutation inside the budget. No duration is asserted here:
this document has no row counts and no cluster measurement, so Phase 2's first task on
that ticket is to measure, not to trust a number.

### 6.2 Mechanism, and the false-verification trap

The parallel event-deletion work already settled the mechanism: reuse
`EventDeletionLogRepository.mutate` in
`internal/repository/clickhouse/event_deletion.go` rather than re-deciding. Its
constraints — why `ALTER TABLE ... DELETE` rather than `DELETE FROM`, why
`mutations_sync = 0`, why a unique marker — are documented at the call site, which is
where they will stay correct.

Two things that mechanism does **not** give Phase 2, which this contract has to:

- **Retry identification.** The marker is generated per call, so a retry cannot find the
  mutation a previous attempt submitted and will issue a second one over the same rows.
  Persist the marker per §2.2.3 and look it up in `system.mutations` first (§2.3 rule 4).
- **Partition scoping.** `mutate` takes a predicate, not a partition. Per §6.1 the
  predicate should be bounded to the customer's actual partitions.

**Because the mutation is asynchronous, a row count taken after submission is
meaningless.** The rows are still there. An operation that submits a delete and
immediately counts will report `purged` with evidence of 0 affected only by luck, and
will more often report a non-zero count and retry a mutation that was already running.
Verification must wait for the mutation to report done in `system.mutations`, then count.

**Existing coverage is `events` and `meter_usage` only.** `raw_events`,
`activity_logs` and `deleted_events_logs` have **no deletion path today**. Three
touchpoints, three Phase 2 tickets, and `activity_logs` is the awkward one (§6.3).

### 6.3 `activity_logs` and the minimal audit record

`activity_logs` is the one table where "delete everything" conflicts with keeping an
audit trail, and it is **the most important finding in this document**.

**`state` is a complete copy of the `customers` row, always, by construction.**
`internal/activity/entities.go` registers the customer entity with
`Snapshot: snapshotOf(customer.FromEnt)`, and `customer.FromEnt`
(`internal/domain/customer/model.go`) returns every field — `ExternalID`, `Name`,
`Email`, `Contact`, `ContactPersons`, the full address, `Timezone`, `Metadata`. An
earlier draft of this section said `state` *can* contain the record. It does not merely
can.

**`entity_label` is a second permanent copy of the external ID**, in ClickHouse, outside
the registry. The same registration sets `Label: ["external_id"]`, so for a customer the
label column holds the external ID verbatim. §4 describes the external ID as retained in
one place; this is the other place, and it was unclassified.

**Therefore every disposition in §5.1 is void unless this touchpoint covers it.**
Redacting `name` and purging `contact_persons` in Postgres achieves nothing while a
ClickHouse row holds the whole pre-redaction struct, and a §2.4 Postgres row count will
report `redacted` truthfully while the data stays fully readable one store over. The
`customer_pg` and `activity_logs_ch` categories are not independent in effect, even
though they are separate tickets.

The table also holds `ip`, `user_agent` and `actor_id` — personal data in their own
right, and `ip`/`user_agent` relate to whoever made the request rather than to the
subject.

**Proposed split**, to be confirmed:

- **Every existing row for the subject is deleted.** No pre-existing `activity_logs` row
  is retained, because any customer row carries the full snapshot and `entity_label`.
  This includes the `CustomerArchived` row that suppression itself writes
  (`internal/repository/ent/customer.go`), which is erasure-adjacent and still carries
  the complete customer.
- **The retained audit record is a new, purpose-written row**, not a filtered existing
  one: `occurred_at`, `action`, `entity_type`, `customer_id`, `request_id` and nothing
  else.

The distinction matters. Retaining a *subset of columns* of an existing row means the
row still physically holds `state` unless it is rewritten, and a `ReplacingMergeTree`
rewrite is the same cost as a delete. Writing a fresh minimal row and deleting all
originals is both cheaper and actually achieves the redaction.

The erasure's own audit record must be written so it never contains PII in the first
place, rather than written fully and redacted later. A record that has to be erased to
be compliant is not an audit record.

---

## 7. Other stores

| Store | Disposition | Note |
|---|---|---|
| Invoice PDFs (object storage) | Access revoked at suppression; **deleted** after the dispute window | `invoice.invoice_pdf_url` exists. **Unverified:** I found no in-repo renderer writing customer identity into a PDF, so the assumption that it contains rendered identity is untested and the generator may live outside this repo. Verify before the annex commits to it. If it does, deleting the invoice row's PII while keeping the PDF is the leak this line prevents |
| Managed exports / imports | **deleted or regenerated** without the subject | Mixed-customer files cannot be deleted wholesale; regeneration is the only correct answer and it is expensive. Open: §9.3 |
| Redis cache | Key **evicted** after commit | One key, the ID key. `GetByLookupKey` is uncached. Caching ships disabled by default; where enabled, TTL is 30 minutes and eviction propagates because the cache is shared |
| Kafka / pipeline | Suppression enforced at the consumer; **expiry** relied on for in-flight | Verify the topic retention is inside the 30-day commitment, or record an approved exception |
| Temporal | New deletion histories carry **no PII**; existing histories age out | A workflow argument containing an external ID is a PII copy with its own retention |
| Backups | **Expire on rotation**; erasure **reapplied after any restore** | A restore silently resurrects an erased customer. The reapply step is the control, and it has no owner yet (§9.4) |
| Provider copies (Stripe, Chargebee, HubSpot, Paddle) | **Out of our boundary**; documented, not erased by us | We cannot erase another controller's copy. The DPA must say so plainly rather than implying coverage |

---

## 8. Retention annex

One line per retained category, with its basis. This is the table the DPA references;
**every line needs counsel's confirmation**, and the bases below are the engineering
rationale, not legal advice.

| # | Retained | Period | Stated basis |
|---|---|---|---|
| 1 | `customers.external_id` | **Permanent** | Anti-recreation and erasure evidence. Requires Article 6 basis + Article 5(1)(e) rationale — **not** Article 5(2) (§4) |
| 2 | `customers.id` | Permanent | Non-personal internal key; keeps retained financial rows attributable without identifying a person |
| 3 | Invoices and line items | Per jurisdiction | Statutory tax and accounting record retention. **Counsel owns the citations** |
| 4 | Payments, refunds, credit notes | Per jurisdiction | As above; a refund is part of the taxable record |
| 5 | Wallet transactions, journal entries | Per jurisdiction | Accounting record of consideration received |
| 6 | Revenue facts, meter usage checkpoints | **Open — §9.2** | Financial summary or usage derivative, depending on whether the figure is reconstructible |
| 7 | Invoice PDFs | **Open — unquantified** | Evidence during a dispute; no basis afterwards. "Dispute window" is not a retention period until it carries a number, and the contents claim is unverified (§7) |
| 8 | Erasure audit record | Permanent | Demonstrating the erasure happened. A **newly written** minimal row, not a filtered existing one — see §6.3, which is why this line is deliverable at all |
| 9 | Deletion request + category rows | Permanent | The evidence itself |
| 10 | Tax treatment, billing currency | Permanent, with the financial record | Required to interpret a retained invoice |

Line 1 is the one a regulator will ask about first. It must not be left implicit, and it
must not be described as anonymisation.

---

## 9. Open decisions

Recorded explicitly rather than inferred. **Schema presence does not mean erasure
support** — several of these are open precisely because a column exists with no path to
clear it.

### 9.1 `address_country`
Retain for tax determination, or redact with the rest of the address? Retaining a
country for an otherwise-redacted person is weakly identifying; redacting it may break
historical tax reinterpretation. **Needs:** finance + counsel.

### 9.2 Revenue facts and usage checkpoints
Are these financial summaries (retain, annex line 6) or usage derivatives (delete)? The
test is whether the figure is reconstructible from retained invoices. If it is, it is a
cache and should be deleted; if it is not, it is a record. **Needs:** finance.

### 9.3 Mixed-customer export artifacts
A CSV containing 500 customers, one of whom is erased. Delete the artifact, regenerate
without the subject, or enforce an approved expiry? Regeneration is correct and
expensive; expiry is cheap and leaves PII for its duration.

Scoped against real code rather than a hypothetical: six writers under
`internal/ee/service/sync/export/` emit a `customer_name` column — the credit usage
report, usage analytics export, wallet debit and ledger exports, and credit top-up
export — and `internal/repository/ent/wallet.go` selects `c.name AS customer_name` in
raw SQL to populate them. `task.file_url` points at the result. **Needs:** product +
counsel.

### 9.4 Post-restore reapplication
A database restore resurrects erased customers. Reapplying erasure after restore is the
control, and it currently has **no owner, no runbook and no trigger**. **Needs:** an
owner before public erasure is enabled.

### 9.5 Cooldown length and cancellation
The reversible window between suppression and redaction. **This is the primary
reversibility control, not a tenant convenience setting** — once
`irreversible_started_at` is set the request cannot be cancelled, and redaction is the
only irreversible stage. Longer is safer, worse for the commitment. **Needs:** product;
must fit inside the 30-day redaction commitment. See §9.11.

### 9.6 Kafka topic retention
Is in-flight event retention inside the 30-day commitment? Unverified — I did not check
the broker configuration, which is infrastructure rather than this repo. **Needs:**
verification, then either a statement or an exception.

### 9.7 Endpoint authorization
FLE-1847 specifies super-admin auth and feature gating for the Phase 4 public API.
Phase 1's internal endpoint must not be weaker. **Needs:** FLE-1847's owner, or the
route ships behind an off flag.
### 9.8 Ingest fence for the ClickHouse touchpoints
There is no ingest suppression: events for a suppressed external ID are still accepted
and still land in ClickHouse. So a CH deletion races live ingest and §2.4's "count
expected 0" is not satisfiable while that continues. This is a **prerequisite** for
`events_ch`, `raw_events_ch` and `meter_usage_ch` (§2.5), not a detail inside them, and
it has no owner and no ticket. **Needs:** an owner before those three can be verified.

### 9.9 Cross-environment retirement namespace
Erasing in production leaves the external ID creatable in another environment of the
same tenant (§4). Which environment classes should share one retirement namespace?
Likely all production-grade environments, with sandbox carved out. **Needs:** product +
counsel, since it is a question about the scope of the subject's right, not a config
value.

### 9.10 PII write-fence after redaction
Nothing currently stops `UpdateCustomer` writing fresh `name`/`email` onto a redacted
row; the existing guard covers the external ID only. §2.4 rule 3 requires the fence.
Open question is its scope: reject all writes to a redacted customer, or only to the PII
fields? **Needs:** a decision before the `customer_pg` redaction ticket.

### 9.11 Erasure blast-radius controls
A single authorized call permanently burns an external ID (immutable, non-partial unique
index) and purges `metadata` across subscriptions, invoices, wallets, payments, credit
notes and checkout sessions. There is no rate limit, no bulk threshold, no second
approval and no cap on concurrent ClickHouse mutations — and N concurrent erasures are N
concurrent mutation sets on the largest tables. §9.5's cooldown is the **primary
reversibility control**, not a tenant convenience. **Needs:** product, before the public
API in Phase 4.

---

## 10. What this unblocks

- **FLE-1884 (Phase 2)** — one ticket per touchpoint from the §2.2.1 registry, each
  implementing §2's contract and its §5/§6 disposition. Tickets are parallelizable
  **only within the constraints of §2.5**: `customer_pg` redaction runs last,
  `payments_pg` depends on `invoices_pg`, and the three event-table touchpoints are
  blocked on the ingest fence (§9.8). The three ClickHouse tables with no deletion path
  (§6.2) are three of those tickets.
- **FLE-1885 (Phase 3)** — orchestration against the §2.2 result contract and §2.3
  retry rules.
- **FLE-1886 (Phase 4)** — the validation endpoint reports §2.4 evidence per category.
- **The DPA** — §8 is the annex; §9 is what cannot be committed to yet.
