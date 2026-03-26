# Subscription Inheritance Execute API — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace flat inheritance fields on `CreateSubscriptionRequest` with a nested `inheritance` object, and add `POST /subscriptions/:id/inheritance/execute` to add children to an existing parent subscription.

**Architecture:** Three-layer change — DTO restructure in `dto/subscription.go`, service layer update in `service/subscription.go` + `interfaces/service.go`, and handler + route registration in `api/v1/subscription.go` + `api/router.go`. Test files in `internal/service/*_test.go` that construct `CreateSubscriptionRequest` with old flat fields must also be updated to use the new `Inheritance` object.

**Tech Stack:** Go 1.23, Gin, Ent ORM, `samber/lo`, `flexprice/internal/errors` (ierr)

**Spec:** `docs/superpowers/specs/2026-03-25-subscription-inheritance-execute-design.md`

---

## File Map

| File | Change |
|------|--------|
| `internal/api/dto/subscription.go` | Add 2 new structs; remove 6 flat fields; rewrite 4 `Validate()` blocks; rewrite `ToSubscription()` for 2 fields |
| `internal/interfaces/service.go` | Add `ExecuteSubscriptionInheritance` to `SubscriptionService` interface |
| `internal/service/subscription.go` | Update `resolveUsageCustomerIDs` + `CreateSubscription`; implement `ExecuteSubscriptionInheritance` |
| `internal/api/v1/subscription.go` | Add `ExecuteSubscriptionInheritance` handler |
| `internal/api/router.go` | Register new route |
| `internal/service/subscription_test.go` | Update all `CreateSubscriptionRequest` literals to use `Inheritance` |
| `internal/service/invoice_test.go` | Same |
| `internal/service/wallet_payment_test.go` | Same |
| `internal/service/billing_test.go` | Same |

---

## Task 1: Add new DTO structs

**Files:**
- Modify: `internal/api/dto/subscription.go`

This task ONLY adds the two new structs. No other changes yet.

- [ ] **Step 1: Add `SubscriptionInheritanceConfig` struct**

In `internal/api/dto/subscription.go`, find the line just before `type CreateSubscriptionRequest struct` and insert:

```go
// SubscriptionInheritanceConfig groups all inheritance-related fields for
// subscription creation. InvoicingCustomerID and InvoicingCustomerExternalID
// use pointer semantics so nil ("not provided") differs from "" ("explicitly empty").
type SubscriptionInheritanceConfig struct {
	CustomerIDsToInheritSubscription         []string              `json:"customer_ids_to_inherit_subscription,omitempty"`
	ExternalCustomerIDsToInheritSubscription []string              `json:"external_customer_ids_to_inherit_subscription,omitempty"`
	ParentSubscriptionID                     string                `json:"parent_subscription_id,omitempty"`
	InvoicingCustomerID                      *string               `json:"invoicing_customer_id,omitempty"`
	InvoicingCustomerExternalID              *string               `json:"invoicing_customer_external_id,omitempty"`
	InvoiceBilling                           *types.InvoiceBilling `json:"invoice_billing,omitempty"`
}

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
			WithHint("Send either internal or external customer IDs for inheritance, not both").
			Mark(ierr.ErrValidation)
	}
	if len(r.CustomerIDsToInheritSubscription) == 0 && len(r.ExternalCustomerIDsToInheritSubscription) == 0 {
		return ierr.NewError("at least one customer ID is required").
			WithHint("Provide customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription").
			Mark(ierr.ErrValidation)
	}
	return nil
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/api/dto/...
```

