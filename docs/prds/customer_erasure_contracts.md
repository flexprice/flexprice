# FLE-1837 — Cleanup operation contracts and data disposition

Status: draft for review. Owner: platform. Parent: FLE-1883 (Phase 1), FLE-1836.

This document is the field-level counterpart to the store-level coverage table in the
Customer Data Deletion PRD. It defines what each cleanup operation receives, what it
must return, and for every field carrying customer identity whether that field is
deleted, redacted or retained — and on what basis.

Nothing here ships code. It is the contract Phase 2 implements against, one touchpoint
per ticket, and the retention annex that the DPA references.

Every claim about schema or storage was verified against the tree at the commit this
document was added on. Where a claim could not be verified it says so rather than
guessing; §9 lists what remains open. The Phase 1 spec has already been wrong twice by
citing files that did not exist, so treat a line number here as a starting point and
re-verify before relying on it.

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

| Field | Source | Why it is in the input |
|---|---|---|
| `tenant_id` | request context | Every read and write is tenant scoped |
| `environment_id` | request context | Retirement and erasure are environment scoped |
| `request_id` | deletion request row | Correlates every category outcome to one request |
| `customer_id` | deletion request row | Internal ID; survives redaction, so it stays joinable |
| `external_id` | deletion request row | Permanently retained (§4); the only key for stores that never held the internal ID |

Both identifiers are required. The internal ID is the join key across Postgres; the
external ID is the only customer key in ClickHouse, which never stores `customer_id`
except as a nullable column on `events`. An operation given only one of the two cannot
cover both stores.

### 2.2 Output

```
CategoryResult {
  category        string  // the touchpoint, e.g. customer_pg, events_ch
  disposition     enum    // purged | redacted | retained | pending | failed
  legal_basis     string  // required when disposition = retained
  verified_at     time    // when the count below was taken
  verification    json    // store-specific evidence, see 2.4
  failure_reason  string  // required when disposition = failed
}
```

One row per touchpoint per request, which is why `customer_deletion_categories` exists
as its own table rather than a status column: a single request legitimately ends with
one store purged, another retained under tax law, and a third still pending.

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

### 2.4 Verification evidence

`disposition` is a claim; `verification` is what backs it. Minimum per store:

| Store | Evidence required |
|---|---|
| Postgres | Row count for the customer in the touchpoint's tables, taken after commit, expected 0 for purge or N with all personal fields at their redacted sentinel |
| ClickHouse | Row count by `external_customer_id`, taken after the mutation reports done — not after it is submitted (§6.2) |
| File storage | Object listing under the customer's prefix, expected empty |
| Cache | Not verifiable by count; verified by the absence of the key, with the TTL window stated |

A count of 0 taken while an asynchronous mutation is still running is the single most
likely way for this feature to report a false `purged`. See §6.2.

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
items, phases, pauses and schedules, payment attempts, wallet transactions.

**Payer versus usage owner.** These are different customers in the same billing
relationship, and the distinction is load-bearing. `invoice` carries both `customer_id`
and `subscription_customer_id`. A deletion that filters only on `customer_id` misses
invoices where the subject is the usage owner but another customer is the payer — and
a deletion that filters on both without care reaches into the other customer's data.
**Every operation must state which role it is erasing.** This is the most likely source
of a cross-customer data loss incident in Phase 2.

**Archived rows are in scope.** `status` is a lifecycle column, not a deletion marker.
`ApplyStatusFilter` defaults to allow-listing `published` and `archived`, so an
operation written against the default filter sees archived rows — but an operation that
passes an explicit status will not, and will silently under-delete.

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

**Scope of retirement is per environment.** The same external ID can be retired in
sandbox and remain usable in production. A sandbox request erased no production data, so
it makes no promise about production, and a sandbox test must not permanently burn an
identifier in the tenant's live namespace.

**Out of scope as a consequence.** The temporary-identifier lifecycle and the
per-tenant salt/secret design are dropped. They existed only so an audit row could
survive the clearing of a temporary identifier. Nothing is cleared, so neither is needed.

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
| `created_by`, `updated_by` | retained | **Verified: these hold the acting *tenant user's* ID, not the end customer's.** Not in scope for end-customer erasure |

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

This list was built by searching for the known denormalized column names. A column
named something unanticipated would not appear, so Phase 2 must re-derive it per
touchpoint rather than trusting this table to be exhaustive.

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

**Every `metadata` blob on a row owned by the subject is purged**, not inspected.
Inspecting means deciding per key whether a tenant's arbitrary string is personal, for
every tenant, forever. Purging is the only defensible default. Tenants who put
load-bearing configuration in customer-scoped metadata lose it on erasure; that is the
correct outcome and belongs in the contract.

---

## 6. Field-level disposition — ClickHouse

### 6.1 Tables and keys

Five tables carry customer identity. The sort key decides whether per-customer deletion
is cheap or a full scan, so it is listed:

