# FLE-1513 — Billing Terms Implementation Plan

- **Date:** 2026-10-27 · Simplified 2026-10-28
- **Status:** Implementation completion and review; deployment pending.
- **Normative design:** [ERD](./2026-10-07-FLE-1513-commitments-revamp-erd.md) §5 and §7.
- **Cases:** [Scenarios](./2026-10-07-FLE-1513-commitments-scenarios.md).
- **Tenant contract:** [Billing Terms API Contract](./2026-10-28-FLE-1513-billing-terms-api-contract.md) is the tracked, shareable contract, including cost explorer and create/edit examples. The older local 24 October draft is superseded.

**Implementation status, 29 October 2026:** the existing six-PR stack now contains the reusable core, owner APIs and inline creation, subscription and separate delivery, lifecycle guards, protected termination settlement, explicit historical correction, and Cost Explorer commitment summaries. Final regression checks and review are in progress; nothing here implies deployment.

The completion stays within the existing stack. Review persistence/core first, then usage projection, invoice sources/void, and finally delivery/API/lifecycle adoption. Keep public creation disabled in intermediate layers; enable it only in the final adoption layer.

Product decisions confirmed for this release:

- Pause/trial defer unclaimed items in both delivery modes. Resume retains original dates and amounts; no proration or regeneration.
- The trusted trial-end invoice can collect due subscription items while temporarily incomplete; ordinary incomplete subscriptions and separate delivery wait for activation.
- Cost Explorer returns top-level commitment summaries, a labelled usage window and lifetime collection totals. Invoice/schedule details use paginated endpoints. Selected-window financial projections are outside this release.
- Collection amounts are principal, not cash received or net revenue. Recognition follows performance obligations.

Verification uses table-driven service tests, real PostgreSQL transactional/concurrency tests, and both Temporal test-suite and actual local-server history replay. The release checklist below remains the acceptance target; final run results belong in the PR descriptions.

## 1. Scope and simplifications

Keep two reusable entities: persisted billing rules and their concrete schedule items. Stable IDs, published/archived status, source links, timestamps and existing activity history provide auditability. Do not introduce a persisted rule-set version model, owner counters, source-version markers, ordering columns, per-rule allocation snapshots or supersession pointers.

Rules collect a finite known obligation. They do not determine pricing, credit grants or performance obligations. Recognition remains a separate system. Commitments are the first production owner; a standalone fixed-fee test adapter proves reuse without building another product.

| Concern | Implementation decision |
|---|---|
| Current plan | Owner's published rules/items, not a version pointer |
| Editing | Archive the complete old set and create fresh IDs for the desired upcoming plan |
| Concurrency | Owner locking plus a computed preview state token; no stored counter/hash |
| Retry | Identical currently installed plan is a no-op; stale old plans conflict rather than restore history |
| Dates | Recurring `start_date` / `end_date`; once `bill_on` / trigger; no independent cutoff |
| Service periods | Snapshot owner term for once, each step for recurring; no rule coverage overrides |
| Splitting | Even by count, or proportional to elapsed step duration; no owner-pricing weights |
| Creation | Persist every item eagerly, atomically with terms/owner, including undated milestones |
| Delivery | Invoice only when due; shared eligibility for subscription and separate modes |
| Void | Archive/cancel permanently; released amount becomes a visible allocation gap requiring replan |
| Existing delivery storage | Keep `instalment_invoicing` and existing Go mappings; map proposed `invoice_delivery` input without a column rename |
| Price reuse | Concrete items remain separate; enable trusted schedule-source invoice lines without fabricating Prices |
| Full-contract correction | Draft before invoice correction; activate against actual released principal, with explicit historical reissue |
| Commitment coverage | Allocation/utilization remain contractual; covered usage bills $0 and period true-up is disabled for `billing_terms` |
| Paused/trialing subscription | Defer unclaimed schedule charges in both delivery modes until resume/trial end; preserve dates/amounts and the obligation |

## 2. Existing code and integration risks

| Verified location | Consequence |
|---|---|
| `internal/api/dto/commitment.go` | Nested rules and invoice_delivery are accepted in the final adoption layer; intermediate layers retain the rejection. |
| `internal/ee/service/commitment.go` | Reuse `prepare` economics, existing create transaction, overlap lock and owner idempotency. Initialize rules/items before commit. |
| `internal/ee/service/commitment_periods.go` | Still computes contractual commitment value and settlement accrual. Do not import it into generic collection splitting or recognition. |
| `internal/ee/service/billing_commitments.go` | Within principal must become zero and true-up suppressed for scheduled collection; usage allocation/analytics remain intact. |
| `internal/ee/service/commitment_engine.go` | `commitmentLines` currently renders both utilized principal and binding-floor true-up. Separate allocation valuation from collection projection; do not make the commitment non-binding to suppress charges. |
| `internal/ee/service/meter_usage.go` | `commitmentLineBreakdowns` uses `Binding` for `TrueUpEnabled`; `amortizeLineTotals` spreads rendered charges into analytics. Both must use the same collection policy as invoices without losing consumed value. |
| `internal/api/dto/subscription.go`, `internal/types/commitment.go`, `internal/api/dto/events.go` | Existing usage DTOs conflate used value with billed principal. Preserve existing charge-field meanings and add explicit utilization/collection views; do not place the whole contract payment on every meter row. |
| `internal/ee/service/invoice.go` | Compute prepares outside a write lock; void locks invoice first; draft recomputation reconciles/deletes lines. Add source-aware atomic claims and preservation. |
| `ent/schema/invoice_line_item.go`, `internal/api/dto/invoice.go` | Stored Price ID is nullable, but subscription request validation requires it. Support trusted validated schedule-source lines and audit tax/discount/export paths; preserve ordinary public validation. |
| `reconcileLineItems`, same file | Legacy fallback can delete/recreate source lines. Protect linked schedule lines and reconcile by item ID. |
| `RecalculateInvoice` / `RecalculateInvoiceV2`, same file | Preserve linked lines in-place; preflight destructive correction before voiding. |
| `internal/postgres/client.go` | Nested `WithTx` reuses the transaction and forces writer reads. Preserve transaction context through all generic operations. |
| `internal/repository/ent/commitment.go` | Current default filters published rows. History and assigned reads must explicitly include archives. |
| `internal/ee/service/factory.go`, `cmd/server/main.go` | Avoid core ↔ parent-service/FX cycles; wire narrow repositories/adapters/core before enriching ServiceParams. |
| `ent/schema/commitment.go`, `migrations/versioned/postgres/20261022120000_create_commitments.sql` | Old delivery column already exists in code/migration, though dormant. Live DB application is not inferred from the checkout. |
| `internal/temporal/activities/subscription/update_billing_period_activities.go` | `CreateDraftInvoicesActivity` creates empty period drafts; actual computation runs later. Keep it returning durable invoice IDs, including schedule-only drafts. |
| `internal/temporal/workflows/subscription/process_subscription_billing_workflow.go` | After period advance it dispatches invoice processing. Integrate schedule collection in that existing path, not a second independent charge activity. |
| `internal/temporal/workflows/invoice/process_invoice_workflow.go`, `internal/temporal/activities/invoice/invoice_activities.go` | `ComputeInvoiceActivity` is the existing entry point for atomic line/claim persistence; finalization and payment follow it. |
| `internal/ee/service/subscription.go` | Inline commitments exist before opening invoice creation. Paused periods are skipped; `ResumeSubscription` creates a resume invoice with `PeriodStart = resumeAt`. Test schedule pickup at both opening and resume. |
| `internal/ee/service/wallet.go` `pendingCharges` / realtime balance; `invoice.go` `GetUnpaidInvoicesToBePaid` | Pending usage and invoice totals are combined with wallet price-type eligibility and draft cutoffs. Keep covered usage zero; reconcile due unclaimed sources and invoice-bound sources once, respecting usage-only versus fixed/all wallets. |

