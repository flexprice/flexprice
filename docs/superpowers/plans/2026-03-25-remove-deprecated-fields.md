# Remove Deprecated Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove `parent_customer_id` (Customer) and `invoice_billing` (Subscription) from the application layer — DB columns stay untouched.

**Architecture:** Two independent removal clusters. InvoiceBilling removal is self-contained (types + dto + service). ParentCustomerID removal cascades through domain model → DTO → filter types → expand config → repository → service → testutil → tests. All changes are pure deletions; no new logic is introduced.

**Tech Stack:** Go 1.23, Gin, Ent ORM, samber/lo

---

## File Map

| File | Change |
|------|--------|
| `internal/types/subscription.go` | Delete `InvoiceBilling` type + 2 constants + 2 methods |
| `internal/api/dto/subscription.go` | Delete `InvoiceBilling` field + 2 validation blocks |
| `internal/service/subscription.go` | Delete `InvoiceBilling`/`ParentCustomerID` fallback block |
| `internal/domain/customer/model.go` | Delete `ParentCustomerID *string` field + `FromEnt` assignment line |
| `internal/api/dto/customer.go` | Delete `ParentCustomerID`, `ParentCustomerExternalID` from Create/Update; delete `ParentCustomer` from response; remove validation blocks; remove from `ToCustomer` |
| `internal/types/customer.go` | Delete `ParentCustomerIDs []string` from filter |
| `internal/types/expand.go` | Remove `ExpandParentCustomer` constant; update `CustomerExpandConfig` |
| `internal/repository/ent/customer.go` | Remove `SetNillableParentCustomerID` from create+update; remove `ParentCustomerIDIn` predicate |
| `internal/service/customer.go` | Remove 4 blocks: CreateCustomer resolve, GetCustomer hydration, GetCustomers bulk-load expand, UpdateCustomer write |
| `internal/testutil/inmemory_customer_store.go` | Remove `ParentCustomerID` from `copyCustomer`; remove fallback in `ListChildrenFromInheritedSubscriptions`; remove `ParentCustomerIDs` filter logic from `customerFilterFn` |
| `internal/service/subscription_test.go` | Remove `ParentCustomerID: &parentCust.ID` line (~line 5777) |
| `internal/service/customer_test.go` | Delete `TestUpdateCustomer_ParentCustomerID` test function |

---

## Task 1: Remove `InvoiceBilling`

**Files:**
- Modify: `internal/types/subscription.go:1-41`
- Modify: `internal/api/dto/subscription.go:264, 593-599, 659-665`
- Modify: `internal/service/subscription.go:100-104`

- [ ] **Step 1: Delete `InvoiceBilling` from `internal/types/subscription.go`**

  Delete lines 10–41 (the `InvoiceBilling` type, 2 constants, `String()` method, `Validate()` method):

  ```go
  // DELETE everything from:
  // InvoiceBilling determines which customer should receive invoices for a subscription
  // type InvoiceBilling string
  // ...through...
  // func (i InvoiceBilling) Validate() error { ... }
  ```

  After deletion, the file starts at `// SubscriptionType categorises a subscription within a customer hierarchy.`

  Do **not** touch the `lo` import — it is used by many other `Validate()` methods further down in the same file (`SubscriptionType`, `SubscriptionStatus`, etc.).

- [ ] **Step 2: Delete `InvoiceBilling` field from `SubscriptionInheritanceConfig` in `internal/api/dto/subscription.go`**

  In `SubscriptionInheritanceConfig` (around line 264), delete:
  ```go
  InvoiceBilling *types.InvoiceBilling `json:"invoice_billing,omitempty"`
  ```

- [ ] **Step 3: Delete 2 validation blocks in `internal/api/dto/subscription.go`**

  Delete the block at ~line 593–599:
  ```go
  // invoice_billing (deprecated) cannot be combined with the new invoicing customer fields
  if r.Inheritance != nil && r.Inheritance.InvoiceBilling != nil &&
  	(r.Inheritance.InvoicingCustomerID != nil || r.Inheritance.InvoicingCustomerExternalID != nil) {
  	return ierr.NewError("invoice_billing cannot be used together with invoicing_customer_id or invoicing_customer_external_id").
  		WithHint("invoice_billing is deprecated; use invoicing_customer_id or invoicing_customer_external_id instead").
  		Mark(ierr.ErrValidation)
  }
  ```

  Delete the block at ~line 659–665:
  ```go
  // Deprecated: invoice_billing is deprecated in favor of invoicing_customer_id / invoicing_customer_external_id.
  // Validate the value if it was explicitly provided for backward compatibility.
  if r.Inheritance != nil && r.Inheritance.InvoiceBilling != nil {
  	if err := r.Inheritance.InvoiceBilling.Validate(); err != nil {
  		return err
  	}
  }
  ```

