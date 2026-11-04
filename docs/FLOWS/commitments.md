# Commitments (v2) and billing terms

A code-level walkthrough of first-class commitments: what each file does, and how a commitment moves from the API
through billing, invoicing, payments and termination. Paths are relative to `internal/ee/service/` unless stated.
The design decisions behind this code live in `docs/design/2026-10-07-FLE-1513-commitments-revamp-erd.md` and
`docs/design/2026-11-02-FLE-1513-billing-terms-erd.md`.

## 1. The model in one paragraph

A commitment promises that a subscription will use at least some **capacity** on a **scope** of its usage lines, over a
**term**. The capacity is an amount (`type: amount`, money) or a quantity (`type: quantity`, units valued at a
committed price). It renews every subscription billing period (`measurement: billing_period`) or every usage bucket,
such as every hour (`measurement: window`, optionally limited to `active_windows` times of day). When an invoice is
computed, the scoped usage is poured into the capacity. Usage that fits is billed on **utilized** lines, usage beyond
it on **overage** lines. A **binding** commitment also bills the unused rest of its capacity as a **true-up**. Under
`collection_method: billing_terms` the commitment's whole value is billed on a schedule of **instalments** instead, so
usage within it is shown at $0 and no true-up is billed.

## 2. Vocabulary

| Term | Meaning | Where |
|---|---|---|
| Term | `[start_date, end_date)`. Open-ended (no `end_date`) runs with the subscription. `terminated_at` cuts it short | `domain/commitment/model.go` (`EffectiveEnd`, `EndedBy`, `IsActiveAt`) |
| Scope | Which usage lines the commitment covers: a subscription, a price group, a meter or a price | `scopeLinesAt`, `commitmentCoversLine` in `commitment_validation.go` |
| Commitment period | One subscription billing period cut to the term. Carries `Capacity` (the floor in units or money) and `Amount` (its money value) | `commitmentPeriod` in `commitment_periods.go` |
| Floor | The money a binding commitment bills at least, per period | `Period.Amount`, rendered in `commitmentLines` |
| Stack | The commitments of one subscription that share lines: same type, measurement and bucket size | `commitmentStack` in `commitment_engine.go` |
| Window | A time slice of the invoice period that usage and capacity are laid out on | `commitmentStack.Windows` |
| Pool | Capacity a commitment holds; windows draw from it (one pool per period, or one per bucket for `window`) | `stackedCommitment.Pools` / `PoolOf` |
| Waterfall | Per window, commitments in priority order drain their pools across their lines in fill order | `allocate` |
| Billing terms | Rules (`once` / `recurring`, `fixed` / `remainder`) that expand into instalments | `domain/billingterm` |
| Instalment | One scheduled bill of part of the commitment's total: `scheduled` → `invoiced` (or `cancelled`) | `domain/billingterm/instalment.go` |
| Settlement | The instalment a termination adds for the instalments already due but not yet billed | `settle` |
| v1 / legacy | Commitment fields on a subscription line item, billed by `commitmentCalculator` | `billing_commitment.go` |

## 3. File map

| File | Responsibility |
|---|---|
| `commitment.go` | Service interface and lifecycle: enable gate, create (lock, replay, prepare, insert), update, terminate, fee line, overlap listing, valuation helpers, responses |
| `commitment_validation.go` | Everything checked on a new commitment: loading the subscription's line history (`commitmentContext`), resolving scope lines, term, scope entity, scope lines, overlaps |
| `commitment_guards.go` | Checks run by other flows when a subscription changes: a line or price joining or leaving a commitment's scope |
| `commitment_periods.go` | Period math: commitment periods, proration, window bucket counting, term value |
| `billing_commitments.go` | The billing loader: picks the commitments for one invoice calculation, lays usage and capacity onto windows, and returns invoice lines |
| `commitment_engine.go` | Pure allocation and rendering: the waterfall and the utilized / true-up / overage lines |
| `commitment_billing_terms.go` | Planning instalments, get / preview / replace terms, settlement on termination, the due-instalments run for separate invoices |
| `commitment_instalments.go` | Triggering a manual (milestone) instalment |
| `billing_instalments.go` | Due instalments as fixed lines on a subscription invoice |
| `invoice_instalments.go` | Instalments meeting invoices: claim on finalize, draft refresh, stale-draft and in-flight payment guards, void reschedule |
| `billing_term_events.go` | `billing_terms.*` and `billing_instalment.*` webhooks, published after commit |
| `billing_commitment.go` | Legacy (v1) line-item commitments, plus bucket helpers (`generateBucketStarts`, `truncateToBucketStart`) the v2 code reuses |
| `internal/domain/commitment` | Commitment model, builder, repository interface, the `billingterm.Owner` adapter (`billing_owner.go`) |
| `internal/domain/billingterm` | Term and instalment models, `Expand` (terms → instalments), `NumberLabels` |
| `internal/api/v1/commitment.go`, `internal/api/dto/commitment.go`, `internal/api/dto/billing_term.go` | HTTP handlers and request / response types |