## 3. Reusable architecture

```text
owner API → owner service → generic billingterms.Service
                                 ↓
                    planner + rule/item repositories
                                 ↓
                     immutable owner-adapter resolver

invoice orchestration → generic eligibility/binding helpers
                     → existing invoice persistence and processing
```

The shared subpackage must not import its parent `internal/ee/service`, ServiceParams, API DTOs or commitment/subscription models. Invoice orchestration calls the core, not the reverse.

| Package | Responsibility |
|---|---|
| `internal/domain/billingterms` | Models/builders, owner reference/snapshot, repository/adapter/service interfaces; no Ent/HTTP/Temporal/Postgres imports |
| `internal/repository/ent` | Rule/item persistence, scoped current/history/assigned reads, source mapping and conditional bindings |
| `internal/ee/service/billingterms` | Normalize/validate, calendar steps, split/rounding, eager expansion, archive-and-replace, allocation summary, eligibility and void cancellation |
| `internal/ee/service/commitment_terms_owner.go` | Commitment economics/context, actual recipient, lifecycle/settlement policy and invoice attribution |
| `internal/ee/service/billing_terms_delivery.go` | Existing-invoice and one-off orchestration, DTO mapping and processing handoff |
| `internal/api/dto/billing_terms.go`, `internal/api/v1/billing_terms.go` | Reusable wire input and thin commitment-scoped handlers |
| `internal/temporal/{models,activities,workflows}/billingterms` | Scoped due/fallback work and deterministic invoice-processing workflows |

Implementation structs are private with constructors/getters and nil handling. Domain models and wire types use the established exceptions; builders copy an existing entity and perform updates. Ent conversion lives at the repository boundary. Register all new dependencies in main through FX; no mutable global registry or cross-request tenant state.

### Owner adapter

The adapter loads a validated snapshot and supplies attribution/lifecycle policy:

- typed owner reference and trusted tenant/environment;
- original total, authorized collection target, currency and finite owner start/end;
- actual invoicing customer, optional subscription and permitted delivery destination;
- calendar/timezone/default cadence and owner-specific alignment constraints;
- generic permission to edit/collect or perform settlement correction;
- labels/source entity attribution.

It has **no shared version-state write method**. Existing owner economics are not made editable. Only owner-specific settlement decisions need owner persistence, outside generic rule/item schema.

For once rules, a standalone owner needs no subscription/calendar. For recurring rules it supplies an explicit independent calendar. V1 commitments retain their subscription alignment constraints. Proportional elapsed duration is calculated in the generic planner; do not require period amounts or Price callbacks.

The commitment adapter resolves `sub.GetInvoicingCustomerID()` rather than blindly using the subject customer. Public routes set owner type server-side. Unknown/duplicate adapter registration fails explicitly; no public arbitrary-owner switch.

FX construction order: repositories/transaction dependencies → narrow owner adapter → immutable resolver → core → enriched `ServiceParams.BillingTerms`. Do not build the adapter from already-enriched params that depend on the core. A missing core for a persisted terms owner is an error, not zero debt.

### Commitment allocation, valuation and collection

Use one shared allocation/valuation result with two consumers: usage reporting and invoice charge projection. Keep the allocation engine's waterfall, amount/quantity capacity, committed-rate valuation, tiers, active windows, clipping and overage selection unchanged. Do not derive used/unused value from the invoice amount after it has been zeroed. Do not set `Binding = false`, zero the committed Price, or wait for payment to decide coverage.

The effective period true-up policy is `binding && effective_collection_method == per_period`. Resolve a binding legacy NULL collection method to the existing `per_period` default. A persisted `billing_terms` owner uses scheduled collection even when its next payment is later, an invoice is unpaid/voided, a gap defers new pickup, or adoption/delivery is disabled. Missing required schedule data is an actionable integrity error; never fall back to a second floor charge.

| Projection | `per_period`, binding | `billing_terms`, binding |
|---|---|---|
| Capacity, used/unused quantity and economic value | Existing calculation | Same calculation |
| Utilized usage line amount | Existing utilized principal | $0, with covering commitment attribution |
| Period true-up | Existing binding-floor remainder | No line; amount $0 and enabled false |
| On-demand usage | Existing overage logic | Same overage logic |
| Contract collection | Utilized plus true-up | Due schedule items through their selected delivery |

Implement this in `commitment_engine.go` / `billing_commitments.go`, then reuse the projection in `meter_usage.go` (`commitmentLineBreakdowns`, `wholeLineTotals`, `amortizeLineTotals`) and invoice preparation. A line covered by several commitments may mix collection methods: suppress only each scheduled commitment's utilized/true-up contribution; retain other contributions. A line-level `TrueUpEnabled` is the OR of effective per-period true-up policies, not an OR of `Binding`. Keep distinct commitment IDs in the explanatory breakdown. The existing `computed_*_amount` charge fields and `TotalCost` remain charge-based; new economic values must not be added back into them during combo/point amortization.

### Usage and collection response contract

Extend the existing `GetUsageBySubscriptionResponse.Commitments[]` / `CommitmentUsageBreakdown`; expose the same owner summaries once at the usage-analytics response level where that API needs the explanation. Reuse the owner-scoped terms view for invoice/history drill-down. Do not append schedule payments to meter usage cost or repeat a payment on every meter, property combo or time point.

- Keep `committed`, `used`, `unused` quantity/amount as capacity and economic consumption. Update comments that currently call unused value a true-up still owed. For quantity commitments, the utilization ratio uses units; for amount commitments it uses consumed money. A zero denominator gives an unavailable ratio, not infinity or a fake 0%.
- Add owner-level `collection_method`, `true_up_enabled`, `true_up_amount` and a collection summary. Existing commitment `billed_amount` remains the calculated commitment contribution to usage billing for the queried window, not actual invoice history or schedule collection; it is $0 under scheduled collection. Overage remains outside that commitment amount.
- The collection summary has its own `as_of` and obligation currency: target, assigned principal (including drafts), finalized non-void invoiced principal, published scheduled principal, due principal (date-due subset of scheduled), unallocated principal, and next bill date. Explain any derived deferral reason such as pause/trial/gap; date-due does not mean eligible for collection. Use the generic summary plus a scoped batch join to invoice state. The usage window and owner-lifetime collection summary must be labeled separately; a $300 annual upfront payment is not January's consumed value.
- Draft principal is reserved/assigned, not "charged". UI can say "$300 invoiced through billing terms" only for non-void finalized invoices, with links and their payment states. Credit notes/refunds are separate corrections; do not label gross principal as net paid cash or infer a per-commitment paid amount from a mixed invoice without payment allocation.
- Invoice-line `commitment_info` can carry covered economic value separately from the zero charge fields. Keep `computed_commitment_utilized_amount + computed_overage_amount + computed_true_up_amount` consistent with the usage charge before discounts. Group/point values sum back to their line; do not multiply the collection summary across groups.
- Batch owner/invoice summary reads under tenant/environment scope, not one PostgreSQL call per analytic point. Preserve existing filtered-query restrictions: if a source/property filter cannot support whole-scope allocation, mark the commitment attribution unavailable/partial rather than inventing full capacity or a true-up. A meter-filtered supported query must still load the full shared pool before allocating.

Example: $100 monthly capacity, $80 consumed, $20 unused, $1,200 total and $300 already invoiced via an upfront term. Show 80% utilization, $80 covered economic value, $0 covered usage charge, $0 true-up, and "$300 invoiced through billing terms; $900 scheduled". This stays true for arrear terms and unpaid invoices; use "covered by commitment; collected through billing terms", not "prepaid". With $130 of usage, $100 is covered and $30 is overage; if a $75 schedule item is also due, that invoice is $105 before tax/discounts, not $205 or $125.

