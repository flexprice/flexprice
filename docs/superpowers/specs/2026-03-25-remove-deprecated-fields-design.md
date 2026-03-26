# Remove Deprecated Fields — Design Spec

## Goal

Remove two sets of deprecated fields from the application layer only. No DB migrations are needed — DB columns become unused dead weight.

1. **`parent_customer_id`** — remove from Customer domain model, DTOs, service logic, repository, and filter types.
2. **`invoice_billing`** — remove from `SubscriptionInheritanceConfig`, related validation, service fallback logic, and the `InvoiceBilling` type definition itself.

---

## Background

### `parent_customer_id` on Customer

Before the subscription inheritance model matured, the system tried to track parent/child customer relationships at the Customer entity level via `ParentCustomerID`. This is now superseded by subscription-level inheritance (`parent_subscription_id`, `invoicing_customer_id`). The field is still present in the domain model, DTOs, repository, and service layer but serves no runtime purpose.

### `invoice_billing` on Subscription

`InvoiceBilling` was an enum (`invoice_to_parent` / `invoice_to_self`) that controlled which customer received the invoice for an inherited subscription. This was replaced by the explicit `invoicing_customer_id` field on the subscription. A fallback block in `CreateSubscription` still reads both `req.Inheritance.InvoiceBilling` and `customer.ParentCustomerID` to auto-populate `InvoicingCustomerID` — once both deprecated fields are gone, this block is deleted too.

---

## Scope

### Application-level removal only

The `parent_customer_id` DB column is left in place — the column becomes unused. No schema migration is produced. This is the safer approach: avoids any risk of data loss, and the column can be formally dropped in a separate housekeeping migration later.

---

## Change 1: Remove `parent_customer_id` from Customer

### Domain model

**File:** `internal/domain/customer/model.go`

Delete:
```go
// Deprecated
ParentCustomerID *string `db:"parent_customer_id" json:"parent_customer_id"`
```

### DTOs

**File:** `internal/api/dto/customer.go`

From `CreateCustomerRequest`, delete:
```go
ParentCustomerID         *string `json:"parent_customer_id,omitempty"`
ParentCustomerExternalID *string `json:"parent_customer_external_id,omitempty"`
```

From `UpdateCustomerRequest`, delete the same two fields.

From `CustomerResponse`, delete:
```go
ParentCustomer *CustomerResponse `json:"parent_customer,omitempty"`
```

### Filter types

**File:** `internal/types/customer.go`

Delete:
```go
ParentCustomerIDs []string
```
from the customer filter/query struct.

### Repository

**File:** `internal/repository/ent/customer.go`

- **Create**: remove `SetNillableParentCustomerID(req.ParentCustomerID)` call.
- **Update**: remove `SetNillableParentCustomerID(req.ParentCustomerID)` call.
- **List/filter**: remove the `ParentCustomerIDIn(filter.ParentCustomerIDs)` predicate (and its non-nil guard).

### Service

**File:** `internal/service/customer.go`

- **`CreateCustomer`**: delete the block that resolves `ParentCustomerExternalID` → `ParentCustomerID` (looks up customer by external ID, sets `req.ParentCustomerID`).
- **`GetCustomer`**: delete the block that hydrates `ParentCustomer` in the response (fetches parent by ID, attaches to `CustomerResponse.ParentCustomer`).
- **`ListCustomers`**: delete the batch-load block that collects parent customer IDs from results and bulk-fetches them to populate `ParentCustomer` on each response item.
- **`UpdateCustomer`**: delete any block that reads/writes `ParentCustomerID` during an update.

---

## Change 2: Remove `invoice_billing` from Subscription

### DTO struct field

**File:** `internal/api/dto/subscription.go`

From `SubscriptionInheritanceConfig`, delete:
```go
InvoiceBilling *types.InvoiceBilling `json:"invoice_billing,omitempty"`
```

### Validation blocks

**File:** `internal/api/dto/subscription.go` — `Validate()` method

