# Grouped Invoicing Subscription Type — Design Spec

**Date:** 2026-05-08  
**Status:** Approved  
**Author:** omkar273

---

## Overview

This spec introduces two new subscription types (`grouped_invoicing` and `delegated`), a user-facing `invoicing_behavior` field on the subscription creation DTO, and a new clubbed invoice billing flow for grouped invoicing. It also formalises the five subscription types that now govern the complete billing and hierarchy behaviour.

---

## Background

Two invoicing flows exist today:

1. **Customer/subscription inheritance** — `parent` + `inherited` types. Inherited children have no line items; usage aggregates to parent; one invoice per parent.
2. **Delegated pair invoicing** — any subscription can set `invoicing_customer_id` to route the invoice to a different customer. Currently modelled as `standalone` with no explicit type distinction.

This spec adds a third flow — **grouped invoicing** — and formalises the delegated pattern as its own type.

---

## Five Subscription Types

### Type Matrix

| Type | `parent_subscription_id` | `invoicing_customer_id` | Line items | Invoice |
|---|---|---|---|---|
| `standalone` | ✗ not set | ✗ not set | own | own |
| `delegated` | ✗ not set | ✓ required | own | raised against invoicing customer |
| `parent` | ✗ not set | optional | own | own + aggregates inherited usage; triggers grouped children invoice |
| `inherited` | ✓ required | ✗ not set | none | skipped — rolled into parent invoice |
| `grouped_invoicing` | ✓ required | optional | own | skipped — clubbed into parent invoice |

### Type Descriptions

**`standalone`** — Default. No hierarchy, no invoice delegation. All wallets, entitlements, and invoices belong to the subscription's own customer.

**`delegated`** — Subscription has its own line items and entitlements but the invoice is raised against a different customer (`invoicing_customer_id`). Used for multi-entity billing where one legal entity pays for another's subscription.

**`parent`** — Owns line items. Can have both `inherited` children (no line items, usage aggregated) and `grouped_invoicing` children (own line items, invoices clubbed). Credits from parent's wallet are used. Entitlements shared only with `inherited` children (not `grouped_invoicing`).

**`inherited`** — Skeleton subscription. No line items. Usage from child customer flows up to parent. Follows parent's billing period, anchor, status, pause, and cancellation. Cannot be cancelled directly.

**`grouped_invoicing`** — Has its own line items and entitlements (independent from parent). Invoice is not generated separately — it is clubbed into the parent's invoice as a flat merge. Billing period, anchor, and count must match parent. `start_date` must be ≥ parent `start_date`.

---

## DTO Changes

### `SubscriptionInheritanceConfig`