## 4. Schema and migrations

### Generic tables

Use the trimmed ERD §5.2/5.3 exactly. Add no numbered version machinery. Rule `start_date` / `end_date` are optional recurring range inputs; dated-once uses `bill_on`. Omit coverage overrides. Resolve non-null item service-period snapshots at generation.

Retain owner/source IDs, base/environment mixins, exact principal, delivery, trigger/date, execution state, invoice references and simple cancellation reason. Current status defines eligibility; historical source lookup explicitly includes archived rows.

Database protections:

1. Every query/update/aggregate filters tenant plus environment, including history and replay/no-op checks.
2. Preserve scoped owner/source integrity with appropriate FK/unique constraints. Polymorphic owner existence/authorization comes from the adapter.
3. Planned charges require a rule ID; owner-authorized settlements alone may omit it. No deletion cascade may erase financial provenance.
4. Use a unique non-null invoice-line `billing_schedule_item_id` so an item is invoiced once across all archival states. IDs are globally unique; source lookups and mutations still require tenant/environment scope. Reissue creates a fresh item. Preserve linked line IDs on recompute.
5. Require positive amounts and valid intervals/dates/states. No persisted position is needed; rule order has no financial priority. Preserve duplicate rules when comparing normalized multisets.
6. Index owner/status, scoped due pickup, rule source and invoice ID. Bound all reads and keyset-page worker/history lists.

### Existing delivery column

Keep `commitments.instalment_invoicing`, the Ent field, existing Go mappings and the historical migration. Map the proposed `invoice_delivery` request value through the commitment adapter to this existing field. A friendlier public input does not require a storage/domain rename; no rename migration or compatibility rollout is in scope.

There are no proposed generic version columns to migrate/backfill. Existing per-period owners have no terms/items and keep existing billing behavior. Adding the generic module does not add owner revision/state counters. Settlement target persistence is owner business state: use existing protected metadata for the commitment decision, not a hidden schedule version model. Preserve reserved settlement metadata during ordinary metadata updates and reject attempts to change it.

Follow schema → `make generate-ent` → required `make generate-migration`, and use the checkout's `make migrate-generate name=billing_terms` flow for deployable dbmate artifacts under `migrations/versioned/postgres`. Review generated/drift/order/checksum checks on clean and current scratch DBs. Use local/scratch configuration, not an unknown `.env` against production.

The minimal financial cost-explorer projection additionally needs scoped ClickHouse schema/migrations under `migrations/clickhouse`, its domain/repository ports and idempotent invoice/credit event-consumer integration. Store source identities and source-state timestamps, query by tenant/environment/date with the required 90GB memory bound, and support void/credit repair and historical backfill. Do not use revenue-recognition facts as an interchangeable source for invoicing dates or add an unbounded PostgreSQL aggregate to bypass this work. This analytics projection is not a new transactional billing entity or payment ledger.

## 5. Pure planner and eager generation

1. Load/transiently construct owner context. Normalize original total and fixed/percent money at currency precision; original total remains percent basis even on edits.
2. Create one item per once rule or build every recurring step in explicit/default range. Use timezone-aware calendar boundaries. Edit requires explicit recurring start and dated-once bill date; no hidden global cutoff/default replay.
3. Compute the unassigned target from live assigned principal, including archived items and drafts/unpaid invoices. Resolve fixed occurrences and the optional remainder against that budget.
4. Even divides remainder by count. Proportional divides by actual elapsed duration of each step; it is unrelated to pricing, usage or recognized revenue. Fixed-per-charge is not automatically prorated.
5. Round items and apply the remainder residual to the final item. Reject zero/negative outputs, bad sums and the 20-rule/500-relevant-item limits.
6. Persist all rules and all generated items immediately. A manual milestone row exists with NULL date; trigger/fallback dates that row later. Do not defer amount generation to invoice pickup.
7. Once service period snapshots the owner term; recurring snapshots each step. No recognition entries or monthly recognition rows are generated.

Acceptance example: $900 across 15-day and 30-day steps is $450/$450 even, or $300/$600 proportional. If the $900 is a once charge, it is one item; recognition handles any straight-line performance obligation independently.

Do not expand rules into recurring Prices or create a Price per item. Prices supply pricing definitions; each schedule item supplies one exact principal, due date and unique claim. Recurrence on a Price cannot replace occurrence-level trigger/void/archive/claim state, and proportional splits or rounding can vary an occurrence amount. Use amount/quantity/period snapshots in the invoice line and the validated schedule source as provenance. Audit provider exports, tax/discount resolution, reconciliation and rendering for nullable Price handling before adoption; do not relax unrelated caller-supplied subscription line validation.

## 6. Mutation, concurrency and retries

### Lock protocol

Existing invoice rows → collection lifecycle guards → typed owner locks → item rows/conditional bindings. Sort each set deterministically with tenant/environment in every key. For commitments, a shared subscription eligibility guard serializes claims with pause/resume/trial-end/activation/cancellation; locking only an owner does not serialize with today's subscription-row lock. Generic adapters/orchestration supply any shared eligibility guard without importing subscription models into the core. Owner-only edits never acquire existing invoice locks in reverse order. New invoice/line rows may be inserted under guards/owner locks before becoming visible; an idempotency race discovering an existing invoice must resolve it through existing-invoice order.

Lifecycle services currently take a subscription row lock and can compute/move invoices in the same transaction. Treat that existing parent-row lock as an outer prefix for lifecycle operations only; invoice/core paths must not acquire it after invoice/guard/owner locks. Audit/refactor nested pause/resume/trial/cancel calls to discover/lock existing invoices before acquiring the eligibility guard and owner locks; never hold a guard/owner lock and then discover an existing invoice that needs locking. Acquire all grouped-child guards in sorted order before claims. Add real-database races for these paths, not just owner-edit versus invoice. There is no new persisted lock/version state.

Commitment creation also takes the lifecycle guard before its existing `commitments:<subscription_id>` membership lock when both are needed. Lifecycle membership reads/writes follow the same order, so adding an owner cannot bypass a concurrent pause or invert the legacy creation lock. Pre-discover the complete invoice/guard/owner set for grouped and lifecycle operations; do not acquire another existing invoice after taking a membership/owner lock.

Lifecycle writers use the same protocol:

| Existing path | Required change before collection |
|---|---|
| Pause/resume and cancellation | Discover and lock existing cycle invoices before guards and commitment locks; abort/retry if a new existing-invoice dependency appears later. |
| Paid/zero-invoice activation callbacks | Trial and ordinary payment activation run after commit with fresh state; existing bounded sweeps recover missed callbacks. |
| Generic subscription updates, period rollover and payment-driven status writes | Updates, period rollover and payment status transitions use fresh guarded state with retryable NOWAIT row acquisition; stale cached status cannot overwrite a pause or cancellation. |
| Bulk overdue status updates | Use bounded candidate discovery and guarded conditional updates instead of bypassing the protocol with bulk SQL. |
| Group membership and inherited-child cascades | Discover parent, old/new destination and child participants together; sort locks and revalidate membership. Grouped adapters include the destination parent; inherited membership writers guard the complete affected set. |
| Commitment creation and owner mutation | Acquire guards before existing membership locks; retain legacy lock keys during rolling deployment and share typed owner locks for token-affecting writes. |

The guard primitive currently accepts at most 500 input scopes per acquisition. Validate grouped operation limits before adoption. A caller must supply the complete set, not acquire another batch after owner locks. Merely adding a guard inside a repository update cannot repair an existing lock-order inversion.

Use caller transaction context and primary/writer state. Owner initialization inside inline subscription create cannot commit independently. Invoice line, item binding and all changes are atomic. Remote preparation occurs before the short write section; finalization/payment/provider/notification work starts after commit through existing pipelines.