Expected: no errors (new structs don't break anything yet).

- [ ] **Step 3: Commit**

```bash
git add internal/api/dto/subscription.go
git commit -m "feat: add SubscriptionInheritanceConfig and ExecuteSubscriptionInheritanceRequest DTOs"
```

---

## Task 2: Restructure `CreateSubscriptionRequest`

**Files:**
- Modify: `internal/api/dto/subscription.go`

Replace the 6 flat fields and update all referencing methods in the same file.

- [ ] **Step 1: Replace the 6 flat fields on `CreateSubscriptionRequest`**

In `internal/api/dto/subscription.go`, find the struct `CreateSubscriptionRequest`.

**Delete** these 6 fields (currently at lines ~268, 273, 279, 368, 380, 385):
```go
InvoicingCustomerID                      *string               `json:"invoicing_customer_id,omitempty"`
InvoicingCustomerExternalID              *string               `json:"invoicing_customer_external_id,omitempty"`
InvoiceBilling                           *types.InvoiceBilling `json:"invoice_billing,omitempty"`
ParentSubscriptionID                     *string               `json:"parent_subscription_id,omitempty"`
CustomerIDsToInheritSubscription         []string              `json:"customer_ids_to_inherit_subscription,omitempty"`
ExternalCustomerIDsToInheritSubscription []string              `json:"external_customer_ids_to_inherit_subscription,omitempty"`
```

**Add** this single field in their place (put it near the bottom of the struct, before `SubscriptionType`):
```go
// Inheritance groups all customer-hierarchy fields.
// When provided, the subscription becomes a PARENT type.
Inheritance *SubscriptionInheritanceConfig `json:"inheritance,omitempty"`
```

- [ ] **Step 2: Rewrite `Validate()` — 4 blocks**

In `Validate()`, replace the 4 blocks that reference the deleted flat fields:

**Block 1** (currently ~lines 578-581) — replace:
```go
// OLD:
if r.InvoicingCustomerID != nil && r.InvoicingCustomerExternalID != nil {
    return ierr.NewError("only one of invoicing_customer_id or invoicing_customer_external_id may be provided").
        WithHint("Send either invoicing_customer_id or invoicing_customer_external_id, but not both").
        Mark(ierr.ErrValidation)
}
```
With:
```go
// NEW:
if r.Inheritance != nil && r.Inheritance.InvoicingCustomerID != nil && r.Inheritance.InvoicingCustomerExternalID != nil {
    return ierr.NewError("only one of invoicing_customer_id or invoicing_customer_external_id may be provided").
        WithHint("Send either invoicing_customer_id or invoicing_customer_external_id, but not both").
        Mark(ierr.ErrValidation)
}
```

**Block 2** (currently ~lines 584-588) — replace:
```go
// OLD:
if r.InvoiceBilling != nil && (r.InvoicingCustomerID != nil || r.InvoicingCustomerExternalID != nil) {
    return ierr.NewError("invoice_billing cannot be used together with invoicing_customer_id or invoicing_customer_external_id").
        WithHint("invoice_billing is deprecated; use invoicing_customer_id or invoicing_customer_external_id instead").
        Mark(ierr.ErrValidation)
}
```
With:
```go
// NEW:
if r.Inheritance != nil && r.Inheritance.InvoiceBilling != nil &&
    (r.Inheritance.InvoicingCustomerID != nil || r.Inheritance.InvoicingCustomerExternalID != nil) {
    return ierr.NewError("invoice_billing cannot be used together with invoicing_customer_id or invoicing_customer_external_id").
        WithHint("invoice_billing is deprecated; use invoicing_customer_id or invoicing_customer_external_id instead").
        Mark(ierr.ErrValidation)
}
```

**Block 3** (currently ~lines 590-594) — replace:
```go
// OLD:
if r.CustomerIDsToInheritSubscription != nil && r.ExternalCustomerIDsToInheritSubscription != nil {
    return ierr.NewError("only one of customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription may be provided").
        WithHint("Send either internal customer IDs or external customer IDs for inheritance, but not both").
        Mark(ierr.ErrValidation)
}
```
With:
```go
// NEW:
if r.Inheritance != nil &&
    len(r.Inheritance.CustomerIDsToInheritSubscription) > 0 &&
    len(r.Inheritance.ExternalCustomerIDsToInheritSubscription) > 0 {
    return ierr.NewError("only one of customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription may be provided").
        WithHint("Send either internal customer IDs or external customer IDs for inheritance, but not both").
        Mark(ierr.ErrValidation)
}
```

**Block 4** (currently ~lines 736-740) — **delete entirely**:

The old check guarded against `*string` being non-nil-but-empty. With the new plain `string` type and `omitempty`, an absent field JSON-deserializes to `""` — there is no way to pass a non-nil empty string, so the guard is redundant. Simply remove these lines:

```go
// DELETE entirely — no replacement needed:
if r.ParentSubscriptionID != nil && lo.FromPtr(r.ParentSubscriptionID) == "" {
    return ierr.NewError("parent_subscription_id cannot be empty when provided").
        WithHint("Omit parent_subscription_id or provide a non-empty subscription ID").
        Mark(ierr.ErrValidation)
}
```

**Block 5 — duplicate at ~line 948** — completely delete:
```go
// DELETE entirely:
if r.CustomerIDsToInheritSubscription != nil && r.ExternalCustomerIDsToInheritSubscription != nil {
    return ierr.NewError("only one of customer_ids_to_inherit_subscription or external_customer_ids_to_inherit_subscription may be provided").
        WithHint("Send either internal customer IDs or external customer IDs for inheritance, but not both").
        Mark(ierr.ErrValidation)
}
```

- [ ] **Step 3: Rewrite `ToSubscription()` — 2 lines**

In `ToSubscription()` (currently ~line 1130-1131), replace:
```go
// OLD:
InvoicingCustomerID:  r.InvoicingCustomerID,
ParentSubscriptionID: r.ParentSubscriptionID,
```
With (place BEFORE the struct literal, then reference the variables inside):
```go
// NEW — compute before the struct literal:
var invoicingCustomerID *string
var parentSubscriptionID *string
if r.Inheritance != nil {
    invoicingCustomerID = r.Inheritance.InvoicingCustomerID
    if r.Inheritance.ParentSubscriptionID != "" {
        parentSubscriptionID = lo.ToPtr(r.Inheritance.ParentSubscriptionID)
    }
}
// Then in the struct literal:
InvoicingCustomerID:  invoicingCustomerID,
ParentSubscriptionID: parentSubscriptionID,
```

- [ ] **Step 4: Check compile**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/api/...
```

Expected: errors in `internal/service/` because flat fields are no longer on the struct. `internal/api/` should compile cleanly.

- [ ] **Step 5: Commit**

```bash
git add internal/api/dto/subscription.go
git commit -m "feat: replace flat inheritance fields with nested Inheritance object on CreateSubscriptionRequest"
```

---

## Task 3: Update service layer

**Files:**
- Modify: `internal/service/subscription.go`
- Modify: `internal/interfaces/service.go`

- [ ] **Step 1: Update `resolveUsageCustomerIDs` to read from `req.Inheritance`**

In `internal/service/subscription.go`, find `resolveUsageCustomerIDs` (~line 6256).

Replace the two nil-checks at the top:
```go
// OLD:
if req.CustomerIDsToInheritSubscription != nil {
    ids := lo.Uniq(req.CustomerIDsToInheritSubscription)
    // ...
}
if req.ExternalCustomerIDsToInheritSubscription != nil {
    externalIDs := lo.Uniq(req.ExternalCustomerIDsToInheritSubscription)
    // ...
}
return nil, nil
```
With:
```go
// NEW:
if req.Inheritance == nil {
    return nil, nil // STANDALONE — no inheritance config provided
}
// NOTE: {"inheritance": {}} with both arrays absent is also STANDALONE.
if len(req.Inheritance.CustomerIDsToInheritSubscription) > 0 {
    ids := lo.Uniq(req.Inheritance.CustomerIDsToInheritSubscription)
    // ... (rest of the validation loop — unchanged, just update the field access)
    return ids, nil
}
if len(req.Inheritance.ExternalCustomerIDsToInheritSubscription) > 0 {
    externalIDs := lo.Uniq(req.Inheritance.ExternalCustomerIDsToInheritSubscription)
    // ... (rest of the resolution loop — unchanged, just update the field access)
    return lo.Uniq(resolvedIDs), nil
}
return nil, nil
```

The internal validation loops (`cf.CustomerIDs = ids`, `cf.ExternalIDs = externalIDs`, etc.) are identical — only the field access path changes.

**Semantic note — empty array behavior:** The old code used nil-vs-non-nil to distinguish "field omitted" from "field explicitly set to empty". The new code uses `len(...) > 0` — meaning `{"inheritance": {"customer_ids_to_inherit_subscription": []}}` (empty array) is now treated as STANDALONE, same as omitting the array entirely. This is intentional: an empty child list means "no children", not "PARENT with zero children".

- [ ] **Step 2: Update `CreateSubscription` flat field reads (~lines 94-136)**

The section that reads `req.InvoicingCustomerExternalID`, `req.InvoicingCustomerID`, `req.InvoiceBilling`, and `req.ParentSubscriptionID` directly must be updated. Replace:

```go
// OLD (~lines 94-136):
if req.InvoicingCustomerExternalID != nil && *req.InvoicingCustomerExternalID != "" {
    invoicingCustomer, err := s.CustomerRepo.GetByLookupKey(ctx, *req.InvoicingCustomerExternalID)
    if err != nil { return nil, err }
    req.InvoicingCustomerID = lo.ToPtr(invoicingCustomer.ID)
} else if req.InvoicingCustomerID == nil || *req.InvoicingCustomerID == "" {
    if lo.FromPtr(req.InvoiceBilling) == types.InvoiceBillingInvoiceToParent && customer.ParentCustomerID != nil {
        req.InvoicingCustomerID = customer.ParentCustomerID
    }
}
if req.InvoicingCustomerID != nil && *req.InvoicingCustomerID != "" {
    // validate invoicing customer exists and is active
}
if req.ParentSubscriptionID != nil && lo.FromPtr(req.ParentSubscriptionID) != "" {
    // validate parent subscription is active
}
```

With:
```go
// NEW:
if req.Inheritance != nil {
    if req.Inheritance.InvoicingCustomerExternalID != nil && *req.Inheritance.InvoicingCustomerExternalID != "" {
        invoicingCustomer, err := s.CustomerRepo.GetByLookupKey(ctx, *req.Inheritance.InvoicingCustomerExternalID)
        if err != nil { return nil, err }
        req.Inheritance.InvoicingCustomerID = lo.ToPtr(invoicingCustomer.ID)
    } else if req.Inheritance.InvoicingCustomerID == nil || *req.Inheritance.InvoicingCustomerID == "" {
        if lo.FromPtr(req.Inheritance.InvoiceBilling) == types.InvoiceBillingInvoiceToParent && customer.ParentCustomerID != nil {
            req.Inheritance.InvoicingCustomerID = customer.ParentCustomerID
        }
    }
    if req.Inheritance.InvoicingCustomerID != nil && *req.Inheritance.InvoicingCustomerID != "" {
        invoicingCustomer, err := s.CustomerRepo.Get(ctx, *req.Inheritance.InvoicingCustomerID)
        if err != nil { return nil, err }
        if invoicingCustomer.Status != types.StatusPublished {
            return nil, ierr.NewError("invoicing customer is not active").
                WithHint("The invoicing customer must be active").
                WithReportableDetails(map[string]interface{}{"invoicing_customer_id": *req.Inheritance.InvoicingCustomerID}).
                Mark(ierr.ErrValidation)
        }
    }
    if req.Inheritance.ParentSubscriptionID != "" {
        parentSub, err := s.SubRepo.Get(ctx, req.Inheritance.ParentSubscriptionID)
        if err != nil { return nil, err }
        if parentSub.SubscriptionStatus != types.SubscriptionStatusActive {
            return nil, ierr.NewError("parent subscription is not active").
                WithHint("The parent subscription must be active").
                WithReportableDetails(map[string]interface{}{"parent_subscription_id": req.Inheritance.ParentSubscriptionID}).
                Mark(ierr.ErrValidation)
        }
    }
}
```

- [ ] **Step 3: Add `ExecuteSubscriptionInheritance` to the interface**

In `internal/interfaces/service.go`, find `SubscriptionService` interface (~line 75). Add after the last method before the closing brace:

```go
// ExecuteSubscriptionInheritance adds new child customers to an existing parent subscription.
// Idempotent: customers who already have an inherited subscription under this parent are silently skipped.
ExecuteSubscriptionInheritance(ctx context.Context, parentSubID string, req *dto.ExecuteSubscriptionInheritanceRequest) (*dto.SubscriptionResponse, error)
```

- [ ] **Step 4: Implement `ExecuteSubscriptionInheritance` in the service**

In `internal/service/subscription.go`, add this method (add it near the end of the file, after `resolveUsageCustomerIDs`):

```go
// ExecuteSubscriptionInheritance adds new child customers to an existing PARENT subscription.
// Customers already inherited under this parent are silently skipped (idempotent).
// Previously-cancelled children are NOT skipped — they get a fresh inherited subscription.
func (s *subscriptionService) ExecuteSubscriptionInheritance(
	ctx context.Context,
	parentSubID string,
	req *dto.ExecuteSubscriptionInheritanceRequest,
) (*dto.SubscriptionResponse, error) {
	// 1. Validate request
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// 2. Fetch parent subscription
	parentSub, err := s.SubRepo.Get(ctx, parentSubID)
	if err != nil {
		return nil, err
	}

	// 3. Validate it is a PARENT type
	if parentSub.SubscriptionType != types.SubscriptionTypeParent {
		return nil, ierr.NewError("subscription is not a parent subscription").
			WithHint("Only PARENT subscriptions can have child subscriptions added via this endpoint").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":   parentSubID,
				"subscription_type": parentSub.SubscriptionType,
			}).
			Mark(ierr.ErrValidation)
	}

	// 4. Resolve customer IDs
	var resolvedIDs []string
	if len(req.CustomerIDsToInheritSubscription) > 0 {
		ids := lo.Uniq(req.CustomerIDsToInheritSubscription)
		cf := types.NewNoLimitCustomerFilter()
		cf.CustomerIDs = ids
		cf.Status = lo.ToPtr(types.StatusPublished)
		children, err := s.CustomerRepo.List(ctx, cf)
		if err != nil {
			return nil, err
		}
		byID := lo.SliceToMap(children, func(c *customer.Customer) (string, *customer.Customer) {
			return c.ID, c
		})
		for _, id := range ids {
			if _, ok := byID[id]; !ok {
				return nil, ierr.NewError("customer not found").
					WithHint("Each customer_id must be a valid published customer").
					WithReportableDetails(map[string]interface{}{"customer_id": id}).
					Mark(ierr.ErrValidation)
			}
		}
		resolvedIDs = ids
	} else {
		externalIDs := lo.Uniq(req.ExternalCustomerIDsToInheritSubscription)
		cf := types.NewNoLimitCustomerFilter()
		cf.ExternalIDs = externalIDs
		cf.Status = lo.ToPtr(types.StatusPublished)
		children, err := s.CustomerRepo.List(ctx, cf)
		if err != nil {
			return nil, err
		}
		byExternalID := lo.SliceToMap(children, func(c *customer.Customer) (string, *customer.Customer) {
			return c.ExternalID, c
		})
		resolvedIDs = make([]string, 0, len(externalIDs))
		for _, externalID := range externalIDs {
			child, ok := byExternalID[externalID]
			if !ok {
				return nil, ierr.NewError("customer not found").
					WithHint("Each external_customer_id must resolve to a valid published customer").
					WithReportableDetails(map[string]interface{}{"external_customer_id": externalID}).
					Mark(ierr.ErrValidation)
			}
			resolvedIDs = append(resolvedIDs, child.ID)
		}
		resolvedIDs = lo.Uniq(resolvedIDs)
	}

	// 5. Fetch existing (non-cancelled) inherited subscriptions for this parent
	existingChildren, err := s.getInheritedSubscriptions(ctx, parentSubID)
	if err != nil {
		return nil, err
	}
	alreadyInherited := make(map[string]bool, len(existingChildren))
	for _, child := range existingChildren {
		alreadyInherited[child.CustomerID] = true
	}

	// 6. Filter out already-inherited customers (idempotent skip)
	filtered := make([]string, 0, len(resolvedIDs))
	for _, id := range resolvedIDs {
		if !alreadyInherited[id] {
			filtered = append(filtered, id)
		}
	}

	// 7. No-op if all customers are already inherited
	if len(filtered) == 0 {
		return s.GetSubscription(ctx, parentSubID)
	}

	// 8. Validate no STANDALONE conflicts for the filtered set
	if err := s.validateCustomerSubscriptionWorkflow(ctx, parentSub.CustomerID, filtered); err != nil {
		return nil, err
	}

	// 9. Create inherited subscriptions
	if err := s.createInheritedSubscriptions(ctx, parentSub, filtered); err != nil {
		return nil, err
	}

	// 10. Return updated parent subscription
	return s.GetSubscription(ctx, parentSubID)
}
```

- [ ] **Step 5: Verify compile**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/service/... ./internal/interfaces/...
```