Delete the block that checks mutual exclusivity of `InvoicingCustomerID`, `InvoicingCustomerExternalID`, and `InvoiceBilling`:
```go
// DELETE: invoiceCount check block
invoiceCount := 0
if req.Inheritance.InvoicingCustomerID != nil { invoiceCount++ }
if req.Inheritance.InvoicingCustomerExternalID != nil { invoiceCount++ }
if req.Inheritance.InvoiceBilling != nil { invoiceCount++ }
if invoiceCount > 1 {
    return err("only one of invoicing_customer_id, invoicing_customer_external_id, invoice_billing")
}
```

After removal, `InvoicingCustomerID` and `InvoicingCustomerExternalID` are still mutually exclusive with each other. That two-way check is the only one that remains.

### Service fallback block

**File:** `internal/service/subscription.go`

Delete the entire block (~line 101–104):
```go
if lo.FromPtr(req.Inheritance.InvoiceBilling) == types.InvoiceBillingInvoiceToParent && customer.ParentCustomerID != nil {
    req.Inheritance.InvoicingCustomerID = customer.ParentCustomerID
}
```

This block is the only remaining place that reads `InvoiceBilling` and `customer.ParentCustomerID` at runtime. Removing it means callers must explicitly pass `invoicing_customer_id` or `invoicing_customer_external_id` when they want non-self invoicing — there is no more auto-inference.

### Type definition

**File:** `internal/types/subscription.go`

Delete entirely:
```go
type InvoiceBilling string

const (
    InvoiceBillingInvoiceToParent InvoiceBilling = "invoice_to_parent"
    InvoiceBillingInvoiceToSelf   InvoiceBilling = "invoice_to_self"
)
```

---

## Files Changed

| File | Change |
|------|--------|
| `internal/domain/customer/model.go` | Delete `ParentCustomerID *string` |
| `internal/api/dto/customer.go` | Delete `ParentCustomerID`, `ParentCustomerExternalID` from Create/Update requests; delete `ParentCustomer` from response |
| `internal/types/customer.go` | Delete `ParentCustomerIDs []string` from filter |
| `internal/repository/ent/customer.go` | Delete `SetNillableParentCustomerID` from create/update; delete `ParentCustomerIDIn` filter predicate |
| `internal/service/customer.go` | Delete parent resolution in Create, hydration in Get, batch load in List, write in Update |
| `internal/api/dto/subscription.go` | Delete `InvoiceBilling` field from `SubscriptionInheritanceConfig`; delete `invoiceCount` validation block |
| `internal/service/subscription.go` | Delete `InvoiceBilling`/`ParentCustomerID` fallback block |
| `internal/types/subscription.go` | Delete `InvoiceBilling` type + both constants |
| Test files | Fix any compilation errors from removed fields (`*_test.go` in `internal/service/`) |

---

## What Is NOT Changed

- `parent_customer_id` DB column — left in place, becomes unused dead weight
- `invoicing_customer_id` on subscription domain model — stays (it is the replacement)
- `InvoicingCustomerID` / `InvoicingCustomerExternalID` on `SubscriptionInheritanceConfig` — stays
- All subscription inheritance logic (`createInheritedSubscriptions`, `validateCustomerSubscriptionWorkflow`, `getInheritedSubscriptions`) — unaffected
- No Ent schema changes, no DB migrations

---

## Behaviour After Removal

- Passing `parent_customer_id`, `parent_customer_external_id` in a Create/Update Customer request → field silently ignored (JSON unmarshal finds no matching struct tag).
- `GET /customers/:id` response no longer includes `parent_customer`.
- Creating a parent subscription with `invoice_billing` in the inheritance object → field silently ignored.
- The auto-inference of `invoicing_customer_id` from `customer.parent_customer_id` is gone; callers must set `invoicing_customer_id` or `invoicing_customer_external_id` explicitly if they want invoicing to go to a different customer.