- [ ] **Step 4: Delete fallback block in `internal/service/subscription.go`**

  At ~line 100–104, inside the `if req.Inheritance != nil` block, delete the inner `else if` branch:
  ```go
  } else if req.Inheritance.InvoicingCustomerID == nil || *req.Inheritance.InvoicingCustomerID == "" {
  	if lo.FromPtr(req.Inheritance.InvoiceBilling) == types.InvoiceBillingInvoiceToParent && customer.ParentCustomerID != nil {
  		req.Inheritance.InvoicingCustomerID = customer.ParentCustomerID
  	}
  }
  ```

  The remaining code block should look like:
  ```go
  if req.Inheritance != nil {
  	if req.Inheritance.InvoicingCustomerExternalID != nil && *req.Inheritance.InvoicingCustomerExternalID != "" {
  		invoicingCustomer, err := s.CustomerRepo.GetByLookupKey(ctx, *req.Inheritance.InvoicingCustomerExternalID)
  		if err != nil {
  			return nil, err
  		}
  		req.Inheritance.InvoicingCustomerID = lo.ToPtr(invoicingCustomer.ID)
  	}
  	if req.Inheritance.InvoicingCustomerID != nil && *req.Inheritance.InvoicingCustomerID != "" {
  		// ... validate invoicing customer
  	}
  ```

- [ ] **Step 5: Build to verify**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Step 6: Run tests**

  ```bash
  go test ./internal/service/... -count=1 -timeout 120s
  ```
  Expected: PASS (the `TestCreditSequentialVsProportionalApplication` test is a known pre-existing flaky test — if it fails, re-run once before investigating).

- [ ] **Step 7: Commit**

  ```bash
  git add internal/types/subscription.go internal/api/dto/subscription.go internal/service/subscription.go
  git commit -m "feat: remove deprecated InvoiceBilling type and field"
  ```

---

## Task 2: Remove `ParentCustomerID` from Customer (all layers)

**Files:**
- Modify: `internal/domain/customer/model.go`
- Modify: `internal/api/dto/customer.go`
- Modify: `internal/types/customer.go`
- Modify: `internal/types/expand.go`
- Modify: `internal/repository/ent/customer.go`
- Modify: `internal/service/customer.go`
- Modify: `internal/testutil/inmemory_customer_store.go`
- Modify: `internal/service/subscription_test.go`
- Modify: `internal/service/customer_test.go`

### Step 1: Domain model

- [ ] **In `internal/domain/customer/model.go`, delete the `ParentCustomerID` field:**

  Delete lines 22–26:
  ```go
  // Deprecated: Customer parent hierarchy is deprecated in favor of subscription-level hierarchy.
  // Retained for backward compatibility; no hierarchy rules are enforced at the service layer.
  // ParentCustomerID is the parent customer identifier for the customer.
  ParentCustomerID *string `db:"parent_customer_id" json:"parent_customer_id"`
  ```

- [ ] **In the same file, delete the `ParentCustomerID` assignment in `FromEnt`:**

  Delete line 66:
  ```go
  ParentCustomerID:  c.ParentCustomerID,
  ```

### Step 2: DTO (`internal/api/dto/customer.go`)

- [ ] **Delete `ParentCustomerID` and `ParentCustomerExternalID` from `CreateCustomerRequest`:**

  Delete lines 67–75:
  ```go
  // Deprecated: Customer parent hierarchy is deprecated in favor of subscription-level hierarchy.
  // This field is accepted for backward compatibility but no hierarchy validations are enforced.
  // parent_customer_id is the internal FlexPrice ID of the parent customer.
  ParentCustomerID *string `json:"parent_customer_id,omitempty"`

  // Deprecated: See ParentCustomerID.
  // parent_customer_external_id is the external ID of the parent customer from your system.
  // Exactly one of parent_customer_id or parent_customer_external_id may be provided.
  ParentCustomerExternalID *string `json:"parent_customer_external_id,omitempty"`
  ```

- [ ] **Delete the same two fields from `UpdateCustomerRequest`:**

  Delete lines 114–123:
  ```go
  // Deprecated: Customer parent hierarchy is deprecated in favor of subscription-level hierarchy.
  // ...
  ParentCustomerID *string `json:"parent_customer_id,omitempty"`

  // Deprecated: See ParentCustomerID.
  // ...
  ParentCustomerExternalID *string `json:"parent_customer_external_id,omitempty"`
  ```

- [ ] **Delete `ParentCustomer` from `CustomerResponse`:**

  Delete line 130:
  ```go
  ParentCustomer *CustomerResponse `json:"parent_customer,omitempty"`
  ```