### Computed preview precondition

`state_token` is a deterministic hash computed from scoped owner economics/lifecycle and relevant current/historical claim state. Include published rule/item IDs/definitions, execution states, dates and invoice bindings, plus target and delivery constraints. Exclude wall-clock now, unrelated log timestamps, payments that do not affect principal, and recognition data. Never persist the token/hash/counter on rules or the owner.

GET/preview returns it. PUT sends `expected_state`; under the owner lock recompute it and conflict with 409 if materially changed. Thus an invoice claim, void, trigger, owner lifecycle change or another edit invalidates an old preview without a database version field.

### Save and retry

- A normalized desired rule multiset already installed with its original generated principal and no allocation gap is a no-op. Preserve operational trigger dates and invoice assignments; identical PUT does not re-arm a milestone or re-expand a partly invoiced series.
- Otherwise require matching expected state; compute current budget; archive all published old rules/items; retain historical assigned claims; cancel obsolete pending charges; insert fresh rules/all upcoming items and verify complete allocation atomically.
- On failure roll back the whole change. After commit, use existing activity events to show old/new IDs and reason; source rows remain the financial audit basis even if a notification fails.
- Exact retry while the desired plan is still installed is a no-op, including after new items have been invoiced. After another distinct edit, the old request conflicts; it must not restore a historical plan. No promise of returning numbered historical responses requires an operation log.
- A void/gap invalidates the no-op shortcut: explicit refreshed review must allocate the released amount before collection resumes.

If the owner recipient or delivery context changes, retain existing invoice bindings with their original attribution. Pending items with stale recipient/subscription/delivery remain visible but derive `requires_replan=true`, even with zero unallocated principal. An explicit reviewed replacement regenerates only unassigned charges in the new context. It never moves or duplicates retained invoices; no new stored transition flag is needed.

Trigger is also replay-safe without a stored request fingerprint: an already-triggered item with omitted/matching requested date returns its first date/current state, not a new now. A different date or canceled item conflicts; archived sources are never made runnable.

## 7. Invoice delivery and correction

### Shared eligibility

Only published scheduled items with published source rules, due bill dates and complete allocation are picked up. Current means published, not a numeric pointer. Both subscription and separate-invoice paths call the same helper and conditional binding. Historical assigned principal includes archives; current eligibility excludes them.

Prepare/preview may list candidates outside the write transaction. Re-read them under the locks before persisting charge lines/bindings. Schedule item ID is the reconciliation identity, not label/coverage/subscription-line-item identity.

### Subscription invoices

Retain each existing invoice's exact linked charge lines on draft recompute/in-place recalculation, including archived sources. Add only new eligible claims. Do not delete/recreate source lines through the legacy reconciliation fallback or merge identical-looking items. Use the right invoicing customer and preserve owner attribution in grouped invoices.

The concrete automatic path is:

```text
ScheduleSubscriptionBillingWorkflow → ScheduleBillingActivity
  → ProcessSubscriptionBillingWorkflow → CreateDraftInvoicesActivity
  → subscriptionService.CreateDraftInvoiceForSubscription (empty draft)
  → TriggerInvoiceWorkflowActivity → ProcessInvoiceWorkflow
  → ComputeInvoiceActivity → invoiceService.ComputeInvoice
  → PrepareSubscriptionInvoiceRequest + schedule candidate projection
  → invoice lock → lifecycle guards → sorted owner locks → revalidate
  → line + claim transaction
  → existing finalize / integration events / payment pipeline
```

Change `billing.go` preparation to append schedule candidates **once per destination invoice**, after cadence fan-out and grouped-child aggregation, independently of whether usage/fixed charge lists are empty. `CalculateAllCharges` alone is not sufficient: it can run several times for cadence windows and would duplicate the schedule projection. Preserve the preview path as read-only. Under the compute transaction, reload source eligibility, preserve existing claims and persist each new source line plus binding together. If an edit/claim changed a prepared candidate, recompute the affected projection or return a retryable conflict before commit; do not commit stale totals/tax calculations. Do not call remote tax/providers while holding the locks.

Use an explicit server-controlled collection boundary: opening invoice = subscription start, cycle = invoice period end, resume = effective resume time (the current resume invoice already uses it as period start), trial-end = effective trial-end billing instant. Real claims also require `bill_on <= collection_as_of` so computing an open-period draft cannot collect future items merely because its period end is later. Include equality at the boundary; overdue unclaimed items carry forward. A delayed run creating February's historical cycle invoice must not claim March payments just because wall-clock now is March. Manual historical recompute preserves existing principal; adding historical replacements requires the explicit selected-item correction context. Threshold/maintenance/provisional compute paths must carry an intentional collection context and cannot silently consume a future period's items.

Use the owner's subscription/recipient to select destinations, including parent/grouped invoices; keep child subscription and owner attribution on the line. Parent preparation must propagate destination invoice/exclusion and collection context to child projections and deduplicate by item ID. Inherited/grouped children must not independently claim the same item. A scheduled principal with zero metered usage must still produce a positive invoice; `IncludeZeroValueLineItems=false` may hide covered $0 rows but cannot remove the schedule charge. A trial preview that zeroes amounts must never claim a positive item; actual due payments wait for an eligible real billing run rather than becoming $0 assigned principal.

Keep `CreateDraftInvoicesActivity` as the durable draft-ID producer. It must not skip a period merely because ordinary charges are empty. Its retry must return the same existing draft for processing; an already-finalized period remains a no-op, without injecting new charges into that issued invoice. Verify period advancement and later `ComputeInvoiceActivity` use the invoice's original billing context, not the subscription's newly advanced current period. Existing opening and resume service flows must call the same schedule-aware compute logic. A due worker does not create an extra ordinary subscription-cycle invoice.

Legacy void-and-recreate recalculation and manual principal edits need a source-aware preflight before destructive work. Route their collection correction to the explicit terms flow; ordinary usage/tax/coupon recompute can retain linked principal as applicable. Trial zeroing, threshold skips and FX cannot mark principal assigned while omitting/zeroing its source line.

Support retrospective reissue after explicit void and replan. It selects reviewed fresh item IDs for the replacement invoice; do not infer a mapping from old source IDs or consume every overdue item. Current `/invoices/{id}/recalculate` accepts no selection body and supports subscription invoices only: extend that workflow/contract for source-aware selection before enabling this use case, and provide equivalent one-off correction through the shared claims. Preserve unrelated invoice charges and preview periods/delivery. A selected item claimed by another worker resolves to its existing invoice or conflicts for renewed review; never create another line. Existing in-place recalculation still preserves assigned principal.

### Separate invoices and recovery

Do not pre-create future invoices. The single scoped due workflow processes bounded item IDs through its create/recover activity and hands off to existing invoice processing. Recover an already-created live invoice ID before attempting a new claim; an activity crash after atomic invoice/claim commit must not strand or duplicate that invoice, even if its source was archived meanwhile. Recovering a live invoice is not permission to create a new claim from an archived item.

Otherwise re-read eligibility under lifecycle guards/owner locks; stale archived candidates stop. Prepare the ordinary one-off recipient, payment/net-terms and applicable coupon/tax configuration through the existing invoice orchestration, then persist a consistent positive draft/line/binding atomically. Do not commit an assigned item with an empty zero-total draft and assume a later `ComputeInvoice(nil)` can reconstruct the one-off request: that path does not synthesize it. Persist/recover the required invoice inputs or materialized associations using existing invoice facilities, so the durable processing/retry sees the same principal/configuration without a client request. Provider/finalization failure retains assignment and retries the same invoice. A voided invoice/canceled item never triggers recovery into another charge.

### Void, allocation gaps and lifecycle