```go
type SubscriptionInheritanceConfig struct {
    // Explicit invoicing behavior. Defaults to standalone if config is nil.
    InvoicingBehavior types.SubscriptionType `json:"invoicing_behavior,omitempty"`

    // parent: create new inherited skeleton subscriptions for these customers
    ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`

    // inherited or grouped_invoicing: link to this parent subscription
    ParentSubscriptionID string `json:"parent_subscription_id,omitempty"`

    // delegated: invoice is raised against this customer (external ID)
    InvoicingCustomerExternalID *string `json:"invoicing_customer_external_id,omitempty"`

    // parent: convert these existing standalone subs to grouped_invoicing under this parent
    SubIDsForGroupedInvoicing []string `json:"sub_ids_for_grouped_invoicing,omitempty"`
}
```

### Validation Rules per `InvoicingBehavior`

| Behavior | Required | Rejected |
|---|---|---|
| `standalone` (default) | — | `parent_subscription_id`, `invoicing_customer_external_id`, `sub_ids_for_grouped_invoicing` |
| `delegated` | `invoicing_customer_external_id` | `parent_subscription_id` |
| `parent` | — | `parent_subscription_id` |
| `inherited` | `parent_subscription_id` | `invoicing_customer_external_id` |
| `grouped_invoicing` | `parent_subscription_id` | — |

If `Inheritance` is nil, behavior defaults to `standalone`.

Backward compat: if `InvoicingBehavior` is absent but other fields are set, the existing auto-detection logic applies (`parent_subscription_id` set → inherited, `external_customer_ids` set → parent, else standalone). Existing `standalone` subs with `invoicing_customer_id` set continue to work unchanged — `GetInvoicingCustomerID()` is not modified.

---

## Enum Changes

### `internal/types/subscription.go`

```go
const (
    SubscriptionTypeStandalone       SubscriptionType = "standalone"
    SubscriptionTypeDelegated        SubscriptionType = "delegated"        // NEW
    SubscriptionTypeParent           SubscriptionType = "parent"
    SubscriptionTypeInherited        SubscriptionType = "inherited"
    SubscriptionTypeGroupedInvoicing SubscriptionType = "grouped_invoicing" // NEW
)
```

No new DB columns. `subscription_type` is varchar(20); both new values fit. A migration is generated to update any DB-level check constraint.

---

## Service Layer

### `prepareSubscriptionInheritanceForCreate` (extended)

Switch on `InvoicingBehavior`:

- **`standalone`** — validate no conflicting fields; proceed as today
- **`delegated`** — resolve `invoicing_customer_external_id` to internal ID; set `sub.InvoicingCustomerID`; set `sub.SubscriptionType = delegated`
- **`parent`** — set `sub.SubscriptionType = parent`; if `ExternalCustomerIDsToInheritSubscription` present → existing inherited children flow; if `SubIDsForGroupedInvoicing` present → after parent persisted, call `addToGroupedInvoicing` for each
- **`inherited`** — existing linking logic (unchanged)
- **`grouped_invoicing`** — resolve parent; call `addToGroupedInvoicing`; set `sub.SubscriptionType = grouped_invoicing`; set `sub.ParentSubscriptionID`

### `addToGroupedInvoicing(ctx, parentSub, childSubID)`

Validates then updates child:

1. Child `subscription_type` == `standalone`
2. Child `subscription_status` in (`active`, `trialing`)
3. Child `parent_subscription_id` is not already set
4. Parent `subscription_type` == `parent`
5. Parent `subscription_status` in (`active`, `trialing`)
6. `child.BillingPeriod == parent.BillingPeriod`
7. `child.BillingPeriodCount == parent.BillingPeriodCount`
8. `child.BillingAnchor == parent.BillingAnchor`
9. `child.StartDate >= parent.StartDate`

On success: sets `child.SubscriptionType = grouped_invoicing`, sets `child.ParentSubscriptionID = parent.ID`.

### `removeFromGroupedInvoicing(ctx, childSubID)`

No validation beyond child existing and being `grouped_invoicing` type. Sets `child.SubscriptionType = standalone`, clears `child.ParentSubscriptionID`. Always succeeds.

### `getGroupedInvoicingSubscriptions(ctx, parentSubID)`

```
filter: subscription_type=grouped_invoicing
        parent_subscription_id=parentSubID
        subscription_status in (active, trialing, draft)
```

Mirrors existing `getInheritedSubscriptions`.

---

## Subscription Modification Service

Grouped invoicing membership changes are handled by the existing `SubscriptionModificationService` (in `internal/service/subscription_modification.go`), not `SubscriptionChangeService`. This keeps the preview/execute pattern consistent with the existing `inheritance` and `quantity_change` modification types.

### New Modification Types

```go
SubscriptionModifyTypeGroupedInvoicingAdd    SubscriptionModifyType = "grouped_invoicing_add"
SubscriptionModifyTypeGroupedInvoicingRemove SubscriptionModifyType = "grouped_invoicing_remove"
```

### Input DTO

```go
// Added to internal/api/dto/subscription_modification.go
type SubModifyGroupedInvoicingParams struct {
    // ParentSubscriptionID is required for grouped_invoicing_add.
    ParentSubscriptionID string   `json:"parent_subscription_id,omitempty"`
    ChildSubscriptionIDs []string `json:"child_subscription_ids" validate:"required,min=1"`
}