- [ ] **Delete the parent validation block from `CreateCustomerRequest.Validate()`:**

  Delete lines 164–169:
  ```go
  // Validate parent customer references – only one of ID or external ID can be provided
  if r.ParentCustomerID != nil && r.ParentCustomerExternalID != nil {
  	return ierr.NewError("only one of parent_customer_id or parent_customer_external_id may be provided").
  		WithHint("Send either parent_customer_id or parent_customer_external_id, but not both").
  		Mark(ierr.ErrValidation)
  }
  ```

- [ ] **Delete the parent validation block from `UpdateCustomerRequest.Validate()`:**

  Delete lines 198–203:
  ```go
  // Validate parent customer references – only one of ID or external ID can be provided
  if r.ParentCustomerID != nil && r.ParentCustomerExternalID != nil {
  	return ierr.NewError("only one of parent_customer_id or parent_customer_external_id may be provided").
  		WithHint("Send either parent_customer_id or parent_customer_external_id, but not both").
  		Mark(ierr.ErrValidation)
  }
  ```

- [ ] **Delete `ParentCustomerID` from `ToCustomer()`:**

  Delete line 187:
  ```go
  ParentCustomerID:  r.ParentCustomerID,
  ```

### Step 3: Filter types and expand config

- [ ] **In `internal/types/customer.go`, delete `ParentCustomerIDs` from `CustomerFilter`:**

  Delete line 22:
  ```go
  ParentCustomerIDs []string `json:"parent_customer_ids,omitempty" form:"parent_customer_ids" validate:"omitempty"`
  ```

- [ ] **In `internal/types/expand.go`, remove `ExpandParentCustomer` constant:**

  Delete line 38:
  ```go
  ExpandParentCustomer ExpandableField = "parent_customer"
  ```

- [ ] **In `internal/types/expand.go`, update `CustomerExpandConfig`:**

  Around lines 143–147, replace:
  ```go
  // CustomerExpandConfig defines what can be expanded on a customer
  CustomerExpandConfig = ExpandConfig{
  	AllowedFields: []ExpandableField{ExpandParentCustomer},
  	NestedExpands: map[ExpandableField][]ExpandableField{
  		ExpandParentCustomer: {},
  	},
  }
  ```
  With:
  ```go
  // CustomerExpandConfig defines what can be expanded on a customer
  CustomerExpandConfig = ExpandConfig{
  	AllowedFields: []ExpandableField{},
  	NestedExpands: map[ExpandableField][]ExpandableField{},
  }
  ```

### Step 4: Repository (`internal/repository/ent/customer.go`)

- [ ] **In `Create`, remove `SetNillableParentCustomerID` (line 76):**

  Delete:
  ```go
  SetNillableParentCustomerID(c.ParentCustomerID).
  ```

- [ ] **In `Update`, remove `SetNillableParentCustomerID` (line 431):**

  Delete:
  ```go
  SetNillableParentCustomerID(c.ParentCustomerID).
  ```

- [ ] **In `applyEntityQueryOptions`, remove `ParentCustomerIDIn` predicate block (lines 586–588):**

  Delete:
  ```go
  if len(f.ParentCustomerIDs) > 0 {
  	query = query.Where(customer.ParentCustomerIDIn(f.ParentCustomerIDs...))
  }
  ```

### Step 5: Service (`internal/service/customer.go`)

- [ ] **In `CreateCustomer`, delete the parent resolution block (lines 37–46):**

  Delete:
  ```go
  // Deprecated: Customer parent hierarchy is deprecated in favor of subscription-level hierarchy.
  // Fields are still accepted for backward compatibility; no hierarchy rules are enforced.
  // Resolve parent_customer_external_id to an internal ID if provided.
  if req.ParentCustomerExternalID != nil {
  	parent, err := s.CustomerRepo.GetByLookupKey(ctx, *req.ParentCustomerExternalID)
  	if err != nil {
  		return nil, err
  	}
  	req.ParentCustomerID = lo.ToPtr(parent.ID)
  }
  ```

- [ ] **In `GetCustomer`, delete the parent hydration block (lines 209–215):**

  Delete:
  ```go
  if customer.ParentCustomerID != nil {
  	parentResp, err := s.GetCustomer(ctx, *customer.ParentCustomerID)
  	if err != nil {
  		return nil, err
  	}
  	resp.ParentCustomer = parentResp
  }
  ```

