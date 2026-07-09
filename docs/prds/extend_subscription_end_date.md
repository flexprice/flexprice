# Extend Subscription End Date — PRD + ERD

**Linear:** [FLE-974](https://linear.app/flexprice/issue/FLE-974)   <!-- pragma: allowlist secret -->
**Status:** Implemented (v1)  
**Surface:** Subscription Modifications API (`POST /v1/subscriptions/:id/modify/{preview,execute}`)  
**Closest precedent:** `trial_end` modification (`internal/ee/service/subscription_modification_trial_end.go`)

---

## 1. Problem Statement

Today, subscriptions can be created with an `end_date`. That value is propagated to plan line items (and phase line items) at create time. After creation there is **no first-class API to push the subscription end further into the future**.

Operators who need a longer fixed term today must either:

1. Cancel and recreate the subscription (lossy: periods, invoices, entitlements, inheritance), or
2. Hack around via cancellation schedules / `cancel_at` updates (which **shorten** or schedule termination — they do not extend).

We need a controlled **extend end date** operation that:

- Updates `subscription.end_date`
- Propagates to the **right** line items (those that inherited the subscription end — not overrides / one-time / already-terminated segments)
- Cascades to inherited children
- Keeps billing, phases, credit grants, and cancellation state consistent
- Is available via the existing **subscription modifications** preview/execute APIs

---

## 2. Goals & Non-Goals

### Goals

| ID | Goal |
|----|------|
| G1 | Extend `subscription.end_date` to a later timestamp via modify APIs |
| G2 | Preview the exact subscription + line-item side effects before commit |
| G3 | Propagate the new end only to line items that currently share the old subscription end (or are open-ended under a fixed-term sub) |
| G4 | Preserve line items with **earlier override** end dates |
| G5 | Cascade to inherited child subscriptions |
| G6 | Clear or reconcile scheduled cancellation when extending past `cancel_at` |
| G7 | Keep last phase / credit-grant bounds consistent with the new end |
| G8 | Emit `subscription.updated` webhook (existing pattern) |

### Non-Goals (v1)

| ID | Non-goal |
|----|----------|
| NG1 | **Shortening** `end_date` (use cancel / scheduled cancel) |
| NG2 | Clearing `end_date` to make the subscription indefinite (follow-up) |
| NG3 | Per-line-item end-date extension as a separate modify type |
| NG4 | Proration / immediate invoice for the extension itself (extension only unlocks future periods) |
| NG5 | Extending already-cancelled subscriptions (no “resurrect”) |
| NG6 | Changing billing period, plan, or quantity as part of this call |

---

## 3. Current State (as-is)

### 3.1 Subscription `EndDate` semantics

| Field | Meaning |
|-------|---------|
| `EndDate *time.Time` | Hard term boundary. `nil` = indefinite. |
| `CancelAt *time.Time` | When cancellation is scheduled to take effect. |
| `CancelledAt *time.Time` | When cancellation was requested / applied. |
| `CancelAtPeriodEnd bool` | True for `end_of_period` / `scheduled_date` cancels. |

There is **no** separate `expired` status. Natural term end → period processor sets status `cancelled` when `period.end == *EndDate` and the end is not in the future.

**Create:** `CreateSubscriptionRequest.EndDate` (or last phase end) → `sub.EndDate`. First `CurrentPeriodEnd` is clipped via `types.NextBillingDate(..., SubscriptionEndDate: sub.EndDate)`.

**Cancel:**

| Type | Sub `EndDate` | Notes |
|------|---------------|-------|
| `immediate` | `= effectiveDate` | Status → `cancelled` |
| `end_of_period` | **not set yet** | `CancelAt = CurrentPeriodEnd`; line items bulk-terminated |
| `scheduled_date` | `= effectiveDate` immediately | May shorten `CurrentPeriodEnd` |

**PATCH update:** setting `cancel_at` also sets `EndDate = cancel_at`. There is **no** direct `end_date` update field today.

### 3.2 Line item `EndDate` semantics

- Domain uses `time.Time` where **zero = open-ended / still active**.
- On create, plan line items copy phase end or `sub.EndDate` (`subscription.go` create path).
- Overrides / explicit line-item `end_date` may be **earlier** than subscription end; validation rejects line item end **after** subscription end.
- Quantity-change versioning ends the old segment at `effective_date` and creates a new segment (often open-ended).
- One-time prices / one-time addons get short, charge-window end dates — **not** the subscription term.

### 3.3 Modification API pattern

```
POST /v1/subscriptions/:id/modify/preview
POST /v1/subscriptions/:id/modify/execute
```

Unified body: `ExecuteSubscriptionModifyRequest` with `type` + exactly one `*_params` object.

Existing types: `inheritance`, `quantity_change`, `grouped_invoicing`, `trial_end`, `coupon`, `tax`.

Response: `SubscriptionModifyResponse{ subscription, changed_resources }`.

---

## 4. Approaches Considered

### A. New modify type `end_date` (recommended)

Add `SubscriptionModifyTypeEndDate` with `end_date_params`, implemented in `subscription_modification_end_date.go`, mirroring `trial_end`.

| Pros | Cons |
|------|------|
| Matches product ask (“via subscription modifications APIs”) | One more type in the switch |
| Preview/execute + `ChangedResources` for free | Must carefully define line-item selection |
| Same auth, routing, webhook, cascade patterns | — |

### B. PATCH `/subscriptions/:id` with `end_date`

| Pros | Cons |
|------|------|
| Simple REST | No preview; inconsistent with mid-cycle ops |
| — | Easy to miss line-item / inheritance / grant side effects |

### C. Reuse cancel / schedule APIs “in reverse”

| Pros | Cons |
|------|------|
| Reuses schedule restore | Cancel APIs are terminate-oriented; confusing UX |
| — | Does not express “extend term” |

**Recommendation:** Approach **A**.

---

## 5. Product Requirements

### 5.1 Actor & eligibility

- **Caller:** authenticated tenant API (same RBAC as other modify write ops).
- **Target:** subscription identified by path `:id`.
- **Allowed statuses:** `active`, `trialing`, `paused` (paused: metadata-only extend; billing remains paused).
- **Rejected statuses:** `cancelled`, `draft`, `incomplete` (v1 — incomplete is payment-pending; do not mutate term).
- **Rejected types:** `inherited` — modify the **parent** instead (same rule as `trial_end`).

### 5.2 Request semantics

```json
{
  "type": "end_date",
  "end_date_params": {
    "new_end_date": "2027-01-01T00:00:00Z"
  }
}
```

| Field | Rules |
|-------|-------|
| `new_end_date` | Required. Must be strictly **after** current `subscription.end_date`. |
| Current `end_date` | Must be non-nil. Extending an indefinite subscription is out of scope (NG2). |
| vs `now` | Must be strictly after `time.Now().UTC()` (cannot extend into the past). |
| vs `start_date` | Must be after `subscription.start_date` (already implied by > old end). |
| vs `trial_end` | If trialing and `trial_end` is set, `new_end_date` must be **≥** `trial_end` (trial cannot outlive the term). |

**Idempotency:** if `new_end_date` equals current `EndDate` (same UTC instant), return success with empty mutations (no-op). If `new_end_date <= current EndDate` and not equal → validation error (extend-only).

### 5.3 Side effects (execute)

1. Set `sub.EndDate = new_end_date`.
2. **Line items:** update selected items’ `EndDate` to `new_end_date` (rules in §6).
3. **Phases:** if the subscription has phases, set the **last phase** `EndDate` to `new_end_date` when it previously equaled the old subscription end (or was the sole driver of sub end).
4. **CurrentPeriodEnd:** if `CurrentPeriodEnd` was clipped to the old end (equal to old `EndDate`), recompute the next period end via `types.NextBillingDate` with the new `SubscriptionEndDate` (may still cliff at the new end). Do **not** invent a longer current period if the natural cycle end is still before the new end.
5. **Scheduled cancellation (§7.2):** if `CancelAt` is set and `new_end_date > CancelAt`, clear cancellation intent (see §7.2).
6. **Inherited children:** cascade `EndDate` (and cancellation clear if applicable) + matching line-item updates.
7. **Credit grants (implicit):** extend subscription-scoped grants whose `EndDate` equaled the old subscription end to the new end. This is **not** an opt-in request flag — it always runs when matching grants exist. Preview and execute must return the modified grants in `changed_resources.credit_grants` (same pattern as subscriptions / line items).
8. **Webhook:** `subscription.updated` on the parent (and optionally each child — v1: parent only, matching trial_end).
9. **No proration invoice** for the extension itself.

### 5.4 Preview

Same validations and computed `changed_resources` as execute, **zero** DB writes. Include:

- `subscriptions[]` with `action: updated` for parent (+ inherited children that would change)
- `line_items[]` with `action: updated` and old→new end dates for each extended item
- `credit_grants[]` with `action: updated` for each grant whose end matched `old_end` (implicit side effect; include full grant payload)
- `invoices`: empty

---

## 6. Line Item Propagation Rules (critical)

### 6.1 Definitions

Let:

- `old_end` = previous `subscription.EndDate` (required, non-nil)
- `new_end` = requested `new_end_date`
- Line item end is “open” when domain `EndDate.IsZero()` (DB NULL)

### 6.2 Selection algorithm

For each line item on the subscription (and each inherited child, after cascade):

```
EXTEND item IFF all of:
  1. item is not soft-deleted (status active)
  2. entity is plan or recurring addon (NOT one-time charge window)
  3. item.EndDate is NOT before now in a way that means "already fully terminated segment"
     — specifically: skip if !EndDate.IsZero() AND EndDate.Before(old_end)
       (earlier override / quantity-change old segment / phase that ended earlier)
  4. AND one of:
       a. item.EndDate equals old_end (same UTC instant)  → inherited term
       b. item.EndDate.IsZero() AND subscription had a fixed term
          → open-ended under fixed-term sub; billing already cliffs at sub end;
            set EndDate = new_end so the term is explicit after extend
```

**Skip always:**

| Case | Reason |
|------|--------|
| `EndDate` strictly before `old_end` | Explicit override / prior version / earlier phase |
| One-time price / one-time addon association | Charge window, not term |
| Quantity-change **old** segments (`EndDate` = version cut) | Historical |
| Soft-deleted line items | Inactive |

**Do not create new line item versions** for an end-date extend — mutate `EndDate` in place (unlike quantity change).

### 6.3 Why “equals old_end” is the primary signal

At create time, plan line items copy `sub.EndDate`. Overrides that set a **shorter** end keep a different timestamp. After quantity changes, the **current** segment is often open-ended (`EndDate` zero) while the subscription still has a term — rule 4b covers that so billing continues to respect the (new) subscription cliff without leaving stale copied ends on older plan rows.

### 6.4 Addon associations

If an addon association’s `EndDate` equals `old_end` (e.g. scheduled cancel terminated the association to the sub end), extend the association `EndDate` to `new_end` together with its line items. One-time addon associations keep their short ends.

### 6.5 Worked examples

**E1 — Simple fixed term**

- Sub end: `2026-12-31`
- Line items A,B: end `2026-12-31`
- Extend to `2027-06-30` → A,B → `2027-06-30`

**E2 — Line item override shorter**

- Sub end: `2026-12-31`
- Line A: `2026-12-31`, Line B: `2026-06-30` (override)
- Extend to `2027-06-30` → A updated; **B unchanged**

**E3 — Quantity change mid-term**

- Sub end: `2026-12-31`
- Old LI ended at `2026-03-01`; new LI open-ended
- Extend → old LI unchanged; new LI gets `EndDate = 2027-…` (rule 4b)

**E4 — One-time addon**

- Onetime LI end = period boundary in March
- Extend sub → onetime LI **unchanged**

**E5 — Phased subscription**

- Phase 1 ends `2026-06-01`, Phase 2 ends `2026-12-31` (= sub end)
- Phase 1 LIs end `2026-06-01` → skip
- Phase 2 LIs end `2026-12-31` → extend; last phase end → new end; sub end → new end

---

## 7. Edge Cases & Controls

### 7.1 Status matrix

| Status | Behavior |
|--------|----------|
| `active` | Allow |
| `trialing` | Allow; enforce `new_end >= trial_end` |
| `paused` | Allow (term metadata); no resume/billing side effects |
| `cancelled` | Reject |
| `draft` / `incomplete` | Reject |
| `inherited` | Reject — use parent |

### 7.2 Interaction with scheduled cancellation

| Situation | v1 behavior |
|-----------|-------------|
| `CancelAt == nil` | No-op on cancel fields |
| `CancelAt` set and `new_end_date <= CancelAt` | Reject — extension does not reach past scheduled cancel; cancel or clear cancel first |
| `CancelAt` set and `new_end_date > CancelAt` | **Unwind scheduled cancellation:** clear `CancelAt`, `CancelAtPeriodEnd`; if a pending cancellation schedule exists, mark cancelled / restore using the same spirit as `restoreCancellationState` but with the **new** end as the restored `EndDate`; re-extend line items that were bulk-terminated to `CancelAt` when they matched the cancel boundary |

Rationale: extending the term past a scheduled cancel is an explicit operator intent to keep the subscription alive longer. Document this clearly in API docs.

**Open product decision (default above):** If product prefers “reject whenever `CancelAt` is set”, flip to hard reject and require an explicit cancel-of-cancellation first. The implementation should keep the unwind path behind a clear code path either way.

### 7.3 `CurrentPeriodEnd` clipped to old end

If `CurrentPeriodEnd.Equal(old_end)`:

1. Recompute candidate next end with `NextBillingDate` from `CurrentPeriodStart` (or from last natural boundary — prefer same params as period processor) with `SubscriptionEndDate: new_end`.
2. Set `CurrentPeriodEnd` to that candidate (still ≤ `new_end`).

If `CurrentPeriodEnd` is already before `old_end` (normal mid-cycle), leave it unchanged.

### 7.4 Credit grants

- Grants with `EndDate == old_end`: update to `new_end` **implicitly** (no request flag).
- Grants with earlier ends: unchanged.
- Grants with nil end: unchanged.
- Do **not** create new grant applications retrospectively.
- **Response:** always surface mutated grants in `changed_resources.credit_grants` (id, action, end_date, credit_grant) on both preview and execute — same visibility bar as the updated subscription.

### 7.5 Commitment / true-up

Extending the term may allow additional commitment windows in future periods. v1 does **not** rewrite historical commitment buckets; future billing uses the new sub end via existing `SubscriptionEndDate` clipping.

### 7.6 Inheritance

- Parent extend cascades `EndDate` to each `inherited` child.
- Apply the **same** line-item selection algorithm on each child.
- If parent unwind clears `CancelAt`, cascade those clears (mirror `CascadeCancelToInheritedSubscriptions` field set).

### 7.7 Timezone

- Persist and compare in **UTC**.
- Period recomputation uses `sub.Timezone` + `BillingAnchor` via existing `NextBillingDate`.
- API accepts RFC3339; normalize with `.UTC()` before compare/store.

### 7.8 Concurrency

- Execute inside `DB.WithTx`.
- Re-read subscription at start of TX; re-validate `EndDate` still equals the `old_end` used for line-item matching (optimistic concurrency). If another writer changed end/cancel mid-flight → abort with conflict/validation error.

### 7.9 Idempotency

- No new idempotency-key header in v1 (consistent with other modify types).
- Equal `new_end_date` → no-op success.
- Clients should treat retries as safe when the end is already at the requested value.

---

## 8. API Contract (ERD / OpenAPI sketch)

### 8.1 Types

```go
// dto/subscription_modification.go

const SubscriptionModifyTypeEndDate SubscriptionModifyType = "end_date"

type SubModifyEndDateRequest struct {
    // NewEndDate is the new subscription end. Must be strictly after the current end_date.
    NewEndDate time.Time `json:"new_end_date" binding:"required"`
}

func (r *SubModifyEndDateRequest) Validate() error {
    if r.NewEndDate.IsZero() {
        return ierr.NewError("new_end_date is required").Mark(ierr.ErrValidation)
    }
    return nil
}
```

Extend `ExecuteSubscriptionModifyRequest`:

```go
EndDateParams *SubModifyEndDateRequest `json:"end_date_params,omitempty"`
```

Update `Validate()` switch + error hint list of valid types.

Optionally enrich `ChangedSubscription` with:

```go
EndDate *time.Time `json:"end_date,omitempty"`
```

so preview/execute responses surface the new term without requiring clients to diff full `subscription`.

### 8.2 Example execute

**Request**

```http
POST /v1/subscriptions/subs_123/modify/execute
Content-Type: application/json

{
  "type": "end_date",
  "end_date_params": {
    "new_end_date": "2027-06-30T00:00:00Z"
  }
}
```

**Response (200)**

```json
{
  "subscription": { "...": "full subscription with end_date=2027-06-30T00:00:00Z" },
  "changed_resources": {
    "subscriptions": [
      {
        "id": "subs_123",
        "action": "updated",
        "status": "active",
        "end_date": "2027-06-30T00:00:00Z"
      }
    ],
    "line_items": [
      {
        "id": "sli_aaa",
        "price_id": "price_1",
        "quantity": "1",
        "end_date": "2027-06-30T00:00:00Z",
        "change_action": "updated"
      }
    ]
  }
}
```

### 8.3 Error catalog (representative)

| Condition | Error (message / hint) |
|-----------|-------------------------|
| Missing params | `end_date_params is required for type 'end_date'` |
| Sub has nil end | `subscription has no end_date to extend` / use create-time end or follow-up clear/set API |
| `new_end <= old_end` | `new_end_date must be after the current subscription end date` |
| `new_end` in the past | `new_end_date must be in the future` |
| Status cancelled | `cannot extend end date on cancelled subscription` |
| Inherited | `cannot modify end date on inherited subscription` |
| `new_end < trial_end` | `new_end_date cannot be before trial_end` |
| CancelAt in the way (if reject mode) | `subscription is scheduled to cancel; clear cancellation before extending` |

---

## 9. Low-Level Technical Design

### 9.1 Files to add / touch

| File | Change |
|------|--------|
| `internal/api/dto/subscription_modification.go` | Type, params, validate, optional `ChangedSubscription.EndDate` |
| `internal/ee/service/subscription_modification.go` | Dispatch `Execute` / `Preview` cases |
| `internal/ee/service/subscription_modification_end_date.go` | **New** — validate, execute, preview, cascade, line-item select |
| `internal/ee/service/subscription_modification_test.go` (+ focused `_end_date_test.go`) | Table-driven unit tests |
| `internal/api/v1/subscription_modification.go` | Swagger description string update only |
| `docs/swagger/*` | Via `make swagger` after implementation |
| `internal/e2eprobe/checks/subscription_modification_flow.go` | Optional e2e case |
| `docs/FLOWS/subscription-lifecycle.md` | Note amend path for end-date extend |

**No Ent schema / migration** — `end_date` columns already exist.

### 9.2 Service sketch

```go
func (s *subscriptionModificationService) executeEndDate(
    ctx context.Context,
    subscriptionID string,
    params *dto.SubModifyEndDateRequest,
) (*dto.SubscriptionModifyResponse, error) {
    // 1. Load sub + line items
    // 2. validateEndDateRequest(sub, params)  // status, nil end, ordering, trial, cancel policy
    // 3. oldEnd := *sub.EndDate; newEnd := params.NewEndDate.UTC()
    // 4. if oldEnd.Equal(newEnd) { return no-op response }
    // 5. selectLineItemsToExtend(items, oldEnd)
    // 6. WithTx:
    //      - optional unwindScheduledCancellation
    //      - sub.EndDate = &newEnd; maybe recompute CurrentPeriodEnd
    //      - update last phase if needed
    //      - for each selected LI: li.EndDate = newEnd; LineItemRepo.Update
    //      - extend matching credit grants
    //      - cascade to inherited (subs + LIs + cancel fields)
    // 7. publishSystemEvent(subscription.updated)
    // 8. return SubscriptionModifyResponse
}
```

Helpers (private on `subscriptionModificationService`):

| Helper | Role |
|--------|------|
| `validateEndDateRequest` | Eligibility + date rules |
| `selectLineItemsToExtend` | §6 algorithm |
| `recomputePeriodEndAfterExtend` | §7.3 |
| `unwindScheduledCancellationForExtend` | §7.2 |
| `cascadeEndDateToInherited` | Children + their LIs |
| `extendCreditGrantsMatchingEnd` | Grant end == old_end |

Reuse:

- `getInheritedSubscriptions` (already on modification service)
- `publishSystemEvent`
- `types.NextBillingDate`
- Schedule restore patterns from `subscription_schedule.go` / `restoreCancellationState`

### 9.3 Layering

- Handler stays thin (already).
- All rules in `SubscriptionModificationService` (EE service layer).
- Repos only: `SubRepo.Update`, line item update, phase update, grant update, schedule update.
- No business logic in API DTO beyond structural `Validate()`.

### 9.4 Observability

Structured logs (zerolog via existing logger):

- `subscription_id`, `old_end_date`, `new_end_date`, `line_items_extended`, `inherited_cascaded`, `cancellation_unwound`

### 9.5 Billing impact (why no invoice)

`FilterLineItemsToBeInvoiced` already skips periods that start at/after `sub.EndDate`. Extending the end **re-enables** future period generation on the next cron/Temporal tick. Historical invoices remain immutable. No mid-cycle proration is required for a pure term extension.

---

## 10. Testing Plan

### 10.1 Unit / service tests

| Case | Expect |
|------|--------|
| Happy path extend + matching LIs | Sub + LIs updated; webhook path invoked |
| Preview does not persist | DB unchanged |
| Override LI with earlier end | Unchanged |
| Open-ended current qty-change segment | Gets new end |
| One-time LI | Unchanged |
| Nil sub end | Validation error |
| `new_end <= old_end` | Validation error |
| Equal new/old | No-op success |
| Cancelled / inherited | Reject |
| Trialing with `new_end < trial_end` | Reject |
| `CurrentPeriodEnd == old_end` | Recomputed ≤ new_end |
| Parent with inherited children | Children end + LIs cascade |
| Phased last phase | Last phase end updated; earlier phase LIs skipped |
| Grant end == old_end | Grant extended |
| Scheduled cancel + extend past CancelAt | Unwind (or reject per final product flag) |

### 10.2 Integration / e2e

- Create fixed-term sub → modify preview → execute → get subscription → assert ends.
- Run period processing across the old end boundary after extend → subscription remains active and invoices the next period.

---

## 11. Rollout

1. Land this design doc (FLE-974 first step).
2. Implement behind normal API availability (no feature flag required unless product wants gradual expose).
3. `make swagger` + SDK regen in a follow-up if public SDK consumers need the new enum immediately.
4. Update customer-facing API docs / changelog: new `type: end_date`.

---

## 12. Open Decisions (defaults chosen)

| # | Question | Default in this doc |
|---|----------|---------------------|
| D1 | Shorten / clear end in same API? | **No** (NG1/NG2) — extend only |
| D2 | Allow when `CancelAt` set? | **Yes if `new_end > CancelAt`**, unwind cancel; else reject |
| D3 | Extend open-ended LIs under fixed-term sub? | **Yes** (set explicit end = new_end) |
| D4 | Extend matching credit grants? | **Yes** when grant end == old_end |
| D5 | Paused subscriptions? | **Allow** |
| D6 | Incomplete subscriptions? | **Reject** |
| D7 | Child webhook events? | **Parent only** in v1 |

Product/engineering should confirm D2 and D3 before implementation; they are the highest-impact behavioral choices.

---

## 13. Implementation Checklist (for the coding PR)

- [x] DTO: `end_date` type + `SubModifyEndDateRequest` + request validate switch
- [x] Service: `subscription_modification_end_date.go` (validate / preview / execute / cascade)
- [x] Dispatch wiring in `Execute` / `Preview`
- [x] Line-item selector + tests for override / onetime / qty-change / phase cases
- [x] Cancellation unwind path + tests
- [x] Period-end recompute when clipped
- [x] Credit grant extend (**implicit**) + `changed_resources.credit_grants` on preview/execute
- [ ] Swagger annotations + `make swagger` (handler description updated; full regen follow-up)
- [x] Flow doc blurb in `docs/FLOWS/subscription-lifecycle.md`
- [ ] E2E probe case (optional but recommended)

---

## 14. References (code)

- Domain: `internal/domain/subscription/model.go`, `line_item.go`
- Create propagation: `internal/ee/service/subscription.go` (plan LI `EndDate` assignment)
- Line item validation: `internal/api/dto/subscription_line_item.go` (`line item end_date cannot be after subscription end date`)
- Cancel + period end: `updateSubscriptionForCancellation`, `processSubscriptionPeriod`
- Billing cliff: `billingService.FilterLineItemsToBeInvoiced`, `types.NextBillingDate`
- Modify framework: `internal/api/dto/subscription_modification.go`, `internal/ee/service/subscription_modification*.go`
- Cascade cancel: `CascadeCancelToInheritedSubscriptions`
- Trial modify template: `subscription_modification_trial_end.go`
