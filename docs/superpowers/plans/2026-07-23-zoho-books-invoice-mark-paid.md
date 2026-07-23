# Zoho Books Invoice Mark-Paid Sync — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a FlexPrice invoice that was synced to Zoho Books becomes fully paid, automatically record a payment against the corresponding Zoho invoice so it shows as Paid in Zoho too.

**Architecture:** Mirrors the existing Whop mark-paid flow (`internal/temporal/workflows/whop_invoice_sync_workflow.go:WhopInvoiceMarkPaidWorkflow`, dispatched from `internal/ee/service/payment_processor.go:dispatchWhopMarkPaid`). A new Temporal workflow (`ZohoBooksInvoiceMarkPaidWorkflow`) runs a new activity (`MarkZohoBooksInvoicePaid`) that calls a new `zoho.InvoiceService.MarkInvoicePaidInZoho` method. That method looks up the invoice's Zoho mapping, reads the live Zoho invoice balance (Zoho's total is intentionally tax-inclusive and can exceed FlexPrice's pre-tax total — see the design spec), and if positive, records a Zoho customer payment for that full balance. Zero balance is treated as already-paid and skipped — this makes the flow naturally idempotent with no new FlexPrice-side bookkeeping.

**Tech Stack:** Go 1.23+, Temporal (workflow/activity), Zoho Books REST API (`/books/v3/invoices/{id}` GET, `/books/v3/customerpayments` POST).

**Design doc:** `docs/superpowers/specs/2026-07-23-zoho-books-invoice-mark-paid-design.md`

---

### Task 1: Workflow input model

**Files:**
- Create: `internal/temporal/models/zoho_books_invoice_mark_paid.go`

- [ ] **Step 1: Write the file**

```go
package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// ZohoBooksInvoiceMarkPaidWorkflowInput contains the input for the Zoho Books mark-paid workflow
type ZohoBooksInvoiceMarkPaidWorkflowInput struct {
	InvoiceID     string `json:"invoice_id"`
	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`
}