func (r *SubModifyGroupedInvoicingParams) Validate() error { ... }
```

`GroupedInvoicingParams *SubModifyGroupedInvoicingParams` is added to `ExecuteSubscriptionModifyRequest`.

### Preview

For each child, calls `validateAddToGroupedInvoicingDryRun` (add) or `validateRemoveFromGroupedInvoicingDryRun` (remove). Returns per-child `ChangedSubscription` entries in `ChangedResources.Subscriptions`. Writes nothing.

### Execute

Calls `addToGroupedInvoicing` / `removeFromGroupedInvoicing` for each child in a single transaction. Rolls back all if any child fails. Returns updated child subscription IDs in `ChangedResources.Subscriptions`.

---

## Billing Flow

### `UpdateBillingPeriods` — routing

```
grouped_invoicing  → skip invoice AND skip period advance (parent does both)
inherited          → existing skip (unchanged)
parent             → existing flow + new grouped_invoicing aggregation
standalone/delegated → existing flow (unchanged)
```

### Parent invoice with `grouped_invoicing` children

When a `parent` sub is processed:

1. `getGroupedInvoicingSubscriptions(parentSubID)` — collect active/trialing children
2. If no children → existing invoice flow, nothing changes
3. If children present:
   a. `PrepareSubscriptionInvoiceRequest` for parent (existing)
   b. `PrepareSubscriptionInvoiceRequest` for each child (fixed + usage charges)
   c. Flat-merge all `LineItems` slices into parent's `CreateInvoiceRequest`
   d. Full invoice computation pipeline runs on merged total: subtotal → discounts → parent's prepaid credits → taxes → amount due
   e. Invoice raised against `parent.GetInvoicingCustomerID()`
4. After invoice is finalised: advance `current_period_start/end` for parent, then for each grouped child

### New billing service method

```go
// Added to BillingService interface
PrepareGroupedInvoiceRequest(
    ctx context.Context,
    params *dto.PrepareGroupedInvoiceRequestParams,
) (*dto.CreateInvoiceRequest, error)

type PrepareGroupedInvoiceRequestParams struct {
    ParentSubscription *subscription.Subscription
    ChildSubscriptions []*subscription.Subscription
    PeriodStart        time.Time
    PeriodEnd          time.Time
}
```

Internally calls `PrepareSubscriptionInvoiceRequest` for each sub and flat-merges `LineItems`. No new invoice computation logic — existing pipeline is reused on the merged request.

### Credit application

Credits come from the **parent's wallet only** (via `parent.GetInvoicingCustomerID()`). Child wallets are not touched. This is the existing behaviour of the invoicing customer lookup.

### Entitlements

`grouped_invoicing` children have **independent entitlements** (per-child limits, per-child usage tracking). They are NOT shared with the parent or other children. This is unlike `inherited` children which share the parent's entitlements. `usageCustomerIDsForSubscription` is NOT extended for `grouped_invoicing` children.

---

## Ent Schema

`ent/schema/subscription.go` — no new columns. The `subscription_type` field comment is updated to document the two new values. `make generate-migration` produces a migration that adds `grouped_invoicing` and `delegated` to any check constraint on the column.

---

## Backward Compatibility

- All existing `standalone`, `parent`, `inherited` subscriptions are unaffected
- `standalone` subs with `invoicing_customer_id` set continue to work via unchanged `GetInvoicingCustomerID()` logic
- `SubscriptionInheritanceConfig` without `InvoicingBehavior` falls through to existing auto-detection
- No data migration; new enum values are purely additive

---

## Out of Scope

- Shared entitlements between `grouped_invoicing` siblings (each child is independent)
- Per-child credit wallets contributing to the clubbed invoice
- Mixed billing periods across `grouped_invoicing` children under one parent
- UI/dashboard grouping of line items by child subscription on the invoice PDF