- [ ] **In `GetCustomers`, delete the parent expand block (lines 262–302):**

  Delete the `// Expand parent customers if requested` section and the `// Attach parent customers to response items` section. After deletion, the return statement follows directly after building the `response` slice:

  ```go
  response := make([]*dto.CustomerResponse, 0, len(customers))
  for _, c := range customers {
  	response = append(response, &dto.CustomerResponse{Customer: c})
  }

  if len(response) == 0 {
  	return &dto.ListCustomersResponse{
  		Items:      response,
  		Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
  	}, nil
  }

  return &dto.ListCustomersResponse{
  	Items:      response,
  	Pagination: types.NewPaginationResponse(total, filter.GetLimit(), filter.GetOffset()),
  }, nil
  ```

- [ ] **In `UpdateCustomer`, delete the parent write block (lines 343–352):**

  Delete:
  ```go
  // Deprecated: Customer parent hierarchy is deprecated in favor of subscription-level hierarchy.
  // The parent_customer_id field is accepted for backward compatibility without enforcing hierarchy rules.
  if req.ParentCustomerID != nil {
  	newParentID := strings.TrimSpace(*req.ParentCustomerID)
  	if newParentID == "" {
  		cust.ParentCustomerID = nil
  	} else {
  		cust.ParentCustomerID = lo.ToPtr(newParentID)
  	}
  }
  ```

- [ ] **In `internal/service/customer.go`, remove the `"strings"` import:**

  The `strings.TrimSpace` call inside the deleted block is the **only** use of `"strings"` in this file. After deleting that block, remove `"strings"` from the import block to avoid a compilation error:

  ```go
  // DELETE this import line:
  "strings"
  ```

  `lo` is still used elsewhere in the file — keep it. All other imports are still used — keep them.

### Step 6: Test utility (`internal/testutil/inmemory_customer_store.go`)

- [ ] **In `copyCustomer`, delete the `ParentCustomerID` assignment (line 52):**

  Delete:
  ```go
  ParentCustomerID:  c.ParentCustomerID,
  ```

- [ ] **In `ListChildrenFromInheritedSubscriptions`, delete the backward-compatible fallback (lines 123–129):**

  Delete the `if s.subscriptionStore == nil` block:
  ```go
  if s.subscriptionStore == nil {
  	// Backward-compatible fallback if the store is not wired by a custom test.
  	filter := types.NewNoLimitCustomerFilter()
  	filter.ParentCustomerIDs = []string{parentCustomerID}
  	return s.List(ctx, filter)
  }
  ```

  Replace it with a simple nil-guard that returns empty instead of the deprecated fallback:
  ```go
  if s.subscriptionStore == nil {
  	return []*customer.Customer{}, nil
  }
  ```

- [ ] **In `customerFilterFn`, delete the `ParentCustomerIDs` filter block (lines 224–232):**

  Delete:
  ```go
  // Apply parent customer ID filter
  if len(f.ParentCustomerIDs) > 0 {
  	if c.ParentCustomerID == nil {
  		return false
  	}
  	if !lo.Contains(f.ParentCustomerIDs, *c.ParentCustomerID) {
  		return false
  	}
  }
  ```

  Check if `lo` is still used in this file after this deletion — it is (used in `lo.Map` and `lo.Contains` for other filters and the `lo.Assign` in `copyCustomer`). Keep the import.

### Step 7: Fix tests

- [ ] **In `internal/service/subscription_test.go`, remove `ParentCustomerID` assignment (~line 5777):**

  In the helper that creates child customers, delete:
  ```go
  ParentCustomerID: &parentCust.ID,
  ```

- [ ] **In `internal/service/customer_test.go`, delete the entire `TestUpdateCustomer_ParentCustomerID` test function (~lines 354–421):**

  Delete the entire function:
  ```go
  func (s *CustomerServiceSuite) TestUpdateCustomer_ParentCustomerID() {
      // ... entire function body ...
  }
  ```

  This test covers behavior that no longer exists — updating `parent_customer_id` on a customer.

### Step 8: Build and verify

- [ ] **Run build:**

  ```bash
  go build ./...
  ```
  Expected: no errors.

- [ ] **Run vet:**

  ```bash
  go vet ./...
  ```
  Expected: no warnings.

- [ ] **Run tests:**

  ```bash
  go test ./internal/... -count=1 -timeout 180s
  ```
  Expected: PASS. Note: `TestCreditSequentialVsProportionalApplication` is a known pre-existing flaky test — re-run once if it fails before investigating.

- [ ] **Commit:**

  ```bash
  git add \
    internal/domain/customer/model.go \
    internal/api/dto/customer.go \
    internal/types/customer.go \
    internal/types/expand.go \
    internal/repository/ent/customer.go \
    internal/service/customer.go \
    internal/testutil/inmemory_customer_store.go \
    internal/service/subscription_test.go \
    internal/service/customer_test.go
  git commit -m "feat: remove deprecated ParentCustomerID from customer domain, DTO, repo, service"
  ```