Cancel/archive a matched source item in the normal invoice void transaction; preserve amount/dates/invoice/rule links. Never reschedule it. Duplicate callbacks are no-ops.

```
assigned + published_scheduled + unallocated = collection_target
```

Normal target is original total. A void may produce a positive gap; derive `requires_replan` and defer this owner's schedule pickup in both delivery modes. Other charges continue. An explicit complete replacement clears the gap; no separate resume state/API is needed.

Example: $600 assigned + $600 upcoming, then a $75 void, becomes $525 + $600 + $75 gap. Review schedules the remaining $675 under fresh rules/items. Reissue is an explicit plan shortcut; it does not revive old IDs.

Commitments own contractual settlement accrual, default termination/cancellation fees and durable settlement decisions. Persist a protected business target in existing owner metadata; prevent ordinary metadata edits from changing/removing it. Terminated collection corrections use that target, not the original-total formula. Additional termination policy inputs are separate contract extensions. A terminal closure may archive without replacement when no money remains; it is not a public empty-rule amendment.

Naturally ended/settlement collection corrections use dated once rules and show one-off fallback if no subscription invoice remains. Original service economics and performance obligations are not changed by collection editing.

### Pause, trial, resume and service lifecycle

Confirmed product policy: a subscription pause defers **unclaimed** commitment schedule charges in both delivery modes. Keep items published/scheduled with original bill dates, amounts and service periods; no cancellation, waiver, proration, term extension or regeneration follows from pausing payment delivery. The existing paused-period invoice remains $0/SKIPPED and claims nothing. Subscription delivery catches up on the first eligible resume/cycle invoice; separate delivery catches up after resume through the scoped due runner. Pause/resume and claims must share the lifecycle guard and re-check owner eligibility; the serialized winner determines whether an item remains pending or belongs to an existing invoice. A closing pre-pause proration invoice can collect only through its effective pause boundary, not dates during the later pause. Existing assigned invoices keep their normal explicit invoice/payment lifecycle.

Confirmed trial policy: defer payments due during trial until trial end in both delivery modes. A trial-start/preview invoice never claims positive principal at $0, and the separate worker cannot bypass the trial. Catch up on the real trial-end invoice or next scoped separate tick once active, preserving original amounts/dates/target. Confirmed trial-end exception: due subscription-delivery payments join the first real paid-period invoice even while trial conversion temporarily marks the subscription incomplete awaiting payment. Only trusted server-side trial-end orchestration may authorize this exception, after revalidating the transition, effective trial end, recipient and eligibility under the shared guards. It must not bypass pause/cancellation, claim future items, or apply to trial previews, ordinary incomplete subscriptions or the separate worker. Separate delivery still waits for activation. Draft/incomplete subscriptions otherwise cannot collect before activation. Trial-end retries recover the same invoice and source bindings; failed payment does not free assigned principal. Creation of rules/items may still be atomic with a pending inline subscription, and triggering a milestone may resolve its date while collection is deferred. Editing permission and collection permission are separate owner-adapter capabilities.

Pause is not "no future subscription boundary" and never authorizes an automatic switch to separate collection. Do not silently recompute original commitment economics or already-generated item periods on resume when `CurrentPeriodStart` changes. Any pause-induced capacity/settlement behavior remains the owner contract's policy, separate from payment deferral; preserve the current usage/allocation rules and test the resume boundary against the original owner total.

## 8. API and UX

Use the ERD's create/edit shapes. Create accepts nested rules plus `invoice_delivery`, without state token. Edit accepts complete rules and `expected_state`; no ordering/cutoff/version fields. History is a paginated rule/item/archive stream, not a revisions API. Input rule indexes locate validation errors but are not stored.

Wire views expose rules, upcoming items, retained invoice items, derived amount/gap summary and computed current state. Do not synthesize earned/deferred revenue in this module; integrate performance obligations separately. Existing 400/404/409 envelopes apply.

Amendment preview alone accepts optional `replace_invoice_ids`. Resolve them within scope and derive only this owner's hypothetical released principal; return conditional amount, blocking invoices and `can_apply: false`. Keep actual state-token inputs unchanged. PUT rejects the preview-only field and reloads actual claims; neither a client-supplied budget nor the hypothetical preview can authorize collection. No reservations or server draft writes occur.

UX entry points:

- Always show the full target: fixed retained invoice entries plus editable proposed charges. For $1,200 with $300 assigned, show both the $300 history and the $900 editable balance.
- Change upcoming billing: keep existing invoices; replace the unclaimed arrangement, without restarting at owner start.
- Correct setup: draft/review a full-contract replacement first. A read-only preview may hypothetically release selected invoice principal, but marks the proposal conditional and blocked from activation. Correct invoices explicitly, refresh state/budget, preview again and save only against actual live claims. No persisted draft/version table is needed.
- Full reset: once all live schedule principal is voided, replacement rules may allocate the full target from owner start, with explicit past bill dates. Reissue selected fresh historical charges through the source-aware invoice flow. Partial correction retains every other invoice's principal.
- No invoice exists: archive first setup and eagerly regenerate all items.
- Review: old set to archive, retained invoices, new charge dates/amounts, service-period display, delivery and due-now effects.
- Conflict: retain the draft, refresh and require review. Gap: display the unallocated amount and review action. History links preserve exact IDs even though old rows are archived.

Do not activate full-total replacement charges while retained invoices still consume that total. That would require explicit allocations from old invoice principal into new items, potentially partial/many-to-many, which is deferred. Correction failures leave the proposal inactive; void-induced gaps defer pickup until a valid plan is saved. Payments, refunds and credit notes alone do not release assigned principal. Mixed invoices need their normal explicit correction/preservation flow.

Generate Swagger/SDK/MCP only once handlers/DTOs are implemented. Custom SDK work belongs in `api/custom`, never generated files. Retain the existing delivery storage name.

## 9. Scoped scheduling and wiring

Provision due/fallback scheduling per known tenant/environment at feature/environment activation, using explicit scope and deterministic IDs. No unscoped cross-tenant billing-table scan. Register workflow/activity types plus actual scoped schedules, with readiness checks, bounded keyset batches, retries and overlap policy. Missing scope fails closed; context is restored in every activity.

Ensure due delivery is provisioned before accepting separate-invoice configuration. The generic core has no Temporal dependency. Manual fallback scans use owner deadline/context and published items; no future item amounts are generated by that worker.

Adoption flags gate new use, not interpretation of existing persisted collection methods. Disabling adoption/delivery never reverts a scheduled owner to per-period usage/true-up. Reads/source resolution/void bookkeeping remain correct. A delivery kill switch defers only new claims; live invoices remain explicit invoice concerns.

### New Temporal entry points and registrations

Introduce **one new workflow type and two new activity types**, all on the cron queue. Add scoped serializable models under `internal/temporal/models/billingterms`, the orchestrator under `workflows/billingterms`, and activities under `activities/billingterms`. Business helpers for fallback, due selection and recovery are service functions with their own tests; they do not each need an activity. No database or service calls from workflow functions.

| New workflow/activity | Queue and responsibility |
|---|---|
| `ProcessBillingScheduleItemsDueWorkflow` | Cron queue; one tenant/environment run at fixed `as_of`, prepare batches, create/recover each invoice and durably hand off to existing invoice processing |
| `PrepareBillingScheduleBatchActivity` | Cron queue; resolve a bounded page of untriggered term-end fallbacks under owner locks, select due separate-delivery candidates and archived-inclusive interrupted live bindings, return IDs/work kind/cursor; no invoice creation |
| `CreateOrRecoverBillingScheduleInvoiceActivity` | Cron queue; one candidate per call; restore trusted context, recover a live binding first or re-check current separate-delivery eligibility under guards/owner locks, then atomically persist ONE_OFF draft/line/claim |

