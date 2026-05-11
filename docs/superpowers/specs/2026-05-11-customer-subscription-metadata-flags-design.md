# Customer Subscription Metadata Flags

**Date:** 2026-05-11  
**Status:** Approved

## Problem

When listing customers it is impossible to tell at a glance whether a customer is a parent, child, or part of an invoicing group without querying and filtering subscriptions separately. We want this hierarchy information surfaced directly on the customer record via readonly metadata flags.

## Solution

Automatically set system-managed boolean flags in customer metadata whenever a subscription is created or its type changes. Flags use the `_fp_` prefix to mark them as readonly and system-owned. They are additive-only — once set to `"true"` they are never removed (subscription cancellation does not clear them).

## Metadata Keys

Defined as constants in `internal/types/customer_metadata.go`.

| Key | Meaning |
|-----|---------|
| `_fp_has_standalone_sub` | Customer has/had a `standalone` subscription |
| `_fp_has_parent_sub` | Customer has/had a `parent` subscription (is a parent with children) |
| `_fp_has_inherited_sub` | Customer has/had an `inherited` subscription (is a child in a hierarchy) |
| `_fp_has_grouped_invoicing_sub` | Customer has/had a `grouped_invoicing` subscription (invoice clubbed with parent) |
| `_fp_has_delegated_invoicing_sub` | Customer has/had a `delegated_invoicing` subscription (invoice raised against another customer) |

Value is always the string `"true"`. Keys are backed by the existing GIN index on `customer.metadata` (JSONB), so filtering with `metadata @> '{"_fp_has_parent_sub":"true"}'` is indexed.

## Readonly Enforcement

In the `UpdateCustomer` API handler (`internal/api/v1/customer.go`), validate that no key in the request metadata starts with `_fp_`. Return a `400` validation error if any such key is present. This runs before the request reaches the service or repository layer.

## Trigger Points

### 1. `CreateSubscription` — `internal/service/subscription.go`

After the subscription is persisted and `prepareSubscriptionInheritanceForCreate` has finalised the type, call `customerRepo.MergeMetadata` on the subscription's `CustomerID` with the flag corresponding to the resolved type:

| Subscription type | Flag set on customer |
|---|---|
| `standalone` | `_fp_has_standalone_sub` |
| `parent` | `_fp_has_parent_sub` |
| `inherited` | `_fp_has_inherited_sub` |
| `delegated_invoicing` | `_fp_has_delegated_invoicing_sub` |
| `grouped_invoicing` | `_fp_has_grouped_invoicing_sub` |

### 2. `addToGroupedInvoicing` — `internal/service/subscription_grouped_invoicing.go`

When a standalone subscription is converted to `grouped_invoicing`:
- Set `_fp_has_grouped_invoicing_sub` on the **child** customer (`child.CustomerID`)
- Set `_fp_has_parent_sub` on the **parent** customer (`parentSub.CustomerID`) — idempotent, likely already set

### 3. `createInheritedSubscriptions` — `internal/service/subscription.go`

When an inherited skeleton subscription is created for a child customer:
- Set `_fp_has_inherited_sub` on the **child** customer
- Parent customer flag is already set from `CreateSubscription`

## Repository Change

Add `MergeMetadata` to the customer repository interface (`internal/domain/customer/repository.go`) and implement it in `internal/repository/customer.go`:

```go
// MergeMetadata merges the given key-value pairs into the customer's existing
// metadata without overwriting unrelated keys.
MergeMetadata(ctx context.Context, customerID string, meta map[string]string) error
```

Implementation uses a PostgreSQL `jsonb` merge (`metadata || $2::jsonb`) so only the provided keys are touched. All existing user-set metadata is preserved.

## What Does Not Trigger an Update

- Subscription cancellation / expiry — flags remain as-is (noise accepted)
- `removeFromGroupedInvoicing` — treated the same as cancellation, no metadata change
- Manual DB edits — flags are not self-healing (acceptable; no recomputation needed)

## Filtering Examples

```sql
-- All parent customers
SELECT * FROM customers WHERE metadata @> '{"_fp_has_parent_sub":"true"}';

-- All child customers (inherited hierarchy)
SELECT * FROM customers WHERE metadata @> '{"_fp_has_inherited_sub":"true"}';

-- Customers with grouped invoicing
SELECT * FROM customers WHERE metadata @> '{"_fp_has_grouped_invoicing_sub":"true"}';
```

## Files Changed

| File | Change |
|------|--------|
| `internal/types/customer_metadata.go` | New — defines `_fp_*` key constants |
| `internal/domain/customer/repository.go` | Add `MergeMetadata` to interface |
| `internal/repository/customer.go` | Implement `MergeMetadata` |
| `internal/service/subscription.go` | Call `MergeMetadata` after `CreateSubscription` and `createInheritedSubscriptions` |
| `internal/service/subscription_grouped_invoicing.go` | Call `MergeMetadata` inside `addToGroupedInvoicing` |
| `internal/api/v1/customer.go` | Reject `_fp_*` keys in `UpdateCustomer` request validation |