| Table | Customer key | In sort key? | Personal payload |
|---|---|---|---|
| `events` | `external_customer_id`, nullable `customer_id` | **No** — `(tenant_id, environment_id, timestamp, id)` | `properties` (free-form) |
| `raw_events` | `external_customer_id` | Yes | `payload` (free-form, the unparsed request) |
| `meter_usage` | `external_customer_id` | Yes | `properties` |
| `deleted_events_logs` | `external_customer_id` | No — `(tenant_id, environment_id, request_id, event_id, meter_id)` | `properties`, `deleted_by` |
| `activity_logs` | `customer_id` | **No** — `(tenant_id, environment_id, occurred_at, id)` | `ip`, `user_agent`, `entity_label`, `state` |

**Disposition: delete, not redact**, for all five. These are append-only analytical
tables with no update path; a redaction would be a rewrite of every matching row, which
costs the same as a delete and leaves a row that proves the customer existed.

**`events` and `activity_logs` have the customer key outside the sort key.** A
per-customer delete on those two scans every partition in retention rather than seeking.
This is a cost and duration fact Phase 2 must plan around — it is the main reason the
30-day redaction window is 30 days and not 24 hours. Do not discover it during the first
production erasure.

### 6.2 Mechanism, and the false-verification trap

The parallel event-deletion work already settled the mechanism, and Phase 2 should reuse
it rather than re-decide:

- **`ALTER TABLE ... DELETE`, not `DELETE FROM`.** A lightweight delete fails on a table
  carrying a projection, and production `events` carries one.
- **`mutations_sync = 0`.** A blocking submit hits the client read deadline while still
  succeeding server-side, which looks like a failure and gets retried.
- A unique marker is appended to the predicate so the submitted mutation can be
  identified in `system.mutations` and waited on.

Both facts are in `internal/repository/clickhouse/event_deletion.go`, with the reasoning
in comments.

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
audit trail. It holds `ip`, `user_agent`, `entity_label` and a `state` snapshot that can
contain the whole pre-redaction record — so it is simultaneously the richest personal
data outside `customers` and the thing an auditor would want.

**Proposed split**, to be confirmed:

- Rows describing *the erasure itself* are retained, reduced to: `occurred_at`,
  `action`, `entity_type`, `customer_id`, `request_id`. No `ip`, no `user_agent`, no
  `entity_label`, no `state`.
- Every other row for the subject is deleted.

The erasure's own audit record must be written so it never contains PII in the first
place, rather than written fully and redacted later. A record that has to be erased to
be compliant is not an audit record.

---

## 7. Other stores

| Store | Disposition | Note |
|---|---|---|
| Invoice PDFs (object storage) | Access revoked at suppression; **deleted** after the dispute window | The PDF contains rendered identity even though the invoice row does not. Deleting the row and keeping the PDF is the leak this line exists to prevent |
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
| 3 | Invoices and line items | Per jurisdiction | EU VAT Directive 2006/112 Art. 244–248; DE HGB §257 / AO §147; India Companies Act §128 |
| 4 | Payments, refunds, credit notes | Per jurisdiction | As above; a refund is part of the taxable record |
| 5 | Wallet transactions, journal entries | Per jurisdiction | Accounting record of consideration received |
| 6 | Revenue facts, meter usage checkpoints | **Open — §9.2** | Financial summary or usage derivative, depending on whether the figure is reconstructible |
| 7 | Invoice PDFs | Dispute window, then deleted | Evidence during a dispute; no basis afterwards |
| 8 | Erasure audit record | Permanent | Demonstrating the erasure happened; minimal fields only (§6.3) |
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
expensive; expiry is cheap and leaves PII for its duration. **Needs:** product + counsel.

### 9.4 Post-restore reapplication
A database restore resurrects erased customers. Reapplying erasure after restore is the
control, and it currently has **no owner, no runbook and no trigger**. **Needs:** an
owner before public erasure is enabled.

### 9.5 Cooldown length and cancellation
The reversible window between suppression and redaction. Longer is safer for the tenant,
worse for the commitment. **Needs:** product; must fit inside the 30-day redaction
commitment.

### 9.6 Kafka topic retention
Is in-flight event retention inside the 30-day commitment? Unverified — I did not check
the broker configuration, which is infrastructure rather than this repo. **Needs:**
verification, then either a statement or an exception.

### 9.7 Endpoint authorization
FLE-1847 specifies super-admin auth and feature gating for the Phase 4 public API.
Phase 1's internal endpoint must not be weaker. **Needs:** FLE-1847's owner, or the
route ships behind an off flag.

---

## 10. What this unblocks

- **FLE-1884 (Phase 2)** — one ticket per touchpoint, each implementing §2's contract
  and its §5/§6 disposition. The three ClickHouse tables with no deletion path (§6.2)
  are three of those tickets.
- **FLE-1885 (Phase 3)** — orchestration against the §2.2 result contract and §2.3
  retry rules.
- **FLE-1886 (Phase 4)** — the validation endpoint reports §2.4 evidence per category.
- **The DPA** — §8 is the annex; §9 is what cannot be committed to yet.