All new activity calls belong to that single scheduled workflow. There is no new per-item workflow type and no new activity added to the subscription-billing workflow. The new workflow calls the creation activity once per bounded item, then starts the **existing** `ProcessInvoiceWorkflow` on the invoice queue with the existing stable invoice-processing ID policy. Wait for durable child start, use abandon-on-parent-close for already-started processing, and treat already-running/completed processing idempotently. Do not wait for payment completion before advancing the batch. The new parent workflow exists before its invoice-creation activity can commit, so retry can recover the binding after a lost response.

The separate path is:

```text
scoped 2-minute schedule → ProcessBillingScheduleItemsDueWorkflow
  → PrepareBillingScheduleBatchActivity (fallback + due/recovery IDs)
  → for each bounded candidate: CreateOrRecoverBillingScheduleInvoiceActivity
  → existing ProcessInvoiceWorkflow on invoice queue (durable start)
  → next page or continue as new
```

`PrepareBillingScheduleBatchActivity` must include interrupted assigned bindings, not only `scheduled` items. If invoice creation committed but the parent exhausted retries before handoff, the next scoped run recovers the **same live invoice**, including archived sources. A voided invoice never resumes into collection. Missing/obsolete/paused/trialing new candidates defer/skip safely, whereas transient database/provider failures retry and integrity failures surface. Keep per-item failure handling from starving later candidates. Use explicit timeouts/retries, deterministic workflow inputs/`workflow.Now`, bounded history and replay tests.

**Subscription delivery is a different path through existing workflows, not through these new activities.** `ScheduleSubscriptionBillingWorkflow`, `ScheduleBillingActivity`, `ProcessSubscriptionBillingWorkflow`, `CreateDraftInvoicesActivity`, `TriggerInvoiceWorkflowActivity` and `ProcessInvoiceWorkflow` retain their orchestration. Service changes in `PrepareSubscriptionInvoiceRequest` and atomic `ComputeInvoice` append/claim subscription items when those existing activities execute. Opening/trial-end/resume use the same services. `CreateDraftInvoicesActivity` already creates empty drafts; add regression coverage rather than introduce a separate schedule-claim step. No new step is required in `ScheduleSubscriptionBillingWorkflow` and it must not dispatch separate items. Keep paused-period/no-transition handling intact.

Wire new constructors via `fx.Provide()` in main, and register only the new workflow plus two activities on the cron queue in `internal/temporal/registration.go`. Reuse existing invoice-queue registrations. Add the scoped schedule/config name and actual tenant/environment schedule provisioning at activation through `internal/temporal/service/schedules.go`; readiness verifies both queues are running. Registering Go types alone is insufficient. Do not copy the global subscription scheduler's unscoped discovery into new billing-table queries. Existing draft-finalization/payment retry schedules retain their jobs. No workflow version marker is needed solely for service changes; if orchestration changes during implementation, require replay tests and `workflow.GetVersion()` as applicable.

Starting defaults: 2-minute ticks, page 100, at most 500 work items per run and 20 concurrent item operations per scope, overlap skipped; continue as new between bounded batches when necessary. These are operational settings, not contract restrictions or an exact-second SLA. Preserve pagination/fairness across due/recovery phases, including duplicate existing invoice processing and failures. Never drop item 501 or send full histories in workflow payloads. Track oldest due age, pending count, deferral, claims and recovery outcomes. Optional post-commit wakeups improve latency but are not required for correctness.

### Cost Explorer

The public `POST /events/analytics` handler calls `GetCostExplorer`; internal invoice/export callers retain their existing analytics entry points. The response adds independently paginated top-level `commitments`, with explicit `usage_window`, `collection_scope=lifetime`, utilization and the authoritative lifetime collection summary. It does not change usage totals. See the tenant contract §10 for the exact wire format.

Discover owners independently of usage rows, including no-event/no-invoice owners. Scope and bound discovery, validate filter-bound cursors, and preserve whole-pool allocation for feature/meter filtering. Source/property-filtered usage can be unavailable without hiding collection state. PostgreSQL supplies bounded transactional owner/source state; usage remains in ClickHouse. No new financial projection, credit aggregation, cash allocation or analytics workflow is required.

## 10. Ordered implementation slices

| Slice | Deliverables | Gate |
|---|---|---|
| 1. Schema/domain | Trimmed models/ports, retained delivery storage, source constraints, generated Ent and deployable migration | Clean/current PostgreSQL migration checks; no adoption enabled |
| 2. Pure planning | Normalization, presets, calendar ranges, time splitting/rounding, eager generation, service snapshots | All create/partial/date/manual/rounding examples agree |
| 3. Core persistence | Scoped current/history/assigned queries, computed state, no-op retries, archive-all edit, summary/gap, trigger | Service tests and real-DB rollback/isolation/concurrency; no counters |
| 4. Invoice integration | Trusted no-Price schedule lines, unique source binding, reconciliation preservation, lock order, atomic void cancellation, FX/recalc guards | No duplicate/orphan principal or lost source IDs; downstream Price-dependent paths verified |
| 5. Commitment adoption and usage | Nested/inline/preview, owner adapter, shared valuation/collection projection, `meter_usage.go`/DTO summaries, covered-at-zero/no-true-up, actual recipient | Amount/quantity/window/mixed-stack utilization is preserved; charge fields and true-up agree across invoice/usage/spend; existing per-period regressions pass |
| 6. Delivery/recovery | One new scoped workflow/two activities, unchanged subscription orchestration using schedule-aware services, just-in-time one-off, durable same-invoice recovery, provisioning/registration and lifecycle gates | Both delivery paths actually persist/process invoices; schedule-only, cutoff, replay, recovery, pause/trial/resume and backlog tests pass |
| 7. Lifecycle/correction | Conditional full-contract preview, explicit invoice correction, selected-item historical reissue, default termination target in protected owner metadata, cancellation fee, gap/replan, ended one-off fallback | No activation against hypothetical released principal, archived revival or target loss; recognition unaffected |
| 8. Cost explorer and API rollout | Top-level commitment summaries, lifetime collection and paginated source drill-down, thin owner routes, tenant contract, Swagger/SDK and rollout | Usage-window/lifetime separation, filter/pagination/isolation tests, lint and actual-worker scoped rehearsal |

Enable create acceptance only after all slices: otherwise usage and scheduled collection could be incomplete. Future owner products, a payment ledger and full performance-obligation implementation remain separate projects.

## 11. Validation and release

Use table-driven unit tests for planner/service logic, real PostgreSQL for repositories/constraints/locks (never mocked Ent), and Temporal activity/workflow tests. Validate:

- Eager once/recurring/manual generation and limits; no invoice precreation.
- Even vs elapsed-duration splits, partial steps, unequal months, timezone/DST, zero amounts and remainder residual.
- Published-only pickup versus archived-inclusive assignment/history; source/owner/tenant/environment integrity.
- Computed token changes for claims/voids/triggers/edits but not wall-clock/recognition changes; no-op retry cannot re-arm or restore old rules.
- Atomic archive/insert/bind rollback; two edits and multi-owner invoice/void/amend races.
- Draft recompute/recalc preserves source lines; identical labels stay distinct; explicit principal correction keeps the original obligation currency; reject unsupported scheduled-source FX/custom-currency conversion before mutation.
- Wrong first setup before/after invoices, gap/defer/replan, mixed invoices, ended and terminated collection corrections.
- Full-contract authoring before void is non-executable; partial correction retains other claims; full release permits explicit owner-start replacement; reissue claims only reviewed fresh IDs and races safely with workers.
- Subscription schedule lines work without synthetic Prices; nullable Price paths handle tax/discount/export/rendering and public caller validation remains intact.
- Activity crash after commit, archived-source recovery, finalization delay without resetting LastComputedAt, completed child-ID reuse, provider failure retry, and paid/voided exclusion. Replay actual due/invoice histories.
- Standalone fixed-fee adapter with no subscription/Price, independent recurring calendar and unchanged shared state machine.
- Collection-only edits do not change performance obligations or recognized revenue. Once owner-term snapshots are informational only. The new upstream journal rejects schedule-source invoice/credit posting until commitment-to-POB allocation exists; tests assert no partial journal writes. Normal invoice/payment processing continues.
- Delivery request maps to existing `instalment_invoicing` without a storage/domain rename; ordinary owner metadata updates cannot alter a frozen business settlement target.
- Scoped provisioning/readiness, missing scope, disabled adoption and delivery pause with existing owners.

