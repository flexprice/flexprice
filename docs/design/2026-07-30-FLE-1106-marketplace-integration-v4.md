# Marketplace Integration — Cancellation Flush & Entity Mapping Lifecycle

Ticket: FLE-1106
Author: Tsage
Status: design, pending approval
Scope: two lifecycle defects and the final-usage flush that closes them. Applies to all three
marketplaces (AWS, GCP, Azure) and, for the mapping defect, to every integration provider.

Builds on [v3](2026-07-23-FLE-1071-marketplace-integration-v3.md). Everything in v3 stands except
where §6 marks it superseded.

---

## 1. What this changes

| # | Problem | Fix |
| --- | --- | --- |
| 1 | Re-linking an entity after unlinking silently leaves the mapping `archived`. The API returns 200 but no active mapping exists. | Treat an archived row as absent: create a new published row instead of updating it (§2). |
| 2 | Cancelling a subscription leaves its marketplace mapping `published`, and the final active-period usage is never reported inside the marketplaces' post-cancellation windows. | A one-shot flush on cancellation: report the backlog, compute and report the final window, then archive the mapping (§3). |

The driver for #2 is timing. v3 §10 assumed the tenant could wait for the 6-hour snapshot cron's
4–10h lag before archiving. AWS and GCP give roughly **one hour** after cancellation, so that
assumption loses the final usage of every cancelled marketplace subscription (§4).

---

## 2. Defect 1 — re-link after unlink

### 2.1 What happens today

`DelinkIntegrationMapping` soft-deletes: it flips `status` to `archived` and leaves the row and its
unique-index columns untouched. That part is correct.

The defect is in `upsertEntityMapping`
([entityintegrationmapping.go](../../internal/ee/service/entityintegrationmapping.go)). It looked up
existing mappings with `types.NewNoLimitQueryFilter()`, which leaves `Status` nil, so
`ApplyStatusFilter` applies **no status predicate** and archived rows match. With only an archived row
present, the service updated it in place and returned it — `Update` writes back whatever status the
caller passed, so the row stayed `archived`. The caller got a 200 and a mapping object, but no active
mapping existed.

The marketplace path was never affected: `RegisterAgreement` / `createMappingIfAbsent`
([marketplace.go](../../internal/ee/service/marketplace.go)) already queried with
`NewNoLimitPublishedQueryFilter()`.

### 2.2 The fix

One-line change: query published-only. An archived row is then invisible to the lookup, so a re-link
falls through to the existing create path and gets a new published row. The archived row is never
touched.

```go
filter := &types.EntityIntegrationMappingFilter{
    QueryFilter: types.NewNoLimitPublishedQueryFilter(),   // was NewNoLimitQueryFilter()
    EntityID:    req.EntityID,
    ...
}
```

```mermaid
sequenceDiagram
    participant Caller
    participant Svc as upsertEntityMapping
    participant DB

    Caller->>Svc: link(entity_id, entity_type, provider_type)
    Svc->>DB: List (published only)
    alt a published row exists
        DB-->>Svc: that row
        Svc->>DB: update it in place
    else none (no row, or only archived rows)
        DB-->>Svc: empty
        Svc->>DB: create new row, status = published
    end
    Svc-->>Caller: mapping (genuinely published)
```

### 2.3 Why no `expired_at` column

Considered and rejected. Reusing an archived row would need a separate timestamp for when it had been
archived — but that timestamp would be overwritten on the next unlink, so it could not carry history
anyway. Creating a new row makes the column unnecessary: the archived row's `updated_at` is when it was
archived, the new row's `created_at` is when it was re-linked, and the full history is the ordered
sequence of rows.

The unique index already permits this. It is **partial** —
`Unique().Annotations(entsql.IndexWhere("((status)::text = 'published'::text)"))`
([ent/schema/entityintegrationmapping.go](../../ent/schema/entityintegrationmapping.go)) — so any
number of archived rows can coexist with one published row for the same
`(tenant, environment, entity_type, entity_id, provider_type)`. No schema change.

### 2.4 Test gap

`TestLinkIntegrationMapping_UpsertExistingMapping` only covered link → link on a still-published row.
Nothing exercised delink → re-link, which is why this shipped unnoticed. A regression test for that
path is part of this work.

---

## 3. Defect 2 — the cancellation flush

### 3.1 Trigger

`CancelSubscription` stays fast and is not restructured. After its existing `WithTx` commits — next to
the existing `publishCancellationEvents` call — it starts the flush with `ExecuteWorkflow`
(non-blocking), gated on:

```go
subscription.SubscriptionStatus == types.SubscriptionStatusCancelled &&
    subscription.CancelAt != nil &&
    s.hasMarketplaceMapping(ctx, subscription.ID)
```

The status check matters: `CancelAt` is set for all three cancellation types, but only `immediate`
sets `Cancelled` synchronously. `end_of_period` and `scheduled_date` leave the subscription active
until a later processor, so flushing then would report a final window for a subscription that has not
ended. `hasMarketplaceMapping` gates at the call site so ordinary cancellations don't start a workflow
that would immediately no-op; a lookup error is logged and treated as "no mapping", since this is a
post-commit side effect that must never fail an already-committed cancellation.

