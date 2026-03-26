# Subscription Inheritance Execute API — Design Spec

## Goal

Two changes to the subscription system:
1. Replace flat inheritance fields on `CreateSubscriptionRequest` with a single nested `inheritance` object.
2. Add a new `POST /subscriptions/:id/inheritance/execute` endpoint that adds new child customers to an already-existing parent subscription.

---

## Background

When creating a parent subscription today, inheritance is declared via flat top-level fields:
- `external_customer_ids_to_inherit_subscription []string`
- `customer_ids_to_inherit_subscription []string`
- `invoicing_customer_id *string`
- `invoicing_customer_external_id *string`
- `parent_subscription_id *string`
- `invoice_billing *types.InvoiceBilling`

These are removed and replaced with a single `inheritance` object. The API is internal, so no backwards-compatibility shim is needed.

After a parent subscription is created, there is currently no way to add new children without recreating the subscription. The new execute endpoint fills this gap.

---

## Change 1: Restructure `CreateSubscriptionRequest`

### New DTO structs

**File:** `internal/api/dto/subscription.go`

Add:

```go
// SubscriptionInheritanceConfig groups all inheritance-related fields.
// InvoicingCustomerID and InvoicingCustomerExternalID use pointer semantics
// so nil ("not provided") can be distinguished from "" ("explicitly empty").
type SubscriptionInheritanceConfig struct {
    CustomerIDsToInheritSubscription         []string              `json:"customer_ids_to_inherit_subscription,omitempty"`
    ExternalCustomerIDsToInheritSubscription []string              `json:"external_customer_ids_to_inherit_subscription,omitempty"`
    ParentSubscriptionID                     string                `json:"parent_subscription_id,omitempty"`
    InvoicingCustomerID                      *string               `json:"invoicing_customer_id,omitempty"`
    InvoicingCustomerExternalID              *string               `json:"invoicing_customer_external_id,omitempty"`
    InvoiceBilling                           *types.InvoiceBilling `json:"invoice_billing,omitempty"`
}
```

Add:

```go
// ExecuteSubscriptionInheritanceRequest is the payload for
// POST /subscriptions/:id/inheritance/execute.
// Exactly one of the two arrays must be non-empty.
type ExecuteSubscriptionInheritanceRequest struct {
    CustomerIDsToInheritSubscription         []string `json:"customer_ids_to_inherit_subscription,omitempty"`
    ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`
}

func (r *ExecuteSubscriptionInheritanceRequest) Validate() error {
    bothProvided := len(r.CustomerIDsToInheritSubscription) > 0 &&
        len(r.ExternalCustomerIDsToInheritSubscription) > 0
    if bothProvided {
        return ierr.NewError("provide either customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription, not both").
            Mark(ierr.ErrValidation)
    }
    if len(r.CustomerIDsToInheritSubscription) == 0 && len(r.ExternalCustomerIDsToInheritSubscription) == 0 {
        return ierr.NewError("at least one customer ID is required").
            Mark(ierr.ErrValidation)
    }
    return nil
}
```

`Validate()` is called by the service at the start of `ExecuteSubscriptionInheritance`, consistent with the pattern used by other request DTOs in the codebase.

### Fields removed from `CreateSubscriptionRequest`

The following six flat fields are deleted entirely:

```go
// DELETE these:
InvoicingCustomerID                      *string               `json:"invoicing_customer_id,omitempty"`
InvoicingCustomerExternalID              *string               `json:"invoicing_customer_external_id,omitempty"`
ParentSubscriptionID                     *string               `json:"parent_subscription_id,omitempty"`
CustomerIDsToInheritSubscription         []string              `json:"customer_ids_to_inherit_subscription,omitempty"`
ExternalCustomerIDsToInheritSubscription []string              `json:"external_customer_ids_to_inherit_subscription,omitempty"`
InvoiceBilling                           *types.InvoiceBilling `json:"invoice_billing,omitempty"`
```

Replace with:

```go
Inheritance *SubscriptionInheritanceConfig `json:"inheritance,omitempty"`
```

### `Validate()` rewrite (same file)

Three validation blocks currently reference the flat fields and must be rewritten to read from `req.Inheritance`:

1. **Mutual-exclusivity of invoicing fields** (currently ~line 578–589):
   ```go
   if req.Inheritance != nil {
       invoiceCount := 0
       if req.Inheritance.InvoicingCustomerID != nil { invoiceCount++ }
       if req.Inheritance.InvoicingCustomerExternalID != nil { invoiceCount++ }
       if req.Inheritance.InvoiceBilling != nil { invoiceCount++ }
       if invoiceCount > 1 {
           return err("only one of invoicing_customer_id, invoicing_customer_external_id, invoice_billing")
       }
   }
   ```
2. **Mutual-exclusivity of child ID arrays** (currently ~line 591–595):
   ```go
   if req.Inheritance != nil &&
       len(req.Inheritance.CustomerIDsToInheritSubscription) > 0 &&
       len(req.Inheritance.ExternalCustomerIDsToInheritSubscription) > 0 {
       return err("provide either customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription, not both")
   }
   ```
3. **ParentSubscriptionID empty-string check** (currently ~line 737–741):
   ```go
   if req.Inheritance != nil && req.Inheritance.ParentSubscriptionID != "" {
       // existing check
   }
   ```
4. **Duplicate child-ID mutual-exclusivity check** (currently ~line 948 — identical in purpose to block 2 above): This second instance references the now-deleted flat fields and must be removed entirely. It becomes redundant after block 2 is rewritten to check `req.Inheritance.*`.

### `ToSubscription()` rewrite (same file)

`ToSubscription()` currently reads flat fields directly into the domain model. After the restructure it must read from `req.Inheritance`.

**Do NOT use `lo.Ternary` here** — Go evaluates all arguments eagerly before the call, so `r.Inheritance.InvoicingCustomerID` would panic when `r.Inheritance == nil`. Use explicit guards:

```go
// Before:
InvoicingCustomerID: r.InvoicingCustomerID,
ParentSubscriptionID: r.ParentSubscriptionID,