Expected: errors only in `*_test.go` files (those reference old flat fields — fixed in Task 4).

- [ ] **Step 6: Commit**

```bash
git add internal/service/subscription.go internal/interfaces/service.go
git commit -m "feat: update service layer for nested Inheritance object and add ExecuteSubscriptionInheritance"
```

---

## Task 4: Add handler and route

**Files:**
- Modify: `internal/api/v1/subscription.go`
- Modify: `internal/api/router.go`

- [ ] **Step 1: Add handler method**

In `internal/api/v1/subscription.go`, add this method (at the end of the file):

```go
// @Summary Execute subscription inheritance changes
// @ID executeSubscriptionInheritance
// @Description Adds new child customers to an existing PARENT subscription. Customers already inherited are silently skipped.
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Parent subscription ID"
// @Param request body dto.ExecuteSubscriptionInheritanceRequest true "Inheritance request"
// @Success 200 {object} dto.SubscriptionResponse
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 404 {object} ierr.ErrorResponse "Subscription not found"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /subscriptions/{id}/inheritance/execute [post]
func (h *SubscriptionHandler) ExecuteSubscriptionInheritance(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.Error(ierr.NewError("subscription ID is required").
			WithHint("Provide the subscription ID in the URL path").
			Mark(ierr.ErrValidation))
		return
	}
	var req dto.ExecuteSubscriptionInheritanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid request format").
			Mark(ierr.ErrValidation))
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

- [ ] **Step 2: Register route**

In `internal/api/router.go`, after the existing change routes (~line 297), add:

```go
// Subscription inheritance management
subscription.POST("/:id/inheritance/execute", handlers.Subscription.ExecuteSubscriptionInheritance)
```

- [ ] **Step 3: Verify compile**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go build ./internal/api/...
```

