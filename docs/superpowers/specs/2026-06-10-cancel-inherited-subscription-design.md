# Cancel / Remove Inherited Child Subscription

**Date:** 2026-06-10  
**Status:** Approved for implementation

---

## Problem

Flexprice supports a parent-child subscription hierarchy. A `parent` subscription owns all line items and aggregates usage from one or more `inherited` child subscriptions — one per child customer. Today, cancelling an `inherited` subscription is a **hard block**:

```
"inherited subscription cannot be cancelled directly"
→ "Cancel the parent subscription instead; inherited subscriptions follow the parent lifecycle"
```

There is no way to remove a single child from the hierarchy without cancelling the entire parent (and therefore all other children too). This is a gap: operators need to offboard one child without affecting others.

---

## Goal

Allow a caller to **detach one or more child customers from a parent subscription** via the existing modify framework (`POST /subscriptions/:id/modify/execute` and `/modify/preview`), with the following behaviour:

- The child's `inherited` subscription is cancelled at the **parent's current period end**
- The child continues to contribute usage for the remainder of the current period (no mid-period split)
- The parent subscription stays as `SubscriptionTypeParent` regardless of how many children remain
- No auto-created standalone subscription for the removed child
- Full preview support before committing

---

## Orb Comparison

Orb's customer hierarchy model works similarly: removing a child takes effect at the current billing period end; the child's full-period usage is included in the parent's invoice; no replacement subscription is auto-created. The key difference is that Orb doesn't materialise a separate subscription record per child — it maintains a customer list on the parent subscription. Our model creates a real `inherited` subscription record per child, which gives a richer audit trail and a natural lifecycle anchor for the cancel timestamp.

---

## Subscription Type Reference