// After:
var invoicingCustomerID *string
var parentSubscriptionID *string
if r.Inheritance != nil {
    invoicingCustomerID = r.Inheritance.InvoicingCustomerID
    if r.Inheritance.ParentSubscriptionID != "" {
        parentSubscriptionID = lo.ToPtr(r.Inheritance.ParentSubscriptionID)
    }
}
// then use invoicingCustomerID and parentSubscriptionID in the struct literal
```

### Service changes: `resolveUsageCustomerIDs`

**File:** `internal/service/subscription.go`

`resolveUsageCustomerIDs` currently reads from flat fields. After the change it reads from `req.Inheritance`:

```go
func (s *subscriptionService) resolveUsageCustomerIDs(
    ctx context.Context,
    req *dto.CreateSubscriptionRequest,
) ([]string, error) {
    if req.Inheritance == nil {
        return nil, nil // STANDALONE — nil Inheritance is equivalent to "not provided"
    }
    // NOTE: {"inheritance": {}} with both arrays absent is also STANDALONE.
    // The nil-slice vs non-nil-empty-slice distinction from the old flat fields
    // is replaced by: non-nil Inheritance + at least one array populated = hierarchy.
    if len(req.Inheritance.CustomerIDsToInheritSubscription) > 0 {
        // resolve and validate internal IDs (existing logic, unchanged)
    }
    if len(req.Inheritance.ExternalCustomerIDsToInheritSubscription) > 0 {
        // resolve external → internal (existing logic, unchanged)
    }
    return nil, nil // both arrays empty → STANDALONE
}
```

All other reads of the flat fields in `CreateSubscription` (invoicing customer resolution at lines 93–121, `InvoiceBilling` at lines 100–106) are updated to read from `req.Inheritance.*`.

---

## Change 2: `POST /subscriptions/:id/inheritance/execute`

### Execution logic

1. Validate request: exactly one of `customer_ids_to_inherit_subscription` or `external_customer_ids_to_inherit_subscription` must be non-empty. Both provided → 400. Both empty → 400.
2. Fetch parent subscription by `:id`. Not found → 404.
3. Validate `subscription_type == "parent"`. STANDALONE or INHERITED → 400.
4. Resolve customer IDs:
   - External IDs → look up via `CustomerRepo`, error if any not found.
   - Internal IDs → validate all exist, error if any not found.
5. Fetch existing inherited subscriptions via `getInheritedSubscriptions(ctx, parentSubID)`.
   - This fetches active, trialing, draft, and paused children — **not cancelled**.
   - **Cancelled child edge case**: if a customer previously had an inherited sub under this parent that was then cancelled, they will NOT appear in the existing set, so the execute will create a fresh inherited sub for them. This is intentional — cancelled means the relationship ended, re-adding is a new creation.
6. Build a set of already-inherited customer IDs (from step 5). Remove those customers from the resolved list.
7. If filtered list is empty → return parent subscription immediately (pure no-op, 200 OK).
8. Run `validateCustomerSubscriptionWorkflow(ctx, parentSub.CustomerID, filteredCustomerIDs)` to catch STANDALONE conflicts.
9. Call `createInheritedSubscriptions(ctx, parentSub, filteredCustomerIDs)`.
10. Fetch parent subscription via `GetSubscription` and return as `*dto.SubscriptionResponse`.

### Service interface

**File:** `internal/interfaces/service.go` (where `SubscriptionService` interface is defined)

Add:

```go
ExecuteSubscriptionInheritance(ctx context.Context, parentSubID string, req *dto.ExecuteSubscriptionInheritanceRequest) (*dto.SubscriptionResponse, error)
```

### Service implementation

**File:** `internal/service/subscription.go`

```go
func (s *subscriptionService) ExecuteSubscriptionInheritance(
    ctx context.Context,
    parentSubID string,
    req *dto.ExecuteSubscriptionInheritanceRequest,
) (*dto.SubscriptionResponse, error)
```

Return type matches the interface convention used by all other subscription service methods.

### Handler

Added to the existing `SubscriptionHandler` (no new handler struct, no DI changes).

**File:** `internal/api/v1/subscription.go`

```go
func (h *SubscriptionHandler) ExecuteSubscriptionInheritance(c *gin.Context) {
    id := c.Param("id")
    if id == "" {
        c.Error(ierr.NewError("subscription ID is required").Mark(ierr.ErrValidation))
        return
    }
    var req dto.ExecuteSubscriptionInheritanceRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.Error(ierr.WithError(err).WithHint("Invalid request format").Mark(ierr.ErrValidation))
        return
    }
    resp, err := h.service.ExecuteSubscriptionInheritance(c.Request.Context(), id, &req)
    if err != nil {
        c.Error(err)
        return
    }
    c.JSON(http.StatusOK, resp)
}
```

### Route registration

**File:** `internal/api/router.go`

```go
subscription.POST("/:id/inheritance/execute", handlers.Subscription.ExecuteSubscriptionInheritance)
```

---

## Files Changed

| File | Change |
|------|--------|
| `internal/api/dto/subscription.go` | Add `SubscriptionInheritanceConfig`, `ExecuteSubscriptionInheritanceRequest`; remove 6 flat fields from `CreateSubscriptionRequest`; rewrite `Validate()` (3 blocks) and `ToSubscription()` |
| `internal/interfaces/service.go` | Add `ExecuteSubscriptionInheritance` to `SubscriptionService` interface |
| `internal/service/subscription.go` | Update `resolveUsageCustomerIDs` and all other flat-field reads in `CreateSubscription`; add `ExecuteSubscriptionInheritance` implementation |
| `internal/api/v1/subscription.go` | Add `ExecuteSubscriptionInheritance` handler |
| `internal/api/router.go` | Register new route |
| `internal/service/*_test.go` | Update all test files that construct `CreateSubscriptionRequest` with the flat fields (`subscription_test.go`, `invoice_test.go`, `wallet_payment_test.go`, `billing_test.go`) to use the new `Inheritance` object |

---

## Error Cases

| Scenario | HTTP | Message |
|----------|------|---------|
| Both arrays provided | 400 | provide either customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription, not both |
| Both arrays empty | 400 | at least one customer ID is required |
| Parent sub not found | 404 | subscription not found |
| Sub is not PARENT type | 400 | subscription is not a parent subscription |
| Customer ID not found | 400 | customer not found: `<id>` |
| STANDALONE conflict | 400 | existing hierarchy conflict error (from validateCustomerSubscriptionWorkflow) |
| All customers already inherited (non-cancelled) | 200 | returns parent sub unchanged — no-op |
| Previously-cancelled child re-added | 200 | creates new inherited sub — intentional |

---

## What Is NOT Changed

- `createInheritedSubscriptions` — reused as-is
- `validateCustomerSubscriptionWorkflow` — reused as-is
- `getInheritedSubscriptions` — reused as-is
- Cascade cancel/pause/resume — unaffected
- No new DB migrations needed
- No new DI wiring — handler added to existing `SubscriptionHandler`