Starting workflows after the transaction matches the existing convention (`CreateSubscription`'s
HubSpot syncs, `CreateCustomer`'s onboarding). **This is not a cron** — one execution per cancellation.
The snapshot (6h) and report (3h) crons are unchanged.

```mermaid
sequenceDiagram
    participant Buyer
    participant MP as Marketplace
    participant Tenant
    participant FP as Flexprice API
    participant Flush

    Buyer->>MP: cancels subscription
    MP->>Tenant: notification (carries the cancellation instant)
    Tenant->>FP: POST /cancel (immediate, cancel_at = that instant)
    FP->>FP: commit — CancelAt = cancel_at
    FP-->>Tenant: 200
    FP->>Flush: ExecuteWorkflow (non-blocking, post-commit)
    Flush->>MP: report backlog, then the final record
    Flush->>Flush: delink mapping (only if everything succeeded)
```

### 3.2 Sequence

Workflow `MarketplaceSubscriptionFinalUsageFlushWorkflow`, activity
`MarketplaceSubscriptionFinalUsageFlushActivity`. The workflow is a thin wrapper — one
`ExecuteActivity` with 5 attempts (5s initial, 2× backoff, 1 min max, 5 min start-to-close). All logic
is in the activity.

```text
FlushActivity(subscriptionID, cancelAt, tenantID, environmentID):
    # cancelAt = sub.CancelAt, never sub.CancelledAt (§3.6)
    # tenantID/environmentID come from the workflow input and are set onto ctx first —
    # every repository call below is scoped by them.

1. subMappings = published subscription mappings for [aws, gcp, azure]
   if none: return                                  # not a marketplace subscription
   for m in subMappings:
       conn = resolve m's connection; prepareConnection(conn)
       on failure: log, set connectionResolutionFailed, continue   # never abort the others

2. # Phase 1 — the pre-existing backlog. Bounded above by cancelAt so the final record is excluded:
   # phase 2 owns it, and only phase 2 applies the reporting margin (§3.4).
   backlog = List(subscription_id, synced=false,
                  period_end >= now-24h, period_end < cancelAt, sort period_end asc)
   for rec in backlog:
       if not isEligibleForReport(rec): continue    # non-USD or negative (§8.2)
       reportRecordToMarketplaces(rec, marketplaces it holds an agreement on); MarkSynced(rec)

3. # Phase 2 — the one final record. Written first, then reported (§3.4).
   computedThrough = MAX(period_end) over published rows with period_end < cancelAt,
                     else earliest mapping created_at (§3.3)
   finalRec = finalUsageRecord(computedThrough, cancelAt)   # nil if cancelAt <= computedThrough
       # looks the window up first — on a retry the row is already there, so nothing is
       # recomputed; otherwise compute and Create it with period_end = cancelAt
   if finalRec and isEligibleForReport(finalRec):
       report it with period_end - 1s on the wire (§3.4); MarkSynced(finalRec)
       if not fully reported: finalUsageFlushFailed = true

4. if connectionResolutionFailed or finalUsageFlushFailed or any backlog record failed:
       return error                                 # Temporal retries; mapping stays published (§3.5)
   else:
       for m in subMappings: entityIntegrationMappingRepo.Delete(m)
```

Customer and plan mappings are **not** archived. Only the subscription's marketplace mappings are.

```mermaid
sequenceDiagram
    participant Flush
    participant DB as usage_records
    participant MP as Marketplaces

    Note over Flush,MP: Phase 1 — backlog (pre-existing rows only)
    Flush->>DB: unsynced rows, period_end within 24h
    loop each record × each relevant connection without an entry
        Flush->>MP: report
        alt accepted
            MP-->>Flush: reporting_id → syncs[marketplace]
        else Azure zero-amount
            Flush->>Flush: syncs[marketplace] = skipped
        else rejected / failed
            Flush->>Flush: no entry — stays unsynced
        end
    end
    Flush->>DB: MarkSynced (true iff every relevant connection has an entry)

    Note over Flush,MP: Phase 2 — the one final record
    Flush->>DB: last computed point = MAX(period_end) where period_end < cancelAt
    alt cancelAt > that point
        Flush->>Flush: compute usage for the remaining window
        Flush->>DB: create record (period_end = cancelAt)
        alt unique key collision
            DB-->>Flush: an earlier attempt already wrote it — report that row
        end
        Flush->>MP: report (wire timestamp = cancelAt - 1s)
        Flush->>DB: MarkSynced
    end

    alt everything succeeded
        Flush->>Flush: delink every mapping
    else any failure
        Flush->>Flush: mapping stays published; return error
    end
```

### 3.3 Window rules

**The window starts from all rows, not just unsynced ones.** Taking the max over unsynced rows alone
can double-bill: if record A `[T-10h, T-4h]` failed to sync but B `[T-4h, T+2h]` succeeded, the
unsynced max is `T-4h` and the flush window `[T-4h, cancelAt]` re-reports everything B covered. The
max over **all published rows** is `T+2h`. Since snapshot windows are contiguous, there are no holes
behind that point — every earlier span belongs to some row, and those rows are what Phase 1 reports.
Rows ending at or after `cancelAt` are excluded so the final record cannot move its own starting
point (§3.4).

**Fallback when no usage record exists.** A subscription can be linked and cancelled without the
snapshot cron ever running for it. The flush then uses the earliest subscription mapping's
`created_at` — the moment the subscription became reportable to that marketplace. Consequence: usage
from before it was linked is deliberately excluded.

**Boundaries.** `period_start` is inclusive, `period_end` exclusive, inherited from the ClickHouse
query builder (`timestamp >= ?` and `timestamp < ?`). Chaining `period_start = previous period_end`
counts a boundary event exactly once. The final record is skipped entirely when
`cancelAt <= windowStart`, so a backdated cancellation cannot produce an inverted window; the backlog
flush is then the whole job.

### 3.4 The final record: written first, reported second

The record is created before it is reported, like every other write in the system, and an unreported
record is deliberately left in the table for the reporting cron or a later attempt to pick up.

What makes that safe is that **every attempt computes the identical window**, so a retry collides with
the unique key on `(tenant, environment, subscription, period_start, period_end)` and reports the
existing row instead of writing a second one. Two bounds produce that:

- The last-computed point ignores rows ending at or after `cancelAt`, so the final record cannot move
  the start of its own window. Without this, a retry would measure from the row the previous attempt
  wrote and compute a narrower window — a **different `period_start`**, which the unique key would not
  catch. That is a real trap, not a theoretical one: the index constrains the window, so nothing
  protects against a window that changes between attempts.
- Phase 1 excludes `period_end >= cancelAt`, so the final record is not also reported by the backlog
  loop, which does not apply the reporting margin.

Keeping one row across attempts also gives the record one identity, which matters for GCP: it
de-duplicates on `OperationID`, which is the record id. A design that rebuilt the record per attempt
would hand GCP a new id each time and could double-count usage it had already accepted. AWS and Azure
de-duplicate on customer, dimension and timestamp, so they are unaffected either way.

The window stability this depends on is covered by `TestUsageRecordFlushRetryReusesTheSameRecord`: a
second attempt must compute the same window, find the same row, and be refused by the unique key. A
change that lets the window move between attempts breaks that test rather than silently duplicating
usage in production.

The record is **not** recomputed or rewritten when it already exists — the amount stays exactly what
was reported. §7 covers why revising it is not an option.

**The stored `period_end` is the true `cancelAt`; only the value sent to a provider carries the
margin.** One consequence worth knowing: if the flush fails and the reporting cron later retries the
record, the cron sends the stored `cancelAt` with no margin. That is correct for AWS, which makes no
such comparison, and for Azure, whose own documentation accepts usage ending exactly at the
cancellation instant. For GCP it would be rejected — but GCP's post-cancellation window is about an
hour and the cron runs every three, so by then the record is unacceptable regardless of its timestamp.
The margin is only ever decisive inside the flush's own attempts.

### 3.5 Failure handling and when the mapping is delinked

A report failure does not stop the run: every backlog record is still attempted and every connection
still resolved, exactly as the report cron behaves. Retries are idempotent for backlog records — a
marketplace that already holds a resolved `rec.Syncs[provider_type]` entry (real accept *or* skip) is
never re-attempted.

**`syncs` is keyed by marketplace, not by connection.** A tenant can delete a connection and create
another for the same marketplace; the connection id changes while the subscription's agreement does
not. Keyed by connection, an already-reported record would read as unreported the moment that happens
and be sent twice. The entry still records `connection_id`, for tracing which credentials were used,
but never as part of the key. An entry counts as resolved only when it carries both `synced_at` and
`agreement_id` — a half-written one is retried rather than mistaken for a completed report.

Each record's outcome is exactly one of three, decided by the caller from `rec.Synced` and whether any
mapped marketplace accepted it (the shared reporter itself only reports; it does not classify):

- **succeeded** — every relevant connection has an entry, and at least one is a real post
- **failed** — at least one relevant connection still has no entry
- **skipped** — every relevant connection has an entry, but all are skips (today only Azure's
  zero-amount case) — nothing was posted anywhere

**Delink runs only when everything succeeded**: no failed backlog record, the final record (if needed)
fully reported, and every mapped connection resolved. On any failure the mapping stays `published` and
the activity returns an error.

This reverses the original draft, which delinked unconditionally on the reasoning that a cancelled
subscription shouldn't keep a live-looking mapping. That trade was wrong: a published mapping is the
*only* thing that keeps a subscription's backlog visible to the 3h report cron
(`hasAgreementFor` resolves through published mappings only). Delinking on failure doesn't
just look stale — it permanently strands whatever failed to report, with no path back short of a human
re-publishing the mapping. Retries are cheap and idempotent, so there was no compensating benefit.

**No skip entries on the row itself.** An earlier draft resolved leftover rows with
`{skipped: true, skip_reason: "subscription_flushed"}`. Dropped: the 24h bound (§5) already stops
expired rows being re-scanned, and marking them synced would make a subscription whose connection was
broken for days look cleanly resolved when revenue was actually lost. Do not confuse this with the
per-run *skipped* outcome above, which is in-memory observability only and never persisted. Azure's
`zero_amount_not_supported` remains the only place `Skipped: true` is set.

### 3.6 The cancellation timestamp — `CancelAt`, not `CancelledAt`

GCP requires the reported timestamp to be strictly *before* its own recorded cancellation instant, and
Flexprice's `cancelled_at` can never satisfy that: it is set only after the marketplace already
cancelled, the marketplace notified the tenant, and the tenant called us. That's causal ordering, not
latency.

```mermaid
sequenceDiagram
    participant MP as Marketplace
    participant Tenant
    participant FP as Flexprice

    Note over MP: T0 — buyer cancels
    MP->>Tenant: notification (carries T0)
    Tenant->>FP: POST /cancel (cancel_at = T0)
    Note over FP: T2 — call processed.<br/>CancelledAt := T2 (now, always)<br/>CancelAt := T0 (the tenant's value)
    Note over FP,MP: T0 < T2 always — use CancelAt
```

The API already carries the right value. `CancelSubscriptionRequest` accepts `cancel_at` for a
backdated immediate cancellation, validated only to reject a future date and to require it fall after
the current period start. **The tenant contract for a marketplace-linked subscription is: call
`/cancel` with `cancellation_type: "immediate"` and `cancel_at` set to the marketplace's own
cancellation instant** (from the same notification that prompted the call).

Which field it lands in is easy to get wrong. In `updateSubscriptionForCancellation`:

```go
now := time.Now().UTC()
subscription.CancelledAt = &now                // ALWAYS wall-clock now
switch cancellationType {
case types.CancellationTypeImmediate:
    subscription.CancelAt = &effectiveDate     // ← the tenant's backdated value lands HERE
    subscription.EndDate = &effectiveDate
```

So the flush reads **`sub.CancelAt`**. Verified against a real row: a `scheduled_date` cancellation
showed `cancelled_at` at the API-call instant while `cancel_at`/`end_date` carried the requested
effective date hours later — the two genuinely diverge.

With the tenant supplying the marketplace's own instant, `cancelAt - 1 second` is a minimal
deterministic correction against the real comparison GCP makes, applied uniformly to all three
providers rather than branched: it satisfies GCP's strict `<`, and conflicts with neither Azure (whose
documented example accepts landing exactly at the instant) nor AWS (no such comparison).