| Type | Line items | Invoice | Notes |
|------|-----------|---------|-------|
| `standalone` | Own | Own | No hierarchy |
| `parent` | Own | Own (aggregates child usage) | Drives inherited children |
| `inherited` | None (uses parent's) | None (covered by parent) | Skeleton, one per child customer |
| `grouped_invoicing` | Own | Raised against parent customer | Separate line items, clubbed invoice |
| `delegated_invoicing` | Own | Raised against different customer | |

---

## Design

### 1. DTO / API Contract

**File:** `internal/api/dto/subscription_modification.go`

Add `InheritanceAction` type and extend `SubModifyInheritanceRequest`:

```go
type InheritanceAction string

const (
    InheritanceActionAdd    InheritanceAction = "add"
    InheritanceActionRemove InheritanceAction = "remove"
)

type SubModifyInheritanceRequest struct {
    // Defaults to "add" when omitted — fully backward-compatible.
    Action InheritanceAction `json:"action,omitempty"`

    // For action="add": external customer IDs to inherit the subscription.
    ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`

    // For action="remove": external customer IDs whose inherited subscriptions to cancel.
    ExternalCustomerIDsToRemove []string `json:"external_customer_ids_to_remove,omitempty"`

    // CancellationType is reserved for future use.
    // Currently always end_of_period (parent.CurrentPeriodEnd). Omit from JSON for now.
}
```

**Validation rules:**

| `action` value | Required fields | Effective date |
|----------------|----------------|----------------|
| `""` or `"add"` | `external_customer_ids_to_inherit_subscription` non-empty | — |
| `"remove"` | `external_customer_ids_to_remove` non-empty | Always `parent.CurrentPeriodEnd` |

No new routes. Existing `POST :id/modify/execute` and `POST :id/modify/preview` are unchanged.

---

### 2. Service Logic

**File:** `internal/service/subscription_modification.go`

`executeInheritance` and `previewInheritance` each get an early branch:

```go
if params.Action == dto.InheritanceActionRemove {
    return s.executeRemoveInheritance(ctx, subscriptionID, params)
}
// existing add logic follows
```

#### `executeRemoveInheritance`

1. **Fetch & validate parent** — must be `SubscriptionTypeParent` and `status in (active, trialing)`
2. **Resolve customer IDs** — convert `ExternalCustomerIDsToRemove` → internal customer IDs using a simple customer-by-external-ID lookup (no status check — do NOT reuse `resolveExternalCustomersForInheritance`, which requires `StatusPublished` and is add-specific)
3. **Find each child's inherited sub** — filter: `parent_subscription_id = parentID AND customer_id = childID AND type = inherited AND status in (active, trialing)`; return error if not found
4. **Guard: already scheduled** — if `child.CancelAt != nil`, return error "inherited subscription is already scheduled for removal"
5. **Effective date** = `parent.CurrentPeriodEnd` (hardcoded, no `CancellationType` param)
6. **Transaction**: for each child inherited sub:
   - Set `cancel_at = effectiveDate`
   - Set `cancel_at_period_end = true`
   - Status remains `active` — child continues to contribute usage until period end
7. **Parent type unchanged** — stays `SubscriptionTypeParent`
8. Return `ChangedSubscription` list with `action = updated`, `status = active`

#### `previewRemoveInheritance`

Same validations as execute, no DB writes. Returns `ChangedSubscription` entries showing which inherited subs will be cancelled and their effective date.

---

### 3. Period-End Processing (Cron Guard)

**File:** `internal/service/subscription.go` → `processSubscriptionPeriod`

There is already a guard at line ~3007 that handles `SubscriptionTypeInherited` by skipping invoice generation and only advancing the billing period. This block currently ignores `cancel_at_period_end`. The fix is to extend it with a cancellation check **before** the period-advance logic:

```go
if sub.SubscriptionType == types.SubscriptionTypeInherited {
    // NEW: if scheduled for period-end removal, cancel without advancing period.
    if sub.CancelAtPeriodEnd && sub.CancelAt != nil {
        return s.cancelInheritedSubscriptionAtPeriodEnd(ctx, sub)
    }
    // Existing: just advance the period, no invoice created.
    newPeriod := periods[len(periods)-1]
    sub.CurrentPeriodStart = newPeriod.start
    sub.CurrentPeriodEnd = newPeriod.end
    ...
    return s.SubRepo.Update(ctx, sub)
}
```

`cancelInheritedSubscriptionAtPeriodEnd`:
- Sets `subscription_status = cancelled`, `cancelled_at = sub.CancelAt`, `end_date = sub.CancelAt`
- Does **not** generate an invoice — parent's invoice already covers this child's full-period usage
- Does **not** advance the period

---

### 4. Analytics & Usage Impact

**No changes required.** The child's `inherited` subscription remains `active` (status) until `parent.CurrentPeriodEnd`. This means:

- `getInheritedSubscriptions` (which filters for `active/trialing/draft`) still includes the child for the remainder of the period
- `usageCustomerIDsForSubscription` returns the child's customer ID as normal
- The parent's ClickHouse usage query includes the child's full-period events
- After period end the child's status becomes `cancelled` and they are automatically excluded from future usage queries

No mid-period proration, no additional ClickHouse queries, no per-customer time bounds.

---

### 5. Entitlements

No explicit entitlement management needed. Entitlement checks read subscription status. The child's status stays `active` until period end → access continues. After cancellation at period end, entitlement checks return no access.

---

### 6. State Transitions

**Before:**
```
parent_sub (type=parent, status=active)
  └── inherited_sub_child1 (type=inherited, status=active)
  └── inherited_sub_child2 (type=inherited, status=active)
```

**After `remove` with `external_customer_ids_to_remove=[child1]`:**
```
parent_sub (type=parent, status=active)          ← unchanged
  └── inherited_sub_child1 (type=inherited, status=active, cancel_at=periodEnd, cancel_at_period_end=true)
  └── inherited_sub_child2 (type=inherited, status=active)
```

**After period end fires:**
```
parent_sub (type=parent, status=active)
  └── inherited_sub_child1 (type=inherited, status=cancelled, cancelled_at=periodEnd)
  └── inherited_sub_child2 (type=inherited, status=active)
```

---

### 7. Files to Change

| File | Change |
|------|--------|
| `internal/api/dto/subscription_modification.go` | Add `InheritanceAction` type; extend `SubModifyInheritanceRequest` |
| `internal/service/subscription_modification.go` | Branch `executeInheritance` / `previewInheritance` on action; add `executeRemoveInheritance`, `previewRemoveInheritance` |
| `internal/service/subscription.go` | Add `cancelInheritedSubscriptionAtPeriodEnd`; add guard in `ProcessSubscriptionPeriod` |

No DB migrations, no new routes, no ClickHouse changes.

---

### 8. Out of Scope

- Immediate cancellation of inherited subs (always end-of-period for now; `CancellationType` reserved)
- Auto-creating a standalone subscription for the removed child
- Reverting parent to `standalone` when the last child is removed
- Mid-period proration of child usage contribution