func (input *ZohoBooksInvoiceMarkPaidWorkflowInput) Validate() error {
	if input.InvoiceID == "" {
		return ierr.NewError("invoice_id is required").
			WithHint("InvoiceID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return ierr.NewError("tenant_id is required").
			WithHint("TenantID must not be empty").
			Mark(ierr.ErrValidation)
	}
	if input.EnvironmentID == "" {
		return ierr.NewError("environment_id is required").
			WithHint("EnvironmentID must not be empty").
			Mark(ierr.ErrValidation)
	}
	return nil
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/temporal/models/...`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/models/zoho_books_invoice_mark_paid.go
git commit -m "feat(zoho): add mark-paid workflow input model"
```

---

### Task 2: Temporal workflow

**Files:**
- Modify: `internal/temporal/workflows/zoho_books_invoice_sync_workflow.go`

- [ ] **Step 1: Add the new consts and workflow function**

Replace the full file content with:

```go
package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowZohoBooksInvoiceSync     = "ZohoBooksInvoiceSyncWorkflow"
	ActivitySyncInvoiceToZoho        = "SyncInvoiceToZoho"
	WorkflowZohoBooksInvoiceMarkPaid = "ZohoBooksInvoiceMarkPaidWorkflow"
	ActivityMarkZohoBooksInvoicePaid = "MarkZohoBooksInvoicePaid"
)

// ZohoBooksInvoiceSyncWorkflow syncs finalized invoices to Zoho Books.
func ZohoBooksInvoiceSyncWorkflow(ctx workflow.Context, input models.ZohoBooksInvoiceSyncWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	if err := workflow.Sleep(ctx, 5*time.Second); err != nil {
		return err
	}
	return workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToZoho, input).Get(ctx, nil)
}

// ZohoBooksInvoiceMarkPaidWorkflow marks the corresponding Zoho Books invoice as paid
// when FlexPrice marks the invoice paid.
func ZohoBooksInvoiceMarkPaidWorkflow(ctx workflow.Context, input models.ZohoBooksInvoiceMarkPaidWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Zoho Books mark-paid workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, opts)

	if err := workflow.ExecuteActivity(ctx, ActivityMarkZohoBooksInvoicePaid, input).Get(ctx, nil); err != nil {
		logger.Error("Failed to mark Zoho Books invoice as paid", "error", err, "invoice_id", input.InvoiceID)
		return err
	}

	logger.Info("Successfully marked Zoho Books invoice as paid", "invoice_id", input.InvoiceID)
	return nil
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/temporal/workflows/...`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/workflows/zoho_books_invoice_sync_workflow.go
git commit -m "feat(zoho): add ZohoBooksInvoiceMarkPaidWorkflow"
```

---

### Task 3: Zoho DTOs for invoice GET and customer payment

**Files:**
- Modify: `internal/integration/zoho/dto.go:116-121` (the `InvoiceResponse` struct)

- [ ] **Step 1: Extend `InvoiceResponse` and add the customer-payment DTOs**

Replace:

```go
type InvoiceResponse struct {
	InvoiceID     string          `json:"invoice_id"`
	InvoiceNumber string          `json:"invoice_number"`
	Status        string          `json:"status,omitempty"`
	Total         decimal.Decimal `json:"total,omitempty"`
}
```

with:

```go
type InvoiceResponse struct {
	InvoiceID     string          `json:"invoice_id"`
	InvoiceNumber string          `json:"invoice_number"`
	Status        string          `json:"status,omitempty"`
	Total         decimal.Decimal `json:"total,omitempty"`
	CustomerID    string          `json:"customer_id,omitempty"`
	Balance       decimal.Decimal `json:"balance,omitempty"`
}

// CustomerPaymentInvoiceApply links a recorded Zoho customer payment to an invoice it settles.
type CustomerPaymentInvoiceApply struct {
	InvoiceID     string          `json:"invoice_id"`
	AmountApplied decimal.Decimal `json:"amount_applied"`
}

// CustomerPaymentCreateRequest is the body for POST /books/v3/customerpayments.
type CustomerPaymentCreateRequest struct {
	CustomerID  string                         `json:"customer_id"`
	PaymentMode string                         `json:"payment_mode"`
	Amount      decimal.Decimal                `json:"amount"`
	Date        string                         `json:"date"`
	Invoices    []CustomerPaymentInvoiceApply  `json:"invoices"`
}

// CustomerPaymentResponse is the response from POST /books/v3/customerpayments.
type CustomerPaymentResponse struct {
	PaymentID string `json:"payment_id"`
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/integration/zoho/...`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/integration/zoho/dto.go
git commit -m "feat(zoho): add DTOs for invoice GET and customer payment create"
```

---

### Task 4: Zoho client methods (GetInvoice, CreateCustomerPayment)

**Files:**
- Modify: `internal/integration/zoho/client.go:22-41` (the `ZohoClient` interface)
- Modify: `internal/integration/zoho/client.go:119-127` (add new methods after `CreateInvoice`)

- [ ] **Step 1: Add the two new methods to the `ZohoClient` interface**

In the interface block (`internal/integration/zoho/client.go:22-41`), add after the `CreateInvoice` line:

```go
	CreateInvoice(ctx context.Context, req *InvoiceCreateRequest) (*InvoiceResponse, error)
	// GetInvoice fetches a Zoho Books invoice by ID (used to read the live balance/customer_id before mark-paid).
	GetInvoice(ctx context.Context, zohoInvoiceID string) (*InvoiceResponse, error)
	// CreateCustomerPayment records a payment against one or more Zoho Books invoices.
	CreateCustomerPayment(ctx context.Context, req *CustomerPaymentCreateRequest) (*CustomerPaymentResponse, error)
```

- [ ] **Step 2: Implement both methods on `*Client`**

Add immediately after the existing `CreateInvoice` method (`internal/integration/zoho/client.go:119-127`):

```go
func (c *Client) GetInvoice(ctx context.Context, zohoInvoiceID string) (*InvoiceResponse, error) {
	var resp struct {
		Invoice InvoiceResponse `json:"invoice"`
	}
	path := fmt.Sprintf("/books/v3/invoices/%s", zohoInvoiceID)
	if err := c.doBooksRequest(ctx, http.MethodGet, path, nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Invoice, nil
}

func (c *Client) CreateCustomerPayment(ctx context.Context, req *CustomerPaymentCreateRequest) (*CustomerPaymentResponse, error) {
	var resp struct {
		Payment CustomerPaymentResponse `json:"payment"`
	}
	if err := c.doBooksRequest(ctx, http.MethodPost, "/books/v3/customerpayments", nil, req, &resp); err != nil {
		return nil, err
	}
	return &resp.Payment, nil
}
```

`fmt` and `http` are already imported in this file (used elsewhere), no new imports needed.

- [ ] **Step 3: Build to verify it compiles**

Run: `go build ./internal/integration/zoho/...`
Expected: no output (success)

- [ ] **Step 4: Commit**

```bash
git add internal/integration/zoho/client.go
git commit -m "feat(zoho): add GetInvoice and CreateCustomerPayment client methods"
```

---

### Task 5: `MarkInvoicePaidInZoho` service method — failing tests first

**Files:**
- Modify: `internal/integration/zoho/invoice.go:18-20` (the `ZohoInvoiceService` interface)
- Create: `internal/integration/zoho/invoice_mark_paid_test.go`

This task writes the tests against methods that don't exist yet (`MarkInvoicePaidInZoho`, and the fakes' `GetInvoice`/`CreateCustomerPayment`/`List` behavior), so it will fail to compile until Task 6 adds the real implementation. That's expected — TDD red state.

- [ ] **Step 1: Add `MarkInvoicePaidInZoho` to the `ZohoInvoiceService` interface**

In `internal/integration/zoho/invoice.go`, change:

```go
type ZohoInvoiceService interface {
	SyncInvoiceToZoho(ctx context.Context, req ZohoInvoiceSyncRequest) (*ZohoInvoiceSyncResponse, error)
}
```

to:

```go
type ZohoInvoiceService interface {
	SyncInvoiceToZoho(ctx context.Context, req ZohoInvoiceSyncRequest) (*ZohoInvoiceSyncResponse, error)
	// MarkInvoicePaidInZoho records a Zoho customer payment for the invoice's current
	// outstanding balance, bringing it to Paid. No-op if the invoice was never synced to
	// Zoho, or if Zoho's balance is already zero.
	MarkInvoicePaidInZoho(ctx context.Context, flexpriceInvoiceID string) error
}
```

- [ ] **Step 2: Write the test file with hand-rolled fakes**

```go
package zoho

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMappingRepo is a minimal in-memory entityintegrationmapping.Repository for these tests.
type fakeMappingRepo struct {
	entityintegrationmapping.Repository
	mappings []*entityintegrationmapping.EntityIntegrationMapping
}

func (f *fakeMappingRepo) List(_ context.Context, filter *types.EntityIntegrationMappingFilter) ([]*entityintegrationmapping.EntityIntegrationMapping, error) {
	var out []*entityintegrationmapping.EntityIntegrationMapping
	for _, m := range f.mappings {
		if filter.EntityID != "" && m.EntityID != filter.EntityID {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// fakeZohoClient is a minimal ZohoClient for these tests. Only GetInvoice and
// CreateCustomerPayment are exercised; all other methods panic if called.
type fakeZohoClient struct {
	ZohoClient
	getInvoiceResp    *InvoiceResponse
	getInvoiceErr     error
	createPaymentReq  *CustomerPaymentCreateRequest
	createPaymentErr  error
	createPaymentCalls int
}

func (f *fakeZohoClient) GetInvoice(_ context.Context, _ string) (*InvoiceResponse, error) {
	return f.getInvoiceResp, f.getInvoiceErr
}

func (f *fakeZohoClient) CreateCustomerPayment(_ context.Context, req *CustomerPaymentCreateRequest) (*CustomerPaymentResponse, error) {
	f.createPaymentCalls++
	f.createPaymentReq = req
	if f.createPaymentErr != nil {
		return nil, f.createPaymentErr
	}
	return &CustomerPaymentResponse{PaymentID: "zoho_payment_1"}, nil
}

func newTestInvoiceService(client ZohoClient, mappingRepo entityintegrationmapping.Repository) *InvoiceService {
	return &InvoiceService{
		client:      client,
		mappingRepo: mappingRepo,
		logger:      logger.NewNoopLogger(),
	}
}

func TestMarkInvoicePaidInZoho_NoMapping_Skips(t *testing.T) {
	client := &fakeZohoClient{}
	mappingRepo := &fakeMappingRepo{}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	assert.Equal(t, 0, client.createPaymentCalls)
}

func TestMarkInvoicePaidInZoho_ZeroBalance_Skips(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Balance:    decimal.Zero,
		},
	}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	assert.Equal(t, 0, client.createPaymentCalls)
}

func TestMarkInvoicePaidInZoho_PositiveBalance_RecordsFullBalance(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Balance:    decimal.NewFromInt(160),
		},
	}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	require.NoError(t, err)
	require.Equal(t, 1, client.createPaymentCalls)
	req := client.createPaymentReq
	assert.Equal(t, "zoho_cust_1", req.CustomerID)
	assert.True(t, decimal.NewFromInt(160).Equal(req.Amount))
	assert.Equal(t, "other", req.PaymentMode)
	require.Len(t, req.Invoices, 1)
	assert.Equal(t, "zoho_inv_1", req.Invoices[0].InvoiceID)
	assert.True(t, decimal.NewFromInt(160).Equal(req.Invoices[0].AmountApplied))
}

func TestMarkInvoicePaidInZoho_CreatePaymentError_Propagates(t *testing.T) {
	client := &fakeZohoClient{
		getInvoiceResp: &InvoiceResponse{
			InvoiceID:  "zoho_inv_1",
			CustomerID: "zoho_cust_1",
			Balance:    decimal.NewFromInt(50),
		},
		createPaymentErr: assert.AnError,
	}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	assert.Error(t, err)
}

func TestMarkInvoicePaidInZoho_GetInvoiceError_Propagates(t *testing.T) {
	client := &fakeZohoClient{getInvoiceErr: assert.AnError}
	mappingRepo := &fakeMappingRepo{
		mappings: []*entityintegrationmapping.EntityIntegrationMapping{
			{EntityID: "inv_1", ProviderEntityID: "zoho_inv_1"},
		},
	}
	svc := newTestInvoiceService(client, mappingRepo)

	err := svc.MarkInvoicePaidInZoho(context.Background(), "inv_1")

	assert.Error(t, err)
	assert.Equal(t, 0, client.createPaymentCalls)
}
```

- [ ] **Step 3: Run the tests to verify they fail to compile**

Run: `go test ./internal/integration/zoho/... -run TestMarkInvoicePaidInZoho -v`
Expected: FAIL — compile error, `svc.MarkInvoicePaidInZoho undefined (type *InvoiceService has no field or method MarkInvoicePaidInZoho)`

- [ ] **Step 4: Commit**

```bash
git add internal/integration/zoho/invoice.go internal/integration/zoho/invoice_mark_paid_test.go
git commit -m "test(zoho): add failing tests for MarkInvoicePaidInZoho"
```

---

### Task 6: `MarkInvoicePaidInZoho` implementation — make tests pass

**Files:**
- Modify: `internal/integration/zoho/invoice.go` (add method, add `time` import if not already present — it already is, used by `SyncInvoiceToZoho`)

- [ ] **Step 1: Add the implementation**

Add this method to `internal/integration/zoho/invoice.go`, right after the existing `SyncInvoiceToZoho` method (after its closing `}` around line 166):

```go
// MarkInvoicePaidInZoho records a Zoho customer payment for the invoice's current
// outstanding balance, bringing it to Paid.
//
// Zoho's invoice total is intentionally tax-inclusive (items are synced as taxable —
// see buildLineItems/EnsureItemsMapped) while FlexPrice's invoice.Total is not, so the
// two totals routinely differ. Rather than track how much FlexPrice has separately
// pushed to Zoho, this reads Zoho's own live balance and pays it off in full: a zero
// balance means Zoho already considers the invoice settled (whether from a prior call
// here, or from the existing inbound "Zoho invoice paid" webhook), so this is a no-op.
func (s *InvoiceService) MarkInvoicePaidInZoho(ctx context.Context, flexpriceInvoiceID string) error {
	filter := types.NewEntityIntegrationMappingFilter()
	filter.EntityType = types.IntegrationEntityTypeInvoice
	filter.EntityID = flexpriceInvoiceID
	filter.ProviderTypes = []string{string(types.SecretProviderZohoBooks)}
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	mappings, err := s.mappingRepo.List(ctx, filter)
	if err != nil {
		return err
	}
	if len(mappings) == 0 {
		s.logger.Info(ctx, "no Zoho mapping for invoice, skipping mark-paid",
			"invoice_id", flexpriceInvoiceID)
		return nil
	}
	zohoInvoiceID := mappings[0].ProviderEntityID

	zohoInv, err := s.client.GetInvoice(ctx, zohoInvoiceID)
	if err != nil {
		return err
	}
	if zohoInv == nil || !zohoInv.Balance.IsPositive() {
		s.logger.Info(ctx, "Zoho invoice already has zero balance, skipping mark-paid",
			"invoice_id", flexpriceInvoiceID,
			"zoho_invoice_id", zohoInvoiceID)
		return nil
	}

	_, err = s.client.CreateCustomerPayment(ctx, &CustomerPaymentCreateRequest{
		CustomerID:  zohoInv.CustomerID,
		PaymentMode: "other",
		Amount:      zohoInv.Balance,
		Date:        time.Now().UTC().Format("2006-01-02"),
		Invoices: []CustomerPaymentInvoiceApply{
			{InvoiceID: zohoInvoiceID, AmountApplied: zohoInv.Balance},
		},
	})
	if err != nil {
		return err
	}

	s.logger.Info(ctx, "marked Zoho invoice as paid",
		"invoice_id", flexpriceInvoiceID,
		"zoho_invoice_id", zohoInvoiceID,
		"amount", zohoInv.Balance.String())
	return nil
}
```

- [ ] **Step 2: Run the tests to verify they pass**

Run: `go test ./internal/integration/zoho/... -run TestMarkInvoicePaidInZoho -v`
Expected: PASS — all 5 tests pass

- [ ] **Step 3: Run the full zoho package test suite to check nothing else broke**

Run: `go test ./internal/integration/zoho/...`
Expected: PASS (this package has no other test files besides `webhook/verify_test.go`, which is unaffected)

- [ ] **Step 4: Commit**

```bash
git add internal/integration/zoho/invoice.go
git commit -m "feat(zoho): implement MarkInvoicePaidInZoho"
```

---

### Task 7: Temporal activity

**Files:**
- Modify: `internal/temporal/activities/zoho/invoice_sync_activities.go`

- [ ] **Step 1: Add the activity method**

Add to `internal/temporal/activities/zoho/invoice_sync_activities.go`, right after the existing `SyncInvoiceToZoho` activity method:

```go
func (a *InvoiceSyncActivities) MarkZohoBooksInvoicePaid(ctx context.Context, input models.ZohoBooksInvoiceMarkPaidWorkflowInput) error {
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	zohoIntegration, err := a.integrationFactory.GetZohoBooksIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError("Zoho Books connection not configured", "ConnectionNotFound", err)
		}
		return err
	}

	return zohoIntegration.InvoiceSvc.MarkInvoicePaidInZoho(ctx, input.InvoiceID)
}
```

No new imports needed — `ierr`, `temporal`, `types`, and `models` are already imported in this file (used by `SyncInvoiceToZoho`).

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/temporal/activities/zoho/...`
Expected: no output (success)

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/activities/zoho/invoice_sync_activities.go
git commit -m "feat(zoho): add MarkZohoBooksInvoicePaid activity"
```

---

### Task 8: Register the workflow type in `internal/types/temporal.go`

**Files:**
- Modify: `internal/types/temporal.go` (4 separate edits, all additive)

- [ ] **Step 1: Add the `TemporalWorkflowType` constant**

At `internal/types/temporal.go:101`, immediately after the `TemporalZohoBooksInvoiceSyncWorkflow` line, add:

```go
	TemporalZohoBooksInvoiceSyncWorkflow               TemporalWorkflowType = "ZohoBooksInvoiceSyncWorkflow"
	TemporalZohoBooksInvoiceMarkPaidWorkflow           TemporalWorkflowType = "ZohoBooksInvoiceMarkPaidWorkflow"
```

- [ ] **Step 2: Add it to `Validate()`'s `allowedWorkflows` list**

At `internal/types/temporal.go:191`, immediately after `TemporalZohoBooksInvoiceSyncWorkflow,`, add:

```go
		TemporalZohoBooksInvoiceSyncWorkflow,
		TemporalZohoBooksInvoiceMarkPaidWorkflow,
```

- [ ] **Step 3: Add it to the `TaskQueue()` switch case**

At `internal/types/temporal.go:212`, the long `case` line lists every task-queue-Task workflow type comma-separated. Add `TemporalZohoBooksInvoiceMarkPaidWorkflow` immediately after `TemporalZohoBooksInvoiceSyncWorkflow` in that list (so `..., TemporalQuickBooksInvoiceSyncWorkflow, TemporalZohoBooksInvoiceSyncWorkflow, TemporalZohoBooksInvoiceMarkPaidWorkflow, TemporalTabsInvoiceSyncWorkflow, ...`).

- [ ] **Step 4: Add it to `GetWorkflowsForTaskQueue`'s `TemporalTaskQueueTask` list**

At `internal/types/temporal.go:267`, immediately after `TemporalZohoBooksInvoiceSyncWorkflow,`, add:

```go
			TemporalZohoBooksInvoiceSyncWorkflow,
			TemporalZohoBooksInvoiceMarkPaidWorkflow,
```

- [ ] **Step 5: Add it to `extractWorkflowContextID`'s vendor-invoice-sync switch** (for a readable deterministic workflow ID, purely cosmetic/observability — see Task 9 for where this lives)

This step is folded into Task 9 since it's in `internal/temporal/service/service.go`, not this file. Skip here.

- [ ] **Step 6: Build to verify it compiles**

Run: `go build ./internal/types/...`
Expected: no output (success)

- [ ] **Step 7: Commit**

```bash
git add internal/types/temporal.go
git commit -m "feat(zoho): register TemporalZohoBooksInvoiceMarkPaidWorkflow type"
```

---

### Task 9: Wire the workflow into `internal/temporal/service/service.go`

**Files:**
- Modify: `internal/temporal/service/service.go` (3 edits)

- [ ] **Step 1: Add a deterministic-workflow-ID case (readability only, matches Whop precedent)**

At `internal/temporal/service/service.go:492-495`, immediately after the `TemporalWhopInvoiceMarkPaidWorkflow` case, add:

```go
	case types.TemporalWhopInvoiceMarkPaidWorkflow:
		if input, ok := params.(models.WhopInvoiceMarkPaidWorkflowInput); ok {
			return input.InvoiceID
		}
	case types.TemporalZohoBooksInvoiceMarkPaidWorkflow:
		if input, ok := params.(models.ZohoBooksInvoiceMarkPaidWorkflowInput); ok {
			return input.InvoiceID
		}
```

- [ ] **Step 2: Add the `buildWorkflowInput` dispatch case**

At `internal/temporal/service/service.go:633-634`, immediately after the `TemporalZohoBooksInvoiceSyncWorkflow` case, add:

```go
	case types.TemporalZohoBooksInvoiceSyncWorkflow:
		return s.buildZohoBooksInvoiceSyncInput(ctx, tenantID, environmentID, params)
	case types.TemporalZohoBooksInvoiceMarkPaidWorkflow:
		return s.buildZohoBooksInvoiceMarkPaidInput(ctx, tenantID, environmentID, params)
```

- [ ] **Step 3: Add the `buildZohoBooksInvoiceMarkPaidInput` builder function**

At `internal/temporal/service/service.go:1054`, immediately after the closing `}` of `buildZohoBooksInvoiceSyncInput`, add:

```go
func (s *temporalService) buildZohoBooksInvoiceMarkPaidInput(_ context.Context, tenantID, environmentID string, params interface{}) (interface{}, error) {
	if input, ok := params.(*models.ZohoBooksInvoiceMarkPaidWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return *input, nil
	}

	if input, ok := params.(models.ZohoBooksInvoiceMarkPaidWorkflowInput); ok {
		input.TenantID = tenantID
		input.EnvironmentID = environmentID
		return input, nil
	}

	return nil, errors.NewError("invalid input for Zoho Books mark-paid workflow").
		WithHint("Provide ZohoBooksInvoiceMarkPaidWorkflowInput with invoice_id").
		Mark(errors.ErrValidation)
}
```

- [ ] **Step 4: Build to verify it compiles**

Run: `go build ./internal/temporal/service/...`
Expected: no output (success)

- [ ] **Step 5: Commit**

```bash
git add internal/temporal/service/service.go
git commit -m "feat(zoho): wire ZohoBooksInvoiceMarkPaidWorkflow into temporal service"
```

---

### Task 10: Register the workflow and activity for the worker

**Files:**
- Modify: `internal/temporal/registration.go` (2 edits)

- [ ] **Step 1: Add the workflow to `workflowsList`**

At `internal/temporal/registration.go:392`, immediately after `workflows.ZohoBooksInvoiceSyncWorkflow,`, add:

```go
			workflows.ZohoBooksInvoiceSyncWorkflow,
			workflows.ZohoBooksInvoiceMarkPaidWorkflow,
```

- [ ] **Step 2: Add the activity to `activitiesList`**

At `internal/temporal/registration.go:419`, immediately after `zohoInvoiceSyncActivities.SyncInvoiceToZoho,`, add:

```go
		zohoInvoiceSyncActivities.SyncInvoiceToZoho,
		zohoInvoiceSyncActivities.MarkZohoBooksInvoicePaid,
```

(`zohoInvoiceSyncActivities` is already a parameter of `buildWorkerConfig` and already passed through at the call site — no signature changes needed.)

- [ ] **Step 3: Build to verify it compiles**

Run: `go build ./internal/temporal/...`
Expected: no output (success)

- [ ] **Step 4: Commit**

```bash
git add internal/temporal/registration.go
git commit -m "feat(zoho): register mark-paid workflow and activity with the worker"
```

---

### Task 11: Dispatch from the payment processor

**Files:**
- Modify: `internal/ee/service/payment_processor.go:740-792`

- [ ] **Step 1: Add the `dispatchZohoMarkPaid` method**

Immediately after the existing `dispatchWhopMarkPaid` method (`internal/ee/service/payment_processor.go:777-792`), add:

```go
func (p *paymentProcessor) dispatchZohoMarkPaid(ctx context.Context, invoiceID string) {
	temporalSvc := temporalservice.GetGlobalTemporalService()
	if temporalSvc == nil {
		p.Logger.Info(ctx, "temporal service unavailable, skipping Zoho mark-paid", "invoice_id", invoiceID)
		return
	}

	input := temporalmodels.ZohoBooksInvoiceMarkPaidWorkflowInput{
		InvoiceID:     invoiceID,
		TenantID:      types.GetTenantID(ctx),
		EnvironmentID: types.GetEnvironmentID(ctx),
	}
	if _, err := temporalSvc.ExecuteWorkflow(ctx, types.TemporalZohoBooksInvoiceMarkPaidWorkflow, input); err != nil {
		p.Logger.Error(ctx, "failed to start Zoho mark-paid workflow", "error", err, "invoice_id", invoiceID)
	}
}
```

- [ ] **Step 2: Call it alongside the Whop dispatch**

At `internal/ee/service/payment_processor.go:740-743`, change:

```go
	// Invoice is fully paid — dispatch Whop mark-paid directly if a mapping exists.
	if invoice.PaymentStatus == types.PaymentStatusSucceeded {
		p.dispatchWhopMarkPaid(ctx, invoice.ID)
	}
```

to:

```go
	// Invoice is fully paid — dispatch Whop and Zoho mark-paid directly if a mapping exists.
	if invoice.PaymentStatus == types.PaymentStatusSucceeded {
		p.dispatchWhopMarkPaid(ctx, invoice.ID)
		p.dispatchZohoMarkPaid(ctx, invoice.ID)
	}
```

- [ ] **Step 3: Build to verify it compiles**

Run: `go build ./internal/ee/service/...`
Expected: no output (success)

- [ ] **Step 4: Commit**

```bash
git add internal/ee/service/payment_processor.go
git commit -m "feat(zoho): dispatch mark-paid workflow when a Zoho-synced invoice is fully paid"
```

---

### Task 12: Full build and test pass

**Files:** None (verification-only task).

- [ ] **Step 1: Build the whole module**

Run: `go build ./...`
Expected: no output (success)

- [ ] **Step 2: Vet the whole module**

Run: `go vet ./...`
Expected: no output (success)

- [ ] **Step 3: Run the full test suite**

Run: `make test`
Expected: all tests pass, including the new `TestMarkInvoicePaidInZoho_*` tests in `internal/integration/zoho/invoice_mark_paid_test.go`

- [ ] **Step 4: Format check**

Run: `gofmt -l .`
Expected: no output (no files need formatting; if any are listed, run `gofmt -w .` and re-check)

No commit for this task — it's verification only. If Step 4 finds formatting issues, fix and fold into a follow-up `chore: gofmt` commit.

---

## Manual / follow-up verification (not part of this plan's automated tests)

This plan's tests cover `MarkInvoicePaidInZoho`'s business logic with fakes. It does **not** include a live end-to-end test against a real Zoho Books sandbox (no test infrastructure for that exists in this repo today — see the design spec's Testing section). Before considering this feature production-ready, manually verify against a Zoho Books sandbox connection:

1. Sync a FlexPrice invoice to Zoho (existing flow) so a draft invoice with tax exists in Zoho.
2. Fully pay that invoice in FlexPrice (e.g. via a test payment).
3. Confirm in the Zoho Books UI that the invoice now shows status "Paid" and a customer payment was recorded for the tax-inclusive balance.
4. Trigger the existing inbound flow (Zoho webhook says paid) on a *different* invoice and confirm `dispatchZohoMarkPaid` doesn't error or double-post (Zoho balance should already be 0 by the time the outbound activity runs, so `MarkInvoicePaidInZoho` should no-op).