Expected: compiles cleanly (handler + route use the new interface method).

- [ ] **Step 4: Commit**

```bash
git add internal/api/v1/subscription.go internal/api/router.go
git commit -m "feat: add ExecuteSubscriptionInheritance handler and route POST /:id/inheritance/execute"
```

---

## Task 5: Fix test files

**Files:**
- Modify: `internal/service/subscription_test.go`
- Modify: `internal/service/invoice_test.go`
- Modify: `internal/service/wallet_payment_test.go`
- Modify: `internal/service/billing_test.go`

All test files that construct `dto.CreateSubscriptionRequest` with the old flat fields must be updated to nest those fields inside `Inheritance: &dto.SubscriptionInheritanceConfig{...}`.

- [ ] **Step 1: Find all affected lines**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && grep -rn \
  "InvoicingCustomerID\|InvoicingCustomerExternalID\|InvoiceBilling\|ParentSubscriptionID\|CustomerIDsToInheritSubscription\|ExternalCustomerIDsToInheritSubscription" \
  internal/service/
```

Note every file and line number.

- [ ] **Step 2: Update each occurrence**

For each test that sets flat fields, wrap them in `Inheritance`:

```go
// OLD pattern:
dto.CreateSubscriptionRequest{
    CustomerID: "...",
    InvoicingCustomerID: lo.ToPtr("cust_123"),
    ParentSubscriptionID: lo.ToPtr("subs_456"),
    ExternalCustomerIDsToInheritSubscription: []string{"ext_1"},
}