### Practical validation and operating limits

One normalization/validation implementation serves create, inline create, create preview, amendment preview and save. Handlers only parse/delegate. Read-only previews do not reserve principal, trigger milestones or launch workflows. Save revalidates on the writer under locks; raw DTO validation cannot substitute for owner/date/budget checks.

| Limit/policy | Validation and boundary tests |
|---|---|
| 1–20 rules per submitted plan | 1 and 20 accepted, 0 and 21 rejected; bound before expansion; archived history does not consume this limit |
| At most 500 relevant items per owner | Count retained live-invoice items plus proposed items; stop generation at 501 before allocating an unbounded slice. Test exactly 500/501, including assigned archived sources. Canceled history remains pageable and does not block future valid edits |
| Finite term, no invented term-duration cap | Validate ordered finite dates and checked calendar arithmetic; item count bounds short cadences. Future standalone daily owner: a one-year plan can fit; a multi-year plan over 500 days is rejected explicitly |
| Monetary safety | Use decimals, owner currency precision and existing currency rules, then check `numeric(20,8)` range and positivity. Test currencies with 0/2/3 decimals, max representable rounded amount, overflow, negative, sub-minor-unit values and residual. Reject an expansion producing any zero item; do not silently omit it |
| Remainder/fixed/percent | At most one remainder; percent uses original total, never remaining budget. Fixed recurring value is per occurrence. Normalize before exact budget comparison; a remainder must be positive and sufficient for every rounded item |
| Dates/frequency | Validate IANA timezone, field combinations and positive interval/count with overflow-safe calendar operations. Commitment recurring boundaries/cadence follow the existing subscription alignment rules; generic owners supply their own calendar. Test leap day, DST, month end, partial last step, equality at term end for arrear bill dates and explicit past correction dates |
| Ownership and delivery | Only binding commitments can use terms. `per_period` rejects terms/delivery. Validate subject scope, actual recipient, currency, subscription membership, permitted parent/grouped destination, lifecycle permission and worker readiness; missing/foreign scope fails closed |
| Owner budget versus invoice limits | Owner's 500-item bound is not a 500-item lifetime-history limit or a global invoice-line limit. Batch eligible reads and aggregate across multiple owners without dropping due charges. Verify a destination with more than 500 aggregate schedule lines is processed exactly once or is rejected in preflight by a documented real downstream provider limit, never silently truncated |
| Operational paging | Worker page 100 / dispatch cap 500 and paginated API history are separate from business limits. Test page/cursor stability for equal bill dates, batch 100/101, dispatch 500/501 and backlog continuation |
| Data integrity | Fixed positive charge, valid state/source/binding combinations, non-null resolved periods, source/owner/tenant/environment agreement and unique claims. Schema constraints complement service checks; no source deletion cascade |

Every C1–C21/T1–T17 rule gets a valid and invalid case, with limits shared across preview/save. Do not invent restrictions such as "only one billing-term owner per subscription" or a maximum number of historical amendments to simplify implementation. Preserve existing supported commitment scope/price/window validation, and add regression cases for later price attachment, phase/addon changes and inline overridden prices.

### Required confidence matrix

These are focused scenario families, not an instruction to run every possible cross-product. Use table-driven unit cases for arithmetic/projections, real PostgreSQL integration for bindings/transactions/isolation, Temporal test-suite/replay tests for orchestration, and a small end-to-end rehearsal against the actual worker. Existing testutil in-memory stores are useful for service logic but do not establish database constraints or lock correctness.