---

## 4. What the marketplaces actually allow after cancellation

All three reject usage for an inactive subscription; each does it differently, and two have a
post-cancellation window far shorter than their general staleness limit.

| | General staleness window | Post-cancellation window | Rejection for an inactive subscription |
| --- | --- | --- | --- |
| **AWS** | 24h, plus a month-end cutoff at 06:00 UTC on the 1st | **~1 hour** from `License Deprovisioned` / `unsubscribe-pending` | `Status: CustomerNotSubscribed` |
| **GCP** | no hard cutoff published; guidance is **within 1 hour** of the usage occurring, with a ≤30-day outage provision (re-timestamped, not backdated) | **1 hour**, and the timestamp must be *before* the cancellation | `reportErrors` `NOT_FOUND` — *inferred, not doc-confirmed* |
| **Azure** | 24h from `effectiveStartTime` | none stated separately; the 24h rule governs | `ResourceNotActive` (batch) / 400 "SaaS subscription isn't in Subscribed status" (single) |

Sources: AWS —
[SaaS EventBridge integration](https://docs.aws.amazon.com/marketplace/latest/userguide/saas-eventbridge-integration.html#saas-eventbridge-final-usage)
("this event marks the start of a 1-hour final reporting window… After this window closes… usage
reporting is no longer accepted"),
[BatchMeterUsage](https://docs.aws.amazon.com/marketplace/latest/APIReference/API_marketplace-metering_BatchMeterUsage.html),
[UsageRecordResult](https://docs.aws.amazon.com/marketplace/latest/APIReference/API_marketplace-metering_UsageRecordResult.html).
GCP —
[Best practices for usage reporting](https://docs.cloud.google.com/marketplace/docs/partners/integrated-saas/best-practices-reporting#report_usage_after_an_entitlement_is_canceled)
("you can still report it with a timestamp that reflects the actual time… Report this usage within one
hour"). Azure —
[Metering service APIs](https://learn.microsoft.com/en-us/partner-center/marketplace-offers/marketplace-metering-service-apis),
[FAQ on already-unsubscribed subscriptions](https://learn.microsoft.com/en-us/partner-center/marketplace-offers/marketplace-metering-service-apis-faq#what-happens-when-you-emit-usage-for-a-saas-subscription-that-s-already-unsubscribed-)
("The only exception is reporting usage for the time that was before the SaaS subscription is
cancelled").

**Why the marketplace can't tell us what we already reported.** Only Azure supports it
(`GET /api/usageEvents?usageStartDate=…`). AWS's metering API has no seller-queryable read (its
guidance is CloudTrail, the tenant's own audit log); GCP's Service Control exposes only `check` and
`report`. So `usage_records` stays the source of truth for all three.

---

## 5. Schema and repository changes

**No migrations.** `entity_integration_mapping` keeps its partial unique index (§2.3);
`usage_records` is unchanged.

**`ListUnsynced` gains a 24h bound.** It previously returned every `synced = false` row forever, so
rows past the submission window were re-scanned by every run and never resolved. Now bounded by
`period_end >= now() - 24h` — Azure's exact limit, AWS's limit (whose month-end rule is a deadline, not
an extension), and far more generous than GCP's hourly guidance.

**`UsageRecordFilter` added.** No filter type existed; the repository exposed only `Create`,
`ExistsForPeriod`, `ListUnsynced` and `MarkSynced`. Rather than one bespoke method per query, a filter
following `EntityIntegrationMappingFilter`'s shape was added (embedded `*QueryFilter` /
`*TimeRangeFilter`, `Filters`, `Sort`, plus explicit columns), with a matching `List` on the interface
and Ent implementation. All three flush queries are the same call:

- last computed point — `{subscription_id, period_end < cancel_at, sort: period_end desc, limit: 1}`
- backlog — `{subscription_id, synced: false, period_end >= now-24h, period_end < cancel_at, sort: period_end asc}`
- the existing final record — `{subscription_id, period_start, period_end}`, the exact window, which
  the unique key makes single-valued

`ListUnsynced` was **not** folded in; the report cron needs it tenant/environment-scoped rather than
subscription-scoped.

**Tenant filter hardened.** `EntityIntegrationMappingQueryOptions.ApplyTenantFilter` skipped the tenant
predicate when the ctx tenant was empty, failing *open* (cross-tenant rows) instead of closed. Now
unconditional, matching `ConnectionQueryOptions`. No call site relied on the old behaviour — the only
deliberately cross-tenant query is `Connection.ListPublishedByProvider`, a separate method that
documents why.

---

## 6. Corrections to v3

| v3 section | Status |
| --- | --- |
| §8.1 Snapshot cron | Still correct for live subscriptions. Cancelled ones are now handled by the flush (§3). |
| §8.2 Reporting cron | Still correct. `ListUnsynced` gains the 24h bound (§5). |
| §10, "archive only after the snapshot cron's 4–10h lag" | **Superseded.** AWS and GCP allow ~1 hour (§4); a 4–10h wait misses both windows. |
| §10 lifecycle table, `Suspend` row | **Incorrect.** Azure permits usage only in `Subscribed` status, explicitly not `Suspended`. Should read: reporting is rejected while suspended; the mapping stays published because `Reinstate` is possible. |
| §2.5, "GCP submission window not documented" | **Resolved, and the framing was wrong.** GCP documents "within one hour" clearly; what it doesn't publish is a hard rejection cutoff like AWS's `TimestampOutOfBoundsException` or Azure's `Expired`. |
| §12, "no terminal state for expired rows" | **Partially addressed.** The 24h bound stops re-scanning; a true terminal status is still not built. |

---

## 7. Known gaps

| Gap | Status |
| --- | --- |
| No "give up" marker on a record that can never be sent | **Partly fixed.** The 24h bound stops the cron re-scanning hopeless rows, but they still sit `synced = false` with nothing marking them dead. A terminal status remains future work. |
| No dead-letter table | **Closed, not needed.** Failure logs carry every identifier (§8) and are queryable in SigNoz; a second copy would be a second thing to keep consistent. |
| GCP's timing rules differ in shape from AWS's and Azure's | **Answered.** GCP expects hourly reporting but publishes no hard cutoff. The 24h bound is exact for AWS/Azure and simply a uniform choice for GCP. |
| Which clock Azure measures lateness by | **Partly answered.** Azure rejects on *status* independently of the 24h rule, and usage from before cancellation stays reportable. Submission-time vs. timestamp is still unstated in any doc. |
| One record per API call instead of batching | **Enhancement.** Not a flush risk: the 24h bound caps a backlog at ~4 records across ≤3 marketplaces, so a flush makes at most about a dozen calls. |
| GCP could reject the final report as too late | **Resolved (§3.6)** by sourcing the timestamp from `sub.CancelAt` and reporting `cancelAt - 1s`. |

**New in v4, still open:**

**Nothing re-triggers the flush once its own retries are exhausted.** Temporal does not restart it and
`CancelSubscription` calls `ExecuteWorkflow` once. Recovery is then the 3h report cron's job, which is
fine for the backlog (it has a real 24h window) but not for the final record's timestamp — AWS/GCP's
~1 hour deadline doesn't care that the cron keeps trying. The 5 attempts are ~2.5 minutes of backoff,
well inside the hour, so exhausting them on a transient issue still leaves the cron a full window. If
the deadline itself was already blown, no retry by anything can fix it — that's the marketplaces' rule,
not a design gap.

**A connection broken for more than a day permanently loses that usage.** Inherent to the providers:
the flush catches a broken connection up only for the last 24 hours, and AWS/Azure reject anything
older. AWS is not double-reported — the window covers only genuinely new time, and the backlog step
skips connections that already have an entry — so the loss is one-sided. The mitigation both Azure and
GCP describe is rolling the expired quantity into a current-timestamped event, which weakens the
customer's audit trail and is a case-by-case call, not something to automate. The real defence is
alerting on the failure logs long before a cancellation exposes it.

**Events arriving after a window was reported cannot be added to it.** A record's amount is fixed once
it has been sent, so late usage for that window is lost. Revising the stored row does not recover it,
and the reason is worth stating because it looks like an easy fix:

- The reporter skips any marketplace already holding a `syncs` entry, so a revised amount is never sent
  to a provider that accepted the original. The row would then claim an amount no marketplace ever
  received, and nothing would flag the divergence — strictly worse than a stale figure, because
  `syncs` stops being a truthful receipt.
- Forcing a resend fails anyway. All three de-duplicate on (customer, dimension, timestamp), and the
  timestamp does not move when the amount does — so a revision is *a different amount at an existing
  key*. AWS answers `DuplicateRecord` (§8.4), GCP de-dupes on the unchanged `OperationID`, Azure
  returns 409. Metering APIs are append-only by design, which is exactly the property that makes our
  retries safe.

The only mechanism the marketplaces offer for late usage is an **additional** record on a new window,
which is what the snapshot cron already does for a live subscription. The final record has no such
successor: once the mapping is archived no further window exists, and AWS/GCP's ~1 hour deadline
closes before the next cron run regardless. Widening this would mean delaying the flush, which the
same deadline forbids.

---

## 8. Logging and what to search for

Levels are `error`, `info`, or `debug` only — never `warn`.

**Credentials are never logged, at any level, for any provider** — no `client_secret` or bearer token
(Azure), no `role_arn`, `external_id` or assumed-role credentials (AWS), no WIF JSON or federated token
(GCP). Provider errors pass through `utils.RedactSecrets`, which strips the tenant's identifiers and
preserves everything else verbatim, because the status and reason are what make a failure diagnosable.

**Every message begins with one of three prefixes**, so a single grep scopes to a flow and the rest of
the line says what happened. The `stage` tag is kept alongside for structured filtering, but the
message alone is enough to read a failure:

| Prefix | Emitted by |
| --- | --- |
| `marketplace usage snapshot:` | snapshot cron |
| `marketplace usage report:` | report cron **and** the flush — both call the same reporter, so when debugging a flush, grep this too |
| `marketplace subscription flush:` | flush only |

```
marketplace subscription flush: failed to list backlog usage records
marketplace subscription flush: final usage record created
marketplace usage report:       failed to assume the tenant's aws role
marketplace usage report:       gcp rejected the usage record, will retry next run
marketplace usage snapshot:     failed to check for an existing usage record
```

Every line tied to one connection carries `marketplace`; every line tied to one
record carries `usage_record_id`. Connection-level lines (auth, mapping load) have no
`usage_record_id`, because no record is in scope yet.

### 8.1 Snapshot activity — every 6h, never contacts a marketplace

Computes usage and writes `usage_records`. Every failure here is Flexprice-side. Window is anchored to
the run's *scheduled* time: `[scheduledTime − 10h, scheduledTime − 4h]`, so re-runs recompute the
identical window.

```
Starting MarketplaceUsageSnapshotWorkflow
[warn-ish info] scheduled start time unavailable; falling back to current time   ← windows stop being reproducible
Starting MarketplaceUsageSnapshotActivity   period_start=… period_end=…
Completed MarketplaceUsageSnapshotActivity  total=… succeeded=… failed=…
MarketplaceUsageSnapshotWorkflow completed  period_start=… period_end=… total=… succeeded=… failed=…
```

| `stage` | What broke | Blast radius |
| --- | --- | --- |
| `list_connections` | Listing published connections for a provider | That **provider** skipped; others continue |
| `list_customer_mappings` | Customer mappings for a connection | That **connection** skipped |
| `list_subscription_mappings` | Subscription mappings for a connection | That **connection** skipped |
| `get_subscription` | `GetWithLineItems` | That subscription counted `failed` |
| `check_existing` | The "already snapshotted" lookup | That subscription counted `failed` |
| `get_meter_usage` | Usage retrieval | That subscription counted `failed` |
| `calculate_charges` | `CalculateMeterUsageCharges` | That subscription counted `failed` |
| `create_usage_record` | The insert, for a reason other than already-exists | That subscription counted `failed` |

Reading the counts: `Total` counts **distinct subscriptions** (a subscription mapped to two
marketplaces is deduplicated, not counted twice). `succeeded` means a row now exists — whether written
this run, already present, or won by a concurrent insert; all three are correct. `failed` means that
window's usage was never captured, and **nothing retries it** — the next scheduled run computes a
different window.

Two things are invisible here, and both matter:

- The first three stages abort before reaching any subscription, so they increment nothing.
  `total=0 succeeded=0 failed=0` next to one of them means "never got far enough to look", not
  "nothing to do".
- A subscription whose customer has no published customer mapping, or a connection whose tenant has no
  mapped customers at all, is skipped with **no log and no count**. Legitimate, but silent.

There is **no per-subscription success log**. The only evidence is the row itself.

### 8.2 Report activity — every 3h, this is where marketplaces are called

Tenant-wide by construction: `ListPublishedByProvider` deliberately bypasses tenant scoping (the one
query that does) so a job with no tenant on ctx can discover work, then groups by (tenant, environment)
and sets both onto ctx before anything else.

Its own failures — every message begins `marketplace usage report: failed to …`:

| `stage` | What broke | Blast radius |
| --- | --- | --- |
| `list_connections` | Listing published connections for a provider | That provider skipped |
| `list_unsynced` | Reading the tenant's unsynced records | That **tenant/environment group** skipped entirely |
| `mark_synced` | Persisting sync state **after** the marketplace already accepted | See below |

Auth and the report call itself are in §8.4 — they are shared with the flush.

> `mark_synced` is the one genuinely dangerous stage: the provider has the usage, our table doesn't.
> The next run re-reports. AWS de-duplicates on customer+dimension+timestamp and GCP on `OperationID`
> (unchanged for a persisted row), so both are safe; Azure returns 409, surfacing as
> `stage=usage_event`. Always investigate.

**Pre-filter, before any connection is chosen** (provider-agnostic, so no `marketplace` tag). Both are
excluded from *every* count:

| Level | Message | Meaning |
| --- | --- | --- |
| `info` | `…skipping usage record, currency is not usd` | None of the three marketplaces accept non-USD. A record in any other currency never syncs and will fall out of the submission window unreported, so this is logged at info rather than debug: it needs to be visible at default settings. |
| `error` | `…skipping usage record, amount is negative` | A credit, not usage. An upstream billing bug; never sent. |

Reading the counts: `Total` counts records that reached at least one relevant connection. `succeeded` =
every relevant connection has an entry and at least one is a real post (`synced` now true, done
forever). `failed` = at least one connection still missing; the next run retries **only** the missing
ones. `skipped` = every entry is a skip (Azure zero-amount only).

Two caveats worth knowing:

- `skipped` is in the workflow result but in **neither completion log** — both log only
  `total`/`succeeded`/`failed`. Read it from the Temporal UI.
- A record is `failed` if *any* relevant connection is missing, however many succeeded. With GCP broken
  and AWS/Azure fine, you will see `…usage record synced` lines for AWS and Azure right next to the
  record being counted `failed`. Expected, not contradictory.

### 8.3 Flush activity — once per cancellation

Trigger-side, all emitted post-commit so none can affect the cancellation:

| Level | Message | Meaning |
| --- | --- | --- |
| `info` | **`marketplace subscription flush workflow started successfully`** | **The definitive confirmation.** Carries `workflow_id` |
| `error` | `failed to look up marketplace mappings for subscription flush` | The gate query failed; treated as "no mapping" so the cancellation is never blocked. Nothing downstream ran |
| `info` | `temporal service not available for marketplace subscription flush` | Temporal isn't wired into this process |
| `error` | `failed to start marketplace subscription flush workflow` | `ExecuteWorkflow` failed — usually Temporal unreachable |

Seeing **none** of the four is itself a finding: the subscription had no published marketplace mapping.

Activity failures — every message begins `marketplace subscription flush: failed to …`:

| `stage` | Message ends | Phase | Blast radius |
| --- | --- | --- | --- |
| `list_subscription_mappings` | list subscription marketplace mappings | setup | **Hard abort**, activity retries |
| `get_connection` | resolve marketplace connection | setup | **Does not abort** — other mappings still processed, but this blocks delink |
| `list_backlog` | list backlog usage records | 1 | **Hard abort** |
| `mark_synced` | record usage sync state | 1 and 2 | That record forced to `failed`; same warning as §8.2 |
| `relevant_connections` | usage record has no connection to report to | 1 and 2 | Nothing is sent for that record and the run fails, so the mappings stay published — archiving them would put the record beyond the cron, which also needs the mapping to judge relevance |
| `last_computed_period_end` | determine last computed period | 2 | **Hard abort** |
| `get_existing_usage_record` | load the existing final usage record | 2 | **Hard abort** — runs before any computation, so nothing was computed |
| `get_subscription` / `get_meter_usage` / `calculate_charges` | load subscription / compute meter usage / calculate charges | 2 | **Hard abort** |
| `create_usage_record` | create final usage record | 2 | **Hard abort** — includes losing an insert race to a concurrent attempt; the retry finds that row |
| `delink_mapping` | archive marketplace mapping | 3 | Only reachable once everything else succeeded — a pure DB failure at the last step |

Success path:

```
Starting MarketplaceSubscriptionFinalUsageFlushWorkflow   subscription_id=…
[if not a marketplace sub] marketplace subscription flush: subscription has no marketplace mapping…  ← ends here
marketplace subscription flush: usage record synced          (phase 1, per record per connection)
marketplace subscription flush: final usage record created   (phase 2, before it is reported)
   — or —
marketplace subscription flush: final usage record already exists, reporting it   (a retry)
marketplace subscription flush: usage record synced          (the final record's connections)
MarketplaceSubscriptionFinalUsageFlushActivity completed     final_record_id=… records_succeeded=… records_skipped=… mappings_delinked=…
MarketplaceSubscriptionFinalUsageFlushWorkflow completed     final_record_id=… records_succeeded=… records_skipped=… mappings_delinked=…
```

**The result is only ever visible on a successful run.** When the activity returns an error Temporal
discards its result value — the task is recorded as failed, carrying the failure and not the payload —
so the workflow's `.Get` leaves the struct zero and its completion log never runs. The error itself
carries only its message: a plain Go error is converted to an `ApplicationError` with `err.Error()`,
so `WithReportableDetails` does not survive into Temporal either.

On a failed run the record ids therefore exist **only in the logs above**, which is what the per-record
lines are for. Do not expect the Temporal UI to show which record failed — grep `usage_record_id`.

Neither completion log reports a failure count, and that is deliberate rather than an omission: the
activity fails the run whenever a record failed, so any count printed on the success path could only
ever be zero.

**Confirming the final record.** It is written *before* it is reported (§3.4), so it exists in
`usage_records` whether or not any marketplace accepted it — an unreported one sits there `synced =
false` with a partial `syncs` map. Either `final usage record created` or `final usage record already
exists, reporting it` appears on every run that needed one, and `final_record_id` on the completion
log carries its id in both cases. If neither line appears, one of these is true and the other logs say
which: no final record was needed (`cancel_at` at or before the last computed point), an earlier stage
aborted, or the record was ineligible (§8.2's two pre-filter lines, which the flush applies too).

**Confirming the delink.** No per-mapping success log — only `mappings_delinked` and the absence of
`stage=delink_mapping`. Per §3.5 it must **only** happen on a fully successful run: a mapping archived
on a run that also logged a failure is a bug worth reporting. Still `published` after a failed flush is
correct and deliberate.

**Timing.** 5 attempts, ~2.5 minutes of total backoff, 5-minute start-to-close per attempt —
comfortably inside the one-hour window. What isn't bounded is queue delay: compare `start_time` on the
tracking line against the line's own timestamp. Minutes is harmless; tens of minutes means worker
saturation.

### 8.4 Connection auth and the report call — shared by §8.2 and §8.3

Both paths use the same reporter, so these lines are identical from either caller and all carry
the `marketplace usage report:` prefix — **including during a flush**.

**Auth has no success log.** If it works you see nothing; success is proven only by execution reaching
the report call. A failure skips the **whole connection** for that run (and in the flush, blocks
delink).

| Provider | `stage` values, in order |
| --- | --- |
| **AWS** | `read_connection` (missing secret data *or* missing `sync_config` region) → `decrypt_role_arn` → `decrypt_external_id` → `load_mappings` → `assume_role` |
| **GCP** | `read_connection` → `decrypt_credentials_json` → `load_mappings` → `wif_session` |
| **Azure** | `read_connection` → `decrypt_tenant_id` → `decrypt_client_id` → `decrypt_client_secret` → `load_mappings` → `get_token` (most commonly an **expired client secret** — the failure that appears after months of working) |

One check covers all three — empty output means every configured credential is valid:

```bash
grep 'marketplace usage report:' log | grep -E 'stage=(assume_role|wif_session|get_token)'
```

**The report call.** First distinguish our data problem from their rejection: `stage=resolve_record`
means the entity mapping is missing a required field and **nothing was sent** (AWS: license_arn /
customer account / plan dimension; GCP: usage_reporting_id / service_name / metric_name; Azure:
resource_id / plan_id / dimension).

| Provider | Log | Meaning |
| --- | --- | --- |
| AWS | `stage=convert_quantity` | Amount doesn't fit AWS's int32 quantity |
| AWS | `stage=batch_meter_usage` | Transport or malformed request — no verdict reached |
| AWS | `…aws did not process the usage record, will retry next run` (info) | No result row returned |
| AWS | `…aws rejected the usage record, customer not subscribed, will retry next run` | `CustomerNotSubscribed` — self-heals if the buyer re-subscribes. **Expected against a test customer** |
| AWS | `…aws rejected the usage record, it conflicts with a different record already on file` | `DuplicateRecord` — AWS holds a *different* record for the same key. Retrying can't fix it |
| AWS | `…aws returned an unrecognized status, will retry next run` | `aws_status` carries the raw value |
| GCP | `stage=services_report` | Transport failure **or** an API-level rejection such as 403 — read the error text, not just the stage |
| GCP | `…gcp rejected the usage record, will retry next run` | HTTP 200 but `reportErrors` set. `error_code=5` NOT_FOUND (inactive consumer), `7` PERMISSION_DENIED (missing IAM grant), `3` INVALID_ARGUMENT |
| Azure | `…skipping usage record for azure, it does not accept a zero quantity` (info) | **Not a failure.** Nothing sent; the connection resolves as a *skip*, permanently |
| Azure | `stage=usage_event` | Every other Azure outcome — rejection, status error and transport failure are indistinguishable at the stage level. Read the error text for the HTTP status |
| All | `…usage record synced` | **Accepted.** `reporting_id` is AWS's `MeteringRecordId`, GCP's `operationId`, or Azure's `usageEventId` |

**The "already reported" silence.** A marketplace that already holds a resolved entry in `rec.Syncs`
is skipped with no log and no API call — correct, but it means a provider can vanish from a run's logs
because an earlier run already satisfied it. Absence of AWS lines does **not** mean AWS failed.

### 8.5 Answering "was this record reported?"

Logs alone can't always tell you, because of the two silences above. Read the row:

```sql
SELECT synced, syncs FROM usage_records WHERE id = '<usage_record_id>';
```

The map is keyed by provider type (`aws_marketplace`, `gcp_marketplace`, `azure_marketplace`). A key
present with a real `reporting_id` = accepted (possibly on an earlier run, possibly through a
connection since replaced — `connection_id` on the entry says which). Present with `"skipped": true` =
resolved via skip. **Absent** = never accepted — and check whether the subscription holds an agreement
there at all: the mapping must exist *and* carry a non-empty provider entity id, or that marketplace is
excluded silently.

For a flush specifically, the final record's `period_end` must equal `cancel_at` **exactly**, not
`cancel_at - 1s` — the margin exists only on the wire (§3.4).

---

## 9. Reference

Everything in v3 §13 still applies. Added here:

**AWS** — [SaaS EventBridge integration](https://docs.aws.amazon.com/marketplace/latest/userguide/saas-eventbridge-integration.html#saas-eventbridge-final-usage) (1-hour final window) ·
[SNS notifications](https://docs.aws.amazon.com/marketplace/latest/userguide/saas-notification.html) (`unsubscribe-pending`) ·
[UsageRecordResult](https://docs.aws.amazon.com/marketplace/latest/APIReference/API_marketplace-metering_UsageRecordResult.html) (`CustomerNotSubscribed`)

**GCP** — [Best practices for usage reporting](https://docs.cloud.google.com/marketplace/docs/partners/integrated-saas/best-practices-reporting#report_usage_after_an_entitlement_is_canceled) (1-hour post-cancellation rule)

**Azure** — [Metering FAQ: already-unsubscribed](https://learn.microsoft.com/en-us/partner-center/marketplace-offers/marketplace-metering-service-apis-faq#what-happens-when-you-emit-usage-for-a-saas-subscription-that-s-already-unsubscribed-) ·
[SaaS subscription life cycle](https://learn.microsoft.com/en-us/partner-center/marketplace-offers/pc-saas-fulfillment-life-cycle) ·
[Implementing a webhook](https://learn.microsoft.com/en-us/partner-center/marketplace-offers/pc-saas-fulfillment-webhook) (`Unsubscribe` is notify-only)