## 4. The gate

Everything v2 is behind the `commitments_v2` environment setting. `IsEnabled` ([commitment.go:71](../../internal/ee/service/commitment.go#L71))
reads it, `requireEnabled` refuses API writes when it is off, and the billing loader returns an empty loader when it is
off, so billing behaves exactly as before. Turning the flag off after v2 commitments exist silently stops their pricing,
so it must not be flipped back.

## 5. Creating a commitment

API: `POST /v1/commitments` ([router.go:730](../../internal/api/router.go#L730)) → `CreateCommitmentRequest`.

1. **DTO** (`dto/commitment.go`). `Validate` checks field-level rules: required fields, `active_windows` only with
   `measurement: window`, billing terms only with `collection_method: billing_terms`. `prepare` then moves the start,
   end and term dates that fall within a second after a billing period edge to that edge (`atPeriodEdge`), and
   `ToCommitment` builds the domain model.
2. **`Create` → `create`** ([commitment.go:112](../../internal/ee/service/commitment.go#L112)). In a transaction it takes the
   subscription's commitments lock (`commitments:<subscription_id>`), so concurrent creates and scope changes on one
   subscription see each other. An idempotency key seen before returns the original commitment (`findReplay`,
   `replayResponse`).
3. **`insert` → `prepare`** ([commitment.go:289](../../internal/ee/service/commitment.go#L289)) validates and builds without writing:
   - `loadCommitmentContext`: the subscription with **every line it has ever had**, their prices and meters. A term can
     reach back before the current lines.
   - `validateAgainstSubscription` ([commitment_validation.go:162](../../internal/ee/service/commitment_validation.go#L162)):
     - `validateTerm`: the subscription can take a commitment, and the term fits inside it.
     - `validateScopeEntity`: the scope names something real.
     - `validateTermLines`: over the whole term the scoped lines must not have entitled meters, must be on one meter
       where needed, share one bucket size for windows, carry no legacy commitment fields, and bill every period.
     - `validateOverlaps`: commitments sharing a line must agree on type, measurement, bucket size and overage price,
       and differ in priority while they allocate together. `blocksUntil` lets a commitment keep blocking until the
       end of the billing period holding its end, because that period's invoice still allocates it.
   - The committed and overage rate prices are validated; `termValue` values the term (nil when open-ended).
   - Under billing terms: `total_amount` is set to the term value, and `planInstalments` expands the terms (section 9).
4. **`insert`** writes the commitment, creates the subscription-scoped rate prices (`createRatePrice`) and, under
   billing terms, the terms and instalments. Webhooks go out after commit.

`POST /v1/commitments/preview` runs the same `prepare` and writes nothing.

## 6. Billing: how a commitment changes an invoice

### 6.1 Where it hooks in

Both usage billing paths build a loader for the invoice period and hand it the lines it owns:

| Path | Loader built | Lines billed |
|---|---|---|
| Raw events | `CalculateUsageCharges` [billing.go:884](../../internal/ee/service/billing.go#L884) | [billing.go:1396](../../internal/ee/service/billing.go#L1396) |
| `meter_usage` | `CalculateMeterUsageCharges` [billing_meter_usage.go:193](../../internal/ee/service/billing_meter_usage.go#L193) | [billing_meter_usage.go:450](../../internal/ee/service/billing_meter_usage.go#L450) |

`HandledLineIDs()` lists the usage lines commitments price. Normal (and legacy v1) pricing skips them. `BillingLines()`
then returns the commitment's lines, which are appended to the usage charges.

Analytics and previews use `newProjectedCommitmentLoader`, which prices a window inside the open period as if through
the period's end but reads usage only up to the window's end: `attachCommitmentBreakdown`
([subscription.go:6712](../../internal/ee/service/subscription.go#L6712)) and `calculateLineItemCommitments`
([meter_usage.go:2869](../../internal/ee/service/meter_usage.go#L2869)).

### 6.2 Building the loader — `newCommitmentLoader` ([billing_commitments.go:58](../../internal/ee/service/billing_commitments.go#L58))

1. `ListOverlapping`: active and terminated commitments whose term overlaps the invoice period.
2. `loadCommitmentContext` once for the subscription.
3. `load` per commitment ([billing_commitments.go:147](../../internal/ee/service/billing_commitments.go#L147)):
   - Skip it when it ended before the period, or when none of its scope lines are billed by this invoice.
   - Load the committed price (quantity) and the overage price; build `amountOf`, which values capacity in money.
   - `commitmentPeriods(window)` returns the billing periods overlapping the invoice window, cut to the term.
     `containingPeriod` picks the one whose regular billing period holds the window (section 7.3).
   - `clipCommitmentPeriod` cuts that period to the invoice window and to `terminated_at`. A partial invoice or a
     mid-period termination therefore gets a prorated floor, marked `Clipped`.
   - Record the scope lines, bucket size and overage price (`loadedScope`), and the `stackedCommitment`.
4. `sortCommitmentStack` sorts by priority, then start, then ID. `checkSharedLines` rejects a line shared by commitments
   of different stacks, which would bill its usage twice.

### 6.3 Laying out a stack — `Stacks` ([billing_commitments.go:291](../../internal/ee/service/billing_commitments.go#L291))

Commitments are grouped into stacks by (type, measurement, bucket size). Each stack is laid out, then allocated.

**Period measurement — `buildPeriodStack`** ([billing_commitments.go:653](../../internal/ee/service/billing_commitments.go#L653))
- Windows are the invoice period split at every commitment start and end, every line version edge, and every
  usage bucket key (`periodSplits`).
- Bucketed prices put each bucket's usage in its own window (`splitLineIntoBuckets`). Other prices read their period
  quantity, price it once like normal billing, and spread it across the spans between commitment edges
  (`splitLineAcrossBoundaries`, `quantitiesPerSpan`, `spreadCost`).
- Each commitment has **one pool**, the period's capacity. Windows where `appliesAt` is false (before its start, at or
  after its term end) get `noPool`.

**Window measurement — `buildWindowStack`** ([billing_commitments.go:550](../../internal/ee/service/billing_commitments.go#L550))
- Windows are the usage buckets of the elapsed period (`generateBucketStarts`), so an empty bucket still counts.
- Each commitment gets **one pool per bucket** it applies to, each holding `value`. A bucket starting before the
  invoice period belongs to the previous invoice.
- The floor counts the applying buckets with `activeBuckets`. `accruedAmount` values it as a difference of cumulative
  rounded values, so the partial floors of a period always add up to its whole floor.

`appliesAt(c, t)` ([billing_commitments.go:605](../../internal/ee/service/billing_commitments.go#L605)) is the single
question "does the commitment's capacity apply at t": within its term (ending with the subscription when open-ended),
and inside its active windows.

### 6.4 Allocating — `allocate` ([commitment_engine.go:137](../../internal/ee/service/commitment_engine.go#L137))

For each window, in time order, each commitment in priority order drains its pool across its lines in fill order
(`sortLinesForFill`). The first commitment fills completely before the next receives anything. Usage left after the
last commitment is overage. The result is, per commitment, used / unused / capacity, and per line and window, overage.
This file has no I/O.

### 6.5 Rendering — `commitmentInvoiceLines` ([commitment_engine.go:234](../../internal/ee/service/commitment_engine.go#L234))

- **Utilized** rows, one per line. An amount stack bills the usage cost it absorbed. A quantity stack prices the
  absorbed units once on the committed price, so tiers walk the total, and splits that by line.
- **True-up**, binding only: floor minus utilized, as a fixed line on the committed price (`trueUpRow`). On a clipped
  period, utilized value is capped at the partial floor.
- **Overage** rows per line, for usage left in windows a commitment covers: the overage price when set, otherwise the
  line's own price at its marginal cost, so tiers continue from where the commitment stopped (`overageLines`,
  `marginalCost`).
- **Plain usage** rows per line, for usage in windows no commitment covers (before its start, after its end or
  termination, outside its active windows): the line's own price, with no commitment metadata.
- `absorbDrift` makes individually rounded rows add up to their rounded target.

`BillingLines` then runs `coverWithBillingTerms` ([billing_commitments.go:374](../../internal/ee/service/billing_commitments.go#L374)).
For billing-terms commitments it zeroes the utilized rows and drops the true-up, because their instalments carry the
value. Overage is still billed.

## 7. Period math — `commitment_periods.go`

### 7.1 `commitmentPeriods(c, sub, amountOf, bucket, span)`

Returns the subscription billing periods overlapping `span`, each cut to the commitment's term. The term ends at
`end_date`, or with the subscription when there is none.
- The first period is the one holding `max(span.Start, start_date)` (`periodAt`).
- The last is the one holding the end of `span`; a period starting exactly at `span.End` lies outside it.
- Each period records its uncut **regular** billing period (`FullStart`, `FullEnd`, from the period grid), which is
  what proration divides by.
- `periodCapacity` gives the capacity within a span: for `billing_period`, `value × (span length / regular period
  length)`; for `window`, `value ×` the buckets that start in it (counted arithmetically for clock buckets, walked when
  there are active windows or calendar buckets).

Callers pass the span they need:

| Caller | Span |
|---|---|
| Billing loader | The invoice window |
| `termValue` (create, preview, responses) | The whole term |
| `planInstalments` | The whole term |

### 7.2 Other helpers

- `clipCommitmentPeriod`: cuts a period to a span, recomputing capacity, with the amount from `accruedAmount`.
- `periodAt`, `periodEndAt`, `walkToPeriodAt`: locate billing periods from the subscription start and anchor.
  Multi-day and multi-week cadences step from the first period's end, so they are walked.

### 7.3 `containingPeriod` and whole seconds

`containingPeriod(periods, window)` ([billing_commitments.go:1026](../../internal/ee/service/billing_commitments.go#L1026))
checks **containment in the regular billing period**, not equality with the commitment period. Example: a monthly
subscription anchored on the 10th, and a monthly $100 commitment starting 15 Jan.

| Invoice window | Commitment period (cut to term) | Regular period | Contains? | Floor |
|---|---|---|---|---|
| 10 Jan – 10 Feb | 15 Jan – 10 Feb | 10 Jan – 10 Feb | yes | 100 × 26/31 = 83.87 |
| 10 Feb – 10 Mar | 10 Feb – 10 Mar | 10 Feb – 10 Mar | yes | 100 |
| 10 Jul – 10 Aug (term ends 15 Jul) | 10 Jul – 15 Jul | 10 Jul – 10 Aug | yes | 100 × 5/31 |

The first and last periods match through their regular period. The check only fails when a window straddles two
billing periods, such as an analytics window from 20 Jan to 20 Feb, or lies outside the term. The commitment is then
skipped for that window and logged at debug.

Billing period edges are whole seconds. `types.NextBillingDate` builds every edge after the first with
`time.Date(..., sec, 0, ...)`, and subscription creation drops the fraction from the start and anchor too, so a new
subscription's edges are all whole seconds. Commitment code builds its period grid on the anchor's whole second, so it
lands on the stored edges even for a subscription created before that, whose start and anchor may sit mid-second.

Edges still compare at whole seconds here, and in `clipCommitmentPeriod`, `periodSplits`, `validateTerm` and
`scopeLinesAtStart`, for those older subscriptions' mid-second first period. Commitment and billing-term dates may carry
a fraction; `atPeriodEdge` takes one within a second after a period edge as the edge, so a date derived from a
mid-second start does not leave a half-second sliver period behind.

## 8. Changing a subscription under a commitment — `commitment_guards.go`

Other flows call these inside their own transaction:

| Guard | Rejects | Called from |
|---|---|---|
| `checkLineAttach` | A usage line joining a scope with legacy commitment fields (`checkLegacyLineFields`) or a price that breaks the scope rules (`CheckPriceAttach`) | line add / update (`subscription_line_item.go`), quantity change, line-item modification, addon attach |
| `CheckPriceAttach` | A price whose meter, bucket size or overage disagrees with the commitments it joins (`checkPriceFits`, `checkCoveringCommitmentsAgree`) | the above, and line create in `subscription_line_item.go` |
| `checkLineDetach` | Ending a line when an active binding commitment would be left with a gap in coverage (`coverageGap`) | line end / delete, line-item modification, addon detach |
| `createPriceSyncLine` | Plan and addon price sync: inserts the line under the commitments lock; a violating price is logged and skipped | `plan.go` (v1 and v2 sync), `addon.go` |

## 9. Billing terms and instalments

### 9.1 Planning — `planInstalments` ([commitment_billing_terms.go:23](../../internal/ee/service/commitment_billing_terms.go#L23))

1. `left = total_amount − invoiced instalments` (all of it on create).
2. The term's periods are valued (`commitmentPeriods` over the whole term), and so are its anniversary periods
   (`anniversaryPeriods`: billing-period lengths from the commitment start, each the sum of the commitment periods'
   parts it spans). Both go to `billingterm.Expand` with a `plannedOwner`, which collects only `left` and starts
   undated terms at the current period on an edit.
3. `Expand` (`domain/billingterm/schedule.go`):
   - `resolveTerm` applies each kind's defaults and checks dates: a once term's `bill_on`; a recurring term's
     `start_date`, `end_date` and `period_count`, whose steps are cut by `cutSteps` from the commitment periods when
     its instalments go on the subscription invoice, or from the anniversary periods when invoiced separately.
   - `resolveAmounts` gives the remainder term whatever the fixed terms leave.
   - `instalmentAmounts` splits each term over its instalments: evenly, or in proportion to the period values; the
     last absorbs rounding.
4. `NumberLabels` ([schedule.go:113](../../internal/domain/billingterm/schedule.go#L113)) fills `{n}` and `{of}` in the
   new instalments, in billing order. Only labels that use them take a number, so a custom label such as a
   milestone's leaves no gap. On an edit the new ones are numbered after the invoiced instalments that took a number;
   `countNumbered` reads those instalments' terms in any status, since an earlier edit may have archived them.

### 9.2 Editing — `ReplaceBillingTerms` ([commitment_billing_terms.go:212](../../internal/ee/service/commitment_billing_terms.go#L212))

The edit runs in one transaction:
1. Take the commitment lock (`commitment:<id>`).
2. `requireActiveBillingTerms`.
3. `requireNoDraftPayment`: lock each draft carrying the commitment's instalments (`invoice:<id>`), and refuse while one
   has a pending, processing or succeeded payment.
4. Cancel the scheduled instalments.
5. Plan against the invoiced ones (`planReplacement`).
6. Archive the old terms and write the new terms and instalments.

`PreviewBillingTerms` plans without writing. Invoiced instalments and their invoices are never touched.

### 9.3 Billing an instalment

Each instalment carries its delivery: its term's `instalment_invoicing`, or the commitment's when the term sets none.

| Delivery | How it is billed |
|---|---|
| `subscription_invoice` | `PrepareSubscriptionInvoiceRequest` adds the scheduled instalments due by the invoice's boundary as fixed lines ([billing.go:1721](../../internal/ee/service/billing.go#L1721), `instalmentBillAt`, `dueInstalmentLines`). A grouped parent's draft also carries its children's. |
| `separate_invoice` | The `BillingInstalmentsDue` Temporal schedule (`internal/temporal/service/schedules.go`) runs `BillDueInstalments`. For each environment with v2 on, `billInstalment` issues a one-off invoice per due instalment under the commitment lock. |

Instalments wait while the subscription is trialing or paused. `TriggerInstalment` sets a manual milestone's `bill_on`
(it may only move earlier).

### 9.4 Claiming, refreshing and voiding — `invoice_instalments.go`

- **Claim on finalize.** `claimInstalments` moves `scheduled` → `invoiced` in the finalize transaction
  (`performFinalizeInvoiceActions` [invoice.go:1562](../../internal/ee/service/invoice.go#L1562), and payment-driven
  finalization [payment_processor.go:958](../../internal/ee/service/payment_processor.go#L958)). Computing a draft never claims.
- **Refresh.** `refreshStaleDraft` recomputes a subscription draft before it is finalized when it carries a
  no-longer-scheduled instalment, misses a due one, or was computed before a commitment of the subscription changed (`FinalizeInvoice` [invoice.go:1336](../../internal/ee/service/invoice.go#L1336),
  `ProcessDraftInvoice` [invoice.go:1961](../../internal/ee/service/invoice.go#L1961)). A manually edited draft is never
  recomputed, and an instalment line's amount can't be edited.
- **Charging.** `requireFreshDraft` refuses to charge a stale draft, both when the payment is created
  ([payment.go:257](../../internal/ee/service/payment.go#L257)) and in `ProcessPayment`. In `ProcessPayment` it runs under the
  invoice lock together with the move to processing ([payment_processor.go:88](../../internal/ee/service/payment_processor.go#L88)).
- **Void.** `rescheduleInstalments` puts a voided invoice's instalments back to `scheduled`, under their commitments'
  locks ([invoice.go:1888](../../internal/ee/service/invoice.go#L1888)).

### 9.5 Events

`billing_term_events.go` publishes `billing_terms.created` / `.updated` and `billing_instalment.created` / `.updated` /
`.cancelled` / `.invoiced`. They are held until the transaction commits (`publishAfterCommit`). Payload builders in
`internal/webhook/payload/billing_term.go` read the current rows when the event is sent.

## 10. Terminating

`POST /v1/commitments/{id}/terminate` → `Terminate` → `terminate` ([commitment.go:678](../../internal/ee/service/commitment.go#L678)),
under the commitment lock:
1. Refuse unless the commitment is active and `effective_at` is before its end; a fee needs a binding commitment.
2. Under billing terms, `settle` cancels the scheduled instalments and adds a settlement instalment for those already
   due by `effective_at`. Later ones are not billed and nothing is refunded; the fee covers anything more.
3. `billTermination` issues one immediate one-off invoice, `billing_reason: COMMITMENT_TERMINATION`, carrying the
   settlement and the cancellation fee (`cancellationFeeLine`).
4. Set the status to terminated with `terminated_at`. Later invoices clip the floor at `terminated_at`.

After the commit, `recomputeDrafts` recomputes the subscription's drafts and skipped period invoices for periods ending after
`effective_at`, so a backdated termination stops billing the commitment on drafts computed before it. `Create` does the
same from the commitment's start. A recompute that fails is caught when the draft is finalized (`refreshStaleDraft`).

Cancelling a subscription calls `TerminateForSubscription` (`CancelSubscription` [subscription.go:2287](../../internal/ee/service/subscription.go#L2287),
`TerminateSubscriptionResources` [subscription.go:5246](../../internal/ee/service/subscription.go#L5246)). A billing-terms commitment
that already ended still has its scheduled instalments billed (`settleEnded`), since no later subscription invoice
will carry them.

## 11. Legacy (v1) commitments and coexistence

v1 commitments are fields on a subscription line item, billed by `commitmentCalculator` in `billing_commitment.go`.
It is called per line from `CalculateUsageCharges` ([billing.go:1246](../../internal/ee/service/billing.go#L1246)) and
`applyMeterUsageCommitment` ([billing_meter_usage.go:659](../../internal/ee/service/billing_meter_usage.go#L659)). Both
kinds can live on one subscription on different lines:
- v2 lines are in `HandledLineIDs`, so v1 never sees them.
- `validateTermLines` and `checkLegacyLineFields` keep a line from carrying both.
- A subscription-level v1 commitment blocks v2 on that subscription.

## 12. Locks

| Key | Taken by | Serialises |
|---|---|---|
| `commitments:<subscription_id>` | create, the subscription-change guards (`checkLegacyLineFields`, `checkPriceAttach`, `checkLineDetach`), `TerminateForSubscription` | Overlap and scope checks against concurrent writes on one subscription |
| `commitment:<id>` | update, terminate, replace terms, trigger, `settleEnded`, separate-invoice billing, void reschedule | Writes to one commitment and its instalments |
| `invoice:<id>` | billing-terms edit (per draft), `ProcessPayment` | Starting a charge on a draft against changing what it bills |