| ID | Case | Required evidence / expected result |
|---|---|---|
| P1 | Same binding amount commitment, $100 capacity and $80 used | `per_period`: $80 utilized + $20 true-up; `billing_terms`: $0 utilized + $0 true-up, still $80 used/$20 unused/80% utilization |
| P2 | Quantity commitment: 1,000 units at $0.10, 800 used | 800 used/200 unused, economic values $80/$20; zero covered charge and no true-up under terms; unit utilization 80% |
| P3 | Usage $130 against $100 amount capacity, $75 item due | $100 covered, $30 on-demand, $0 true-up; invoice $105 before adjustments; usage cost $30 and collection $75 stay separate |
| P4 | Zero usage or no events, with/without a due item | Capacity/unused remain available in owner summary; no true-up or fabricated events; a due $75 item still creates/processes a $75 invoice |
| P5 | Shared scope/meter/group and mixed `per_period` / `billing_terms` stack | Same waterfall and quantities; suppress only scheduled owners' principal; mixed line boolean/amounts reflect actual per-period true-up contributions |
| P6 | Windows, timezone, partial commitment starts/ends, tiered committed Price and overage tiers | Economic valuation and allocation match existing engine; collection projection alone changes; no per-line tier reset or clipped-floor regression |
| P7 | No usage under legacy NULL collection, non-binding or v2-disabled baseline | Legacy defaults/per-period true-up and existing non-binding behavior unchanged; terms on non-binding rejected |
| P8 | Invoice versus `GetUsageBySubscription` versus detailed meter analytics | Same scope/allocation/value/overage; scheduled owners' charge/true-up fields zero; used/unused never inferred from zero line amount |
| P9 | Group-by property/source, points, meter filter and unsupported filtered scope | Supported combo/point charges sum to line; full shared pool loaded when necessary; unsupported allocation labeled unavailable/partial; no duplicate owner payment per combo |
| P10 | Billing-term unpaid, paid, draft, archived-source live invoice and void | Coverage unchanged; draft is assigned, finalized non-void is invoiced, void releases assignment; paid status is not a coverage switch or editable-budget switch |
| P11 | Spend alerts/current bill | Covered consumption contributes $0; eligible due unclaimed schedule items and existing invoice charges counted once, assigned drafts not counted again; future/deferred items excluded from collectible pending charges, overage retained |
| P12 | Collection-summary query over two usage windows/multiple meters | Owner-lifetime summary returned once per owner with `as_of`; no double-counting $300 upfront or misleading attribution to a queried month |
| P13 | Usage-only versus fixed/all wallets and unpaid/current drafts | Honor existing wallet eligibility; do not add ineligible fixed schedule principal to usage-only balance, debit covered $0 usage, or double-count a source moving from unclaimed to draft/finalized; current-period draft exclusions remain reconciled |
| D1 | Inline terms plus opening invoice, including paid draft activation | Owner/rules/items atomic before compute; upfront due item charged once with real invoice ID; transaction failure leaves no partial creation |
| D2 | Standalone commitment added mid-period | Subscription delivery waits for its next eligible invoice; separate delivery picks due item on tick; preview labels timing; no new global subscription invoice |
| D3 | Subscription cycle through actual activities/workflows | Empty draft ID reaches `ProcessInvoiceWorkflow`; compute binds due item and finalizes/attempts payment; period advance does not lose historical cutoff |
| D4 | Schedule-only / $0 covered usage / hidden zero-value lines | Positive scheduled principal prevents inappropriate SKIPPED invoice; hiding informational usage cannot hide the payment |
| D5 | Opening/cycle/advance/arrear dates exactly at boundary | Start and end equality honored; one claim across current/next cadence fan-out; next future item not selected early |
| D6 | Open-period maintenance compute or delayed multi-period catch-up | Never collect beyond min(actual as-of, destination boundary); backfill allocates due items to correct invoice, not first historical invoice using wall-clock now |
| D7 | Parent/grouped/inherited destinations and billing customer differs | One destination claim, actual invoicing customer, correct child/owner line attribution; no child duplicate or missing parent charge |
| D8 | Trial start/preview/future start, both delivery modes | No positive item assigned at $0 or before due; due trial payments wait for actual trial end, then catch up once with original total; ordinary draft/incomplete wait for activation; trusted real trial-end subscription invoice includes due principal while conversion is incomplete; preview has zero writes |
| D8a | Trial-end conversion incomplete, retry and payment failure | Terms-only first paid-period invoice retains positive principal; retry reuses the same invoice/source IDs; unpaid principal stays assigned; ordinary incomplete, paused/canceled and separate-worker requests cannot use the exception; future items remain unclaimed |
| D9 | Separate dated item, manual trigger and untriggered term-end fallback | Each path reaches a ONE_OFF invoice via named workflow; fallback dates the existing row once; trigger/fallback race cannot charge twice |
| D10 | Paused period in both modes | $0/SKIPPED subscription invoice, no separate claim, unchanged pending amounts/dates; capacity/payment timing not silently regenerated |
| D11 | Resume mid-period with overdue scheduled items | Subscription resume invoice includes eligible backlog; separate runner catches up; original total/service periods retained; no paused-period invoice rewritten |
| D12 | Natural end/cancellation/termination with no future invoice | Owner-authorized settlement/explicit one-off fallback only; pause is not a fallback trigger; original target or protected termination target reconciles |
| R1 | Two workers or subscription/manual delivery race on one item | Unique source binding plus locks gives exactly one invoice/line; loser recovers or skips, never frees a winner's assignment |
| R2 | Failure before invoice transaction commit | No claim/orphan line remains; retry creates one valid invoice |
| R3 | Crash after commit before activity response/handoff | Retry recovers same live invoice and durable processing; no second charge, including if source archived meanwhile |
| R4 | Finalization/provider/payment failure or deferred finalization | Assigned amount remains reserved; existing processing/retry mechanisms resume same invoice; payment failure never regenerates principal |
| R5 | Activity/workflow exhausted retries, worker restarts, disabled delivery | Archived-inclusive recovery finds stranded live bindings; adoption off never re-enables per-period true-up; delivery pause stops new claims only |
| R6 | More than one page/run of due items, equal dates, duplicate workflow IDs | All items eventually dispatched within bounded runs/history; stable cursor and no starvation, truncation or duplicate invoice |
| R7 | Actual schedule provisioning/queue registration/replay | Known-scope schedule exists and starts registered workflow/activity on right queues; missing scope/registration fails readiness; old workflow histories replay |
| R8 | Claim versus pause/resume/trial-end/activation/cancel, including grouped children | Shared lifecycle guard actually serializes eligibility; parent-row/invoice/guard/owner lock order has no inversion or missed new owner; no post-pause new claim |
| E1 | Edit before first invoice | Archive old set and regenerate full target eagerly; stale queued old IDs cannot invoice |
| E2 | Edit after partial invoicing, including drafts | Retained assigned principal unchanged; fresh upcoming items balance remaining target; state-token conflict on a concurrent claim |
| E3 | Full-contract conditional correction before and after selected voids | No activation against hypothetical release; full/partial actual release determines executable budget; explicit historical fresh IDs can be reissued |
| E4 | Void old archived-source invoice after another edit | Old item stays canceled, released gap defers this owner, replacement invoices unaffected; repeated void is a no-op |
| E5 | Recompute/recalculate source invoice and mixed invoices | In-place keeps linked principal/source IDs; destructive path requires explicit correction; unrelated lines preserved; identical labels do not merge items |
| E6 | Trigger replay, edit/trigger/fallback/pause/void race and no-op PUT | First date retained, archived IDs cannot re-arm, no-op preserves claims; one lock-ordered winner and meaningful conflict/defer outcome |
| V1 | Limits, fields, precision, timezone/calendar and all C/T validation rules | Shared valid/invalid cases across every write/preview entry; 400 leaves no writes, 409 leaves no partial archive |
| V2 | Tenant/environment/source/owner/recipient spoofing and no-Price source path | Foreign IDs fail all read/write/history/worker paths; only validated internal source bypasses ordinary Price requirement |
| V3 | Tax/discount/credits/custom FX and maximum aggregate invoice | Source principal remains in obligation currency; actual invoice totals adjust normally; no lost links or duplicated tax; downstream limits fail explicitly before partial claims |
| V4 | Migration and compatibility | Additive rule/item/source schema works on clean/current PostgreSQL; existing delivery column unchanged; Ent generated, no hand-edited output |
| V5 | Standalone future-owner adapter | No commitment/subscription/Price dependency; independent calendar, edit, both applicable delivery/lifecycle capabilities and same retry/claim model |
| A1 | Public Cost Explorer endpoint | separate commitments array, explicit usage_window and collection_scope=lifetime; existing usage totals unchanged |
| A2 | No events/invoices and filtered usage | owner remains visible; collection shown once, unsupported usage attribution explicit |
| A3 | Draft/finalized/voided/archived sources | assigned and invoiced principal match authoritative sources, detail exposes invoice status, no invented cash allocation |
| A4 | Commitment and history pagination | bounded filter/scope-bound cursors, independent pages and lifetime totals; no page sum masquerading as contract total |
| A5 | Duplicate/out-of-order collection projection events, outage and backfill | Real ClickHouse plus event-consumer integration tests establish idempotent repair, tenant/environment/90GB bounds, freshness and missing-readiness 503 rather than fabricated zero |

Use fixed clocks and explicit decimal expectations. Add property/fuzz tests where useful for conservation: allocated capacity never exceeded, rounded items sum to target, every active principal is assigned/scheduled/unallocated exactly once, and analytics fan-out preserves charge totals. Do not add a new test for each trivial getter or require a combinatorial test suite.

Minimum end-to-end rehearsal in one tenant/environment: create with $300 upfront plus recurring remainder; process opening invoice; ingest under-cap usage and verify $0 usage/no true-up with positive utilization; process one subscription $75 payment; repeat with separate delivery; pause before another due date and resume; create a trialing fixture whose upfront payment catches up at trial end; edit then void/replan an old source; interrupt/retry delivery after commit; verify all source references and derived balances. Also run a scope-isolation fixture. Observe the actual Temporal schedules/activities rather than invoking service methods only.

Map ERD phase-2 I1–I22 and I26–I91 to actual tests and the focused matrix above; recognition/analytics I18/I23–I25 belong to the separately integrated performance-obligation/analytics work, not fake core recognition. Run focused Go tests with `-race`, relevant commitment/subscription/invoice regression suites, `make lint-ci`, migration checks and required generation/build checks as their slices land. Documentation-only changes require static document/example verification, not a Go test run.

Roll out schema first, then complete writers/workers, provision one scope, validate preview/create/edit/both deliveries/void-gap-replan/recovery, then expand adoption. Rollback preserves audit rows, source bindings and the scheduled interpretation; never revive archived IDs or switch existing owners to per-period. Persist source history in PostgreSQL and publish operational events after commit; logs/webhooks are supplementary.

Use ctx-first structured logs for meaningful changes/failures, with literal `"error"` fields for errors, Info for obsolete/deferred decisions and no new Warn in business code. Metrics have bounded owner-type/delivery/outcome labels, not customer/item-ID cardinality. Reconciliation is scoped and reports missing allocation/source integrity; it never repairs money automatically.