// NEW pattern:
dto.CreateSubscriptionRequest{
    CustomerID: "...",
    Inheritance: &dto.SubscriptionInheritanceConfig{
        InvoicingCustomerID: lo.ToPtr("cust_123"),
        ParentSubscriptionID: "subs_456",          // note: plain string, not *string
        ExternalCustomerIDsToInheritSubscription: []string{"ext_1"},
    },
}
```

Key type changes to watch for:
- `ParentSubscriptionID` was `*string` → now `string` (no `lo.ToPtr()` needed)
- `InvoicingCustomerID` is still `*string`
- `InvoicingCustomerExternalID` is still `*string`
- `InvoiceBilling` is still `*types.InvoiceBilling`

- [ ] **Step 3: Run tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go test ./internal/service/... -count=1 -timeout 120s 2>&1 | tail -30
```

Expected: all tests pass (or at most pre-existing failures unrelated to this change).

- [ ] **Step 4: Run go vet**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && go vet ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/service/subscription_test.go internal/service/invoice_test.go \
        internal/service/wallet_payment_test.go internal/service/billing_test.go
git commit -m "fix: update test files to use nested Inheritance object on CreateSubscriptionRequest"
```

---

## Task 6: Swagger regeneration

**Files:**
- Modify: `docs/swagger/` (generated)

- [ ] **Step 1: Regenerate Swagger**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice && make swagger
```

- [ ] **Step 2: Commit**

```bash
git add docs/swagger/
git commit -m "docs: regenerate swagger after inheritance API changes"
```

---

## Final Checks

- [ ] Full build: `go build ./...`
- [ ] Full vet: `go vet ./...`
- [ ] Tests: `go test ./internal/service/... -count=1 -timeout 120s`
- [ ] Push: `git push origin feat/cust-hierarchy-v4`
