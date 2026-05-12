# Progressive Billing (Auto-Invoice Threshold) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a 5-minute cron job that automatically creates invoices when a subscription's current-period usage dollar amount crosses a configured threshold, then advances `current_period_start` to now.

**Architecture:** `auto_invoice_threshold` is added as a nullable decimal to both `plan` and `subscription` ent schemas. The subscription value overrides the plan value; `NULL` on both means disabled. A new Temporal cron workflow (`ThresholdBillingWorkflow`) runs every 5 minutes, calling a single activity that batches through all subscriptions where an effective threshold exists, checks usage, and — if crossed — creates a Draft → Compute → Finalize invoice before advancing `current_period_start`.

**Tech Stack:** Go 1.23, Ent ORM, Temporal, PostgreSQL, `shopspring/decimal`, `samber/lo`

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `ent/schema/plan.go` | Add `auto_invoice_threshold` field |
| Modify | `ent/schema/subscription.go` | Add `auto_invoice_threshold` field |
| Run | `make generate-ent` | Regenerate ent client + predicates |
| Run | `make generate-migration` | Produce migration SQL |
| Modify | `internal/domain/plan/model.go` | Add field + update `FromEnt` |
| Modify | `internal/domain/subscription/model.go` | Add field + update `GetSubscriptionFromEnt` |
| Modify | `internal/domain/subscription/repository.go` | Add `GetSubscriptionsWithAutoInvoiceThreshold` to interface |
| Modify | `internal/repository/ent/subscription.go` | Implement `GetSubscriptionsWithAutoInvoiceThreshold` |
| Modify | `internal/types/invoice.go` | Add `InvoiceBillingReasonThresholdBilling` constant |
| Modify | `internal/interfaces/service.go` | Add `FinalizeInvoice` to `InvoiceService`; add `ProcessThresholdBilling` to `SubscriptionService` |
| Modify | `internal/api/dto/subscription.go` | Add `ThresholdBillingResult` struct |
| Modify | `internal/service/subscription.go` | Implement `ProcessThresholdBilling` |
| Modify | `internal/temporal/models/cron.go` | Add `ThresholdBillingWorkflowInput/Result` structs |
| Modify | `internal/temporal/activities/cron/subscription_activities.go` | Add `ProcessThresholdBillingActivity` |
| Create | `internal/temporal/workflows/cron/threshold_billing_workflow.go` | New cron workflow |
| Modify | `internal/temporal/registration.go` | Register workflow + activity |

---

### Task 1: Add `auto_invoice_threshold` to Ent Schemas

**Files:**
- Modify: `ent/schema/plan.go`
- Modify: `ent/schema/subscription.go`

- [ ] **Step 1: Add field to plan schema**

Open `ent/schema/plan.go`. In the `Fields()` function, add after the existing `field.Int("display_order")` entry:

```go
field.Other("auto_invoice_threshold", decimal.Decimal{}).
    Optional().
    Nillable().
    SchemaType(map[string]string{
        "postgres": "decimal(20,6)",
    }),
```

Add the import `"github.com/shopspring/decimal"` to the file's imports block.

- [ ] **Step 2: Add field to subscription schema**

Open `ent/schema/subscription.go`. In the `Fields()` function, add at the end of the field list (before the closing `}`):

```go
field.Other("auto_invoice_threshold", decimal.Decimal{}).
    Optional().
    Nillable().
    SchemaType(map[string]string{
        "postgres": "decimal(20,6)",
    }).
    Comment("Threshold usage amount (in subscription currency) that triggers an intermediate invoice. Overrides plan-level threshold when set."),
```

- [ ] **Step 3: Regenerate ent code**

```bash
make generate-ent
```

Expected: no errors, updated files in `ent/` directory (including `ent/subscription.go`, `ent/plan.go`, `ent/mutation.go`, `ent/subscription_update.go`, etc.)

- [ ] **Step 4: Generate migration**

```bash
make generate-migration
```

Expected: a new SQL migration file in `migrations/postgres/` containing:
```sql
ALTER TABLE "subscriptions" ADD COLUMN "auto_invoice_threshold" decimal(20,6) NULL;
ALTER TABLE "plans" ADD COLUMN "auto_invoice_threshold" decimal(20,6) NULL;
```

- [ ] **Step 5: Verify code compiles**

```bash
go build ./...
```

Expected: no compile errors.

- [ ] **Step 6: Commit**

```bash
git add ent/schema/plan.go ent/schema/subscription.go ent/ migrations/
git commit -m "feat(schema): add auto_invoice_threshold to plan and subscription"
```

---

### Task 2: Update Domain Models

**Files:**
- Modify: `internal/domain/plan/model.go`
- Modify: `internal/domain/subscription/model.go`

- [ ] **Step 1: Update Plan domain model**

In `internal/domain/plan/model.go`, add to the `Plan` struct (after `DisplayOrder`):

```go
// AutoInvoiceThreshold is the usage amount (in subscription currency) that triggers
// an intermediate invoice mid-period. Nil means threshold billing is not configured at plan level.
AutoInvoiceThreshold *decimal.Decimal `db:"auto_invoice_threshold" json:"auto_invoice_threshold,omitempty" swaggertype:"string"`
```

Add `"github.com/shopspring/decimal"` import if not already present.

Update `FromEnt` function to map the new field:

```go
func FromEnt(e *ent.Plan) *Plan {
    if e == nil {
        return nil
    }
    return &Plan{
        ID:                   e.ID,
        Name:                 e.Name,
        LookupKey:            e.LookupKey,
        Description:          e.Description,
        EnvironmentID:        e.EnvironmentID,
        Metadata:             types.Metadata(e.Metadata),
        DisplayOrder:         &e.DisplayOrder,
        AutoInvoiceThreshold: e.AutoInvoiceThreshold,
        BaseModel: types.BaseModel{
            TenantID:  e.TenantID,
            Status:    types.Status(e.Status),
            CreatedAt: e.CreatedAt,
            UpdatedAt: e.UpdatedAt,
            CreatedBy: e.CreatedBy,
            UpdatedBy: e.UpdatedBy,
        },
    }
}
```

- [ ] **Step 2: Update Subscription domain model**

In `internal/domain/subscription/model.go`, add to the `Subscription` struct (after `SubscriptionType` field):

```go
// AutoInvoiceThreshold is the usage amount (in subscription currency) that triggers
// an intermediate invoice. Overrides the plan-level threshold when set.
// Nil means: inherit from the plan's threshold (which may also be nil = disabled).
AutoInvoiceThreshold *decimal.Decimal `db:"auto_invoice_threshold" json:"auto_invoice_threshold,omitempty" swaggertype:"string"`
```

Update `GetSubscriptionFromEnt` function — add the new field in the returned `&Subscription{...}` struct literal:

```go
AutoInvoiceThreshold: sub.AutoInvoiceThreshold,
```

Add it after the `SubscriptionType` line.

- [ ] **Step 3: Verify compile**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/plan/model.go internal/domain/subscription/model.go
git commit -m "feat(domain): add AutoInvoiceThreshold to Plan and Subscription models"
```

---

### Task 3: Add Repository Method

**Files:**
- Modify: `internal/domain/subscription/repository.go`
- Modify: `internal/repository/ent/subscription.go`

- [ ] **Step 1: Add method to repository interface**

In `internal/domain/subscription/repository.go`, add to the `Repository` interface (after `GetRecentSubscriptionsByPlan`):

```go
// GetSubscriptionsWithAutoInvoiceThreshold returns active subscriptions (paginated) where
// either the subscription itself or its plan has auto_invoice_threshold set.
GetSubscriptionsWithAutoInvoiceThreshold(ctx context.Context, limit, offset int) ([]*Subscription, error)
```

- [ ] **Step 2: Implement in ent repository**

In `internal/repository/ent/subscription.go`, add at the end of the file:

```go
// GetSubscriptionsWithAutoInvoiceThreshold returns active, published subscriptions (paginated)
// where either the subscription or its parent plan has auto_invoice_threshold set.
// Uses a two-step approach: raw SQL to collect matching IDs, then ent for full objects.
func (r *subscriptionRepository) GetSubscriptionsWithAutoInvoiceThreshold(ctx context.Context, limit, offset int) ([]*domainSub.Subscription, error) {
	tenantID := types.GetTenantID(ctx)
	envID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "subscription", "get_subscriptions_with_auto_invoice_threshold", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": envID,
		"limit":          limit,
		"offset":         offset,
	})
	defer FinishSpan(span)

	// Step 1: collect matching subscription IDs via a plan join.
	idQuery := `
		SELECT s.id
		FROM subscriptions s
		LEFT JOIN plans p
			ON  p.id             = s.plan_id
			AND p.status         = 'published'
			AND p.tenant_id      = $1
			AND p.environment_id = $2
		WHERE s.tenant_id              = $1
		  AND s.environment_id         = $2
		  AND s.status                 = 'published'
		  AND s.subscription_status    = 'active'
		  AND (
				s.auto_invoice_threshold IS NOT NULL
			OR  p.auto_invoice_threshold IS NOT NULL
		  )
		ORDER BY s.id
		LIMIT  $3
		OFFSET $4`

	rows, err := r.client.Reader(ctx).QueryContext(ctx, idQuery, tenantID, envID, limit, offset)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query threshold subscription IDs").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan threshold subscription ID").
				Mark(ierr.ErrDatabase)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to iterate threshold subscription IDs").
			Mark(ierr.ErrDatabase)
	}

	if len(ids) == 0 {
		SetSpanSuccess(span)
		return nil, nil
	}

	// Step 2: fetch full objects via ent using the collected IDs.
	subs, err := r.client.Reader(ctx).Subscription.Query().
		Where(
			subscription.IDIn(ids...),
			subscription.TenantID(tenantID),
			subscription.EnvironmentID(envID),
			subscription.Status(string(types.StatusPublished)),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to fetch threshold subscriptions").
			Mark(ierr.ErrDatabase)
	}

	result := make([]*domainSub.Subscription, len(subs))
	for i, sub := range subs {
		result[i] = domainSub.GetSubscriptionFromEnt(sub)
	}

	SetSpanSuccess(span)
	return result, nil
}
```

- [ ] **Step 3: Verify compile**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/subscription/repository.go internal/repository/ent/subscription.go
git commit -m "feat(repo): add GetSubscriptionsWithAutoInvoiceThreshold"
```

---

### Task 4: Add `InvoiceBillingReasonThresholdBilling` Constant

**Files:**
- Modify: `internal/types/invoice.go`

- [ ] **Step 1: Add constant**

In `internal/types/invoice.go`, add after the `InvoiceBillingReasonManual` constant (around line 230):

```go
// InvoiceBillingReasonThresholdBilling is generated mid-period when cumulative usage for the
// current billing period crosses the subscription's auto_invoice_threshold.
// Flow: ThresholdBillingWorkflow (5-min cron) → ProcessThresholdBilling service method
// Compute: arrear usage charges for current_period_start → now (ReferencePointPeriodEnd)
// Zero-dollar: marked SKIPPED; current_period_start still advances to avoid re-checking.
// Side-effect: advances current_period_start to the invoice's period_end after finalization.
InvoiceBillingReasonThresholdBilling InvoiceBillingReason = "THRESHOLD_BILLING"
```

- [ ] **Step 2: Add to Validate() allowed list**

In the `Validate()` method on `InvoiceBillingReason`, add `InvoiceBillingReasonThresholdBilling` to the `allowed` slice:

```go
allowed := []InvoiceBillingReason{
    InvoiceBillingReasonSubscriptionCreate,
    InvoiceBillingReasonSubscriptionCycle,
    InvoiceBillingReasonSubscriptionUpdate,
    InvoiceBillingReasonSubscriptionTrialEnd,
    InvoiceBillingReasonSubscriptionTrialStart,
    InvoiceBillingReasonProration,
    InvoiceBillingReasonManual,
    InvoiceBillingReasonThresholdBilling,
}
```

- [ ] **Step 3: Verify compile**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/types/invoice.go
git commit -m "feat(types): add InvoiceBillingReasonThresholdBilling constant"
```

---

### Task 5: Update Service Interfaces

**Files:**
- Modify: `internal/interfaces/service.go`
- Modify: `internal/api/dto/subscription.go`

- [ ] **Step 1: Add FinalizeInvoice to InvoiceService interface**

In `internal/interfaces/service.go`, add `FinalizeInvoice` to the `InvoiceService` interface (after `VoidInvoice`):

```go
type InvoiceService interface {
    CreateInvoice(ctx context.Context, req dto.CreateInvoiceRequest) (*dto.InvoiceResponse, error)
    CreateEmptyDraftInvoice(ctx context.Context, req dto.CreateDraftInvoiceRequest) (*dto.InvoiceResponse, error)
    ComputeInvoice(ctx context.Context, invoiceID string, req *dto.InvoiceComputeRequest) (bool, error)
    FinalizeInvoice(ctx context.Context, id string) error
    GetInvoice(ctx context.Context, id string) (*dto.InvoiceResponse, error)
    ListInvoices(ctx context.Context, filter *types.InvoiceFilter) (*dto.ListInvoicesResponse, error)
    UpdateInvoice(ctx context.Context, id string, req dto.UpdateInvoiceRequest) (*dto.InvoiceResponse, error)
    DeleteInvoice(ctx context.Context, id string) error
    ReconcilePaymentStatus(ctx context.Context, invoiceID string, paymentStatus types.PaymentStatus, paymentAmount *decimal.Decimal) error
    VoidInvoice(ctx context.Context, id string, req dto.InvoiceVoidRequest) error
}
```

- [ ] **Step 2: Add ProcessThresholdBilling to SubscriptionService interface**

In `internal/interfaces/service.go`, add `ProcessThresholdBilling` to the `SubscriptionService` interface (after `ProcessSubscriptionRenewalDueAlert`):

```go
// ProcessThresholdBilling checks all subscriptions with an effective auto_invoice_threshold
// and generates mid-period invoices for those whose current-period usage has crossed the threshold.
ProcessThresholdBilling(ctx context.Context) (*dto.ThresholdBillingResult, error)
```

- [ ] **Step 3: Add ThresholdBillingResult DTO**

In `internal/api/dto/subscription.go`, add after the `SubscriptionUpdatePeriodResponse` struct:

```go
// ThresholdBillingResult is the result of a single ProcessThresholdBilling run.
type ThresholdBillingResult struct {
    TotalChecked  int                          `json:"total_checked"`
    TotalInvoiced int                          `json:"total_invoiced"`
    TotalSkipped  int                          `json:"total_skipped"`
    TotalFailed   int                          `json:"total_failed"`
    Items         []*ThresholdBillingResultItem `json:"items,omitempty"`
}

// ThresholdBillingResultItem is the per-subscription outcome.
type ThresholdBillingResultItem struct {
    SubscriptionID string `json:"subscription_id"`
    Invoiced       bool   `json:"invoiced"`
    InvoiceID      string `json:"invoice_id,omitempty"`
    Error          string `json:"error,omitempty"`
}
```

- [ ] **Step 4: Verify compile**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/interfaces/service.go internal/api/dto/subscription.go
git commit -m "feat(interfaces): add ProcessThresholdBilling and FinalizeInvoice to service interfaces"
```

---

### Task 6: Implement ProcessThresholdBilling Service Method

**Files:**
- Modify: `internal/service/subscription.go`

- [ ] **Step 1: Write the failing test first**

Create `internal/service/subscription_threshold_billing_test.go`:

```go
package service_test

import (
    "context"
    "testing"
    "time"

    "github.com/flexprice/flexprice/internal/api/dto"
    "github.com/flexprice/flexprice/internal/domain/subscription"
    "github.com/flexprice/flexprice/internal/testutil"
    "github.com/flexprice/flexprice/internal/types"
    "github.com/shopspring/decimal"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestProcessThresholdBilling(t *testing.T) {
    // Uses testutil.SetupTestDB() for a real DB — skip if no DB available.
    testutil.SkipIfNoDB(t)

    ctx := context.Background()
    env := testutil.NewTestEnvironment(ctx, t)
    svc := env.SubscriptionService

    threshold := decimal.NewFromFloat(100)

    tests := []struct {
        name           string
        setupSub       func() *subscription.Subscription
        mockUsageAmt   float64
        wantInvoiced   bool
        wantErrContain string
    }{
        {
            name: "usage below threshold — no invoice",
            setupSub: func() *subscription.Subscription {
                return testutil.CreateActiveSubscriptionWithThreshold(ctx, t, env, &threshold)
            },
            mockUsageAmt: 50.0,
            wantInvoiced: false,
        },
        {
            name: "usage at threshold — invoice created",
            setupSub: func() *subscription.Subscription {
                return testutil.CreateActiveSubscriptionWithThreshold(ctx, t, env, &threshold)
            },
            mockUsageAmt: 100.0,
            wantInvoiced: true,
        },
        {
            name: "usage above threshold — invoice created",
            setupSub: func() *subscription.Subscription {
                return testutil.CreateActiveSubscriptionWithThreshold(ctx, t, env, &threshold)
            },
            mockUsageAmt: 150.0,
            wantInvoiced: true,
        },
        {
            name: "no threshold set — no invoice",
            setupSub: func() *subscription.Subscription {
                return testutil.CreateActiveSubscription(ctx, t, env)
            },
            mockUsageAmt: 999.0,
            wantInvoiced: false,
        },
    }

    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            sub := tc.setupSub()
            _ = sub // used to seed DB

            result, err := svc.ProcessThresholdBilling(ctx)
            require.NoError(t, err)
            require.NotNil(t, result)

            if tc.wantInvoiced {
                assert.Greater(t, result.TotalInvoiced, 0)
            } else {
                assert.Equal(t, 0, result.TotalInvoiced)
            }
        })
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -v -race ./internal/service -run TestProcessThresholdBilling
```

Expected: FAIL — `ProcessThresholdBilling` not implemented yet (compile error or method missing).

- [ ] **Step 3: Implement ProcessThresholdBilling**

In `internal/service/subscription.go`, add the following method to `subscriptionService` (near other cron methods like `UpdateBillingPeriods`):

```go
// ProcessThresholdBilling checks all active subscriptions that have an effective
// auto_invoice_threshold and generates a mid-period invoice whenever current-period
// usage (in dollars) meets or exceeds that threshold.
// After a successful invoice, current_period_start is advanced to now so the next
// run starts fresh from that point.
func (s *subscriptionService) ProcessThresholdBilling(ctx context.Context) (*dto.ThresholdBillingResult, error) {
    const batchSize = 100
    now := time.Now().UTC()

    s.Logger.InfowCtx(ctx, "starting threshold billing run", "now", now)

    result := &dto.ThresholdBillingResult{
        Items: make([]*dto.ThresholdBillingResultItem, 0),
    }

    offset := 0
    for {
        subs, err := s.SubRepo.GetSubscriptionsWithAutoInvoiceThreshold(ctx, batchSize, offset)
        if err != nil {
            return result, fmt.Errorf("fetching threshold subscriptions: %w", err)
        }
        if len(subs) == 0 {
            break
        }

        for _, sub := range subs {
            // Propagate tenant/env context per subscription.
            subCtx := context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
            subCtx = context.WithValue(subCtx, types.CtxEnvironmentID, sub.EnvironmentID)
            subCtx = context.WithValue(subCtx, types.CtxUserID, sub.CreatedBy)

            result.TotalChecked++
            item := &dto.ThresholdBillingResultItem{SubscriptionID: sub.ID}

            if err := s.processOneThresholdSubscription(subCtx, sub, now, item); err != nil {
                s.Logger.ErrorwCtx(subCtx, "threshold billing failed for subscription",
                    "subscription_id", sub.ID, "error", err)
                result.TotalFailed++
                item.Error = err.Error()
            } else if item.Invoiced {
                result.TotalInvoiced++
            } else {
                result.TotalSkipped++
            }
            result.Items = append(result.Items, item)
        }

        offset += len(subs)
        if len(subs) < batchSize {
            break
        }
    }

    s.Logger.InfowCtx(ctx, "threshold billing run complete",
        "total_checked", result.TotalChecked,
        "total_invoiced", result.TotalInvoiced,
        "total_skipped", result.TotalSkipped,
        "total_failed", result.TotalFailed)

    return result, nil
}

// processOneThresholdSubscription checks and (if needed) invoices a single subscription.
func (s *subscriptionService) processOneThresholdSubscription(
    ctx context.Context,
    sub *subscription.Subscription,
    now time.Time,
    item *dto.ThresholdBillingResultItem,
) error {
    invoiceService := NewInvoiceService(s.ServiceParams)
    // Guard: skip non-active or inherited types.
    if sub.SubscriptionStatus != types.SubscriptionStatusActive {
        return nil
    }
    if sub.SubscriptionType == types.SubscriptionTypeInherited ||
        sub.SubscriptionType == types.SubscriptionTypeGroupedInvoicing {
        return nil
    }

    // Resolve effective threshold: subscription overrides plan.
    effectiveThreshold := sub.AutoInvoiceThreshold
    if effectiveThreshold == nil {
        plan, err := s.PlanRepo.Get(ctx, sub.PlanID)
        if err != nil {
            return fmt.Errorf("fetching plan %s: %w", sub.PlanID, err)
        }
        effectiveThreshold = plan.AutoInvoiceThreshold
    }
    if effectiveThreshold == nil {
        // Threshold not set at either level — skip.
        return nil
    }

    // Calculate current-period usage amount.
    usageResp, err := s.GetUsageBySubscription(ctx, &dto.GetUsageBySubscriptionRequest{
        SubscriptionID: sub.ID,
        StartTime:      sub.CurrentPeriodStart,
        EndTime:        now,
    })
    if err != nil {
        return fmt.Errorf("calculating usage for subscription %s: %w", sub.ID, err)
    }

    usageAmount := decimal.NewFromFloat(usageResp.Amount)
    if usageAmount.LessThan(*effectiveThreshold) {
        // Below threshold — nothing to do.
        return nil
    }

    // Threshold crossed — create invoice for current_period_start → now.
    billingPeriod := string(sub.BillingPeriod)
    idempotencyKey := fmt.Sprintf("threshold-%s-%d", sub.ID, sub.CurrentPeriodStart.Unix())

    draft, err := invoiceService.CreateEmptyDraftInvoice(ctx, dto.CreateDraftInvoiceRequest{
        CustomerID:             sub.GetInvoicingCustomerID(),
        SubscriptionCustomerID: &sub.CustomerID,
        SubscriptionID:         lo.ToPtr(sub.ID),
        InvoiceType:            types.InvoiceTypeSubscription,
        Currency:               sub.Currency,
        BillingPeriod:          &billingPeriod,
        PeriodStart:            lo.ToPtr(sub.CurrentPeriodStart),
        PeriodEnd:              lo.ToPtr(now),
        BillingReason:          types.InvoiceBillingReasonThresholdBilling,
        IdempotencyKey:         &idempotencyKey,
    })
    if err != nil {
        return fmt.Errorf("creating draft invoice for subscription %s: %w", sub.ID, err)
    }

    skipped, err := invoiceService.ComputeInvoice(ctx, draft.ID, nil)
    if err != nil {
        return fmt.Errorf("computing invoice %s for subscription %s: %w", draft.ID, sub.ID, err)
    }

    if !skipped {
        if err := invoiceService.FinalizeInvoice(ctx, draft.ID); err != nil {
            return fmt.Errorf("finalizing invoice %s for subscription %s: %w", draft.ID, sub.ID, err)
        }
    }

    // Advance current_period_start only after successful invoice creation.
    sub.CurrentPeriodStart = now
    if err := s.SubRepo.Update(ctx, sub); err != nil {
        return fmt.Errorf("advancing current_period_start for subscription %s: %w", sub.ID, err)
    }

    item.Invoiced = true
    item.InvoiceID = draft.ID
    return nil
}
```

Add `"github.com/samber/lo"` to the imports at the top of `subscription.go` if not already present. The `interfaces` package is already imported (it is aliased at the top of the file for `SubscriptionService`).

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -v -race ./internal/service -run TestProcessThresholdBilling
```

Expected: PASS (or SKIP if no DB available in CI — that is acceptable).

- [ ] **Step 5: Compile check**

```bash
go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/subscription.go internal/service/subscription_threshold_billing_test.go
git commit -m "feat(service): implement ProcessThresholdBilling"
```

---

### Task 7: Add Temporal Models

**Files:**
- Modify: `internal/temporal/models/cron.go`

- [ ] **Step 1: Add input and result structs**

In `internal/temporal/models/cron.go`, add at the end of the file:

```go
// ===================== Threshold Billing =====================

// ThresholdBillingWorkflowInput is the input for ThresholdBillingWorkflow.
// No fields required — the activity fetches all qualifying subscriptions itself.
type ThresholdBillingWorkflowInput struct{}

// ThresholdBillingWorkflowResult mirrors key counts from ProcessThresholdBilling.
type ThresholdBillingWorkflowResult struct {
    TotalChecked  int `json:"total_checked"`
    TotalInvoiced int `json:"total_invoiced"`
    TotalSkipped  int `json:"total_skipped"`
    TotalFailed   int `json:"total_failed"`
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/models/cron.go
git commit -m "feat(temporal): add ThresholdBillingWorkflow input/result models"
```

---

### Task 8: Add Temporal Activity

**Files:**
- Modify: `internal/temporal/activities/cron/subscription_activities.go`

- [ ] **Step 1: Add ProcessThresholdBillingActivity**

In `internal/temporal/activities/cron/subscription_activities.go`, add at the end of the file:

```go
// ProcessThresholdBillingActivity runs ProcessThresholdBilling for all qualifying subscriptions.
func (a *SubscriptionCronActivities) ProcessThresholdBillingActivity(ctx context.Context) (*cronModels.ThresholdBillingWorkflowResult, error) {
    log := activity.GetLogger(ctx)
    log.Info("Processing threshold billing (cron activity)")

    result, err := a.subscriptionService.ProcessThresholdBilling(ctx)
    if err != nil {
        return nil, err
    }

    return &cronModels.ThresholdBillingWorkflowResult{
        TotalChecked:  result.TotalChecked,
        TotalInvoiced: result.TotalInvoiced,
        TotalSkipped:  result.TotalSkipped,
        TotalFailed:   result.TotalFailed,
    }, nil
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/activities/cron/subscription_activities.go
git commit -m "feat(temporal): add ProcessThresholdBillingActivity"
```

---

### Task 9: Create Threshold Billing Workflow

**Files:**
- Create: `internal/temporal/workflows/cron/threshold_billing_workflow.go`

- [ ] **Step 1: Create the workflow file**

```go
package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessThresholdBilling = "ProcessThresholdBillingActivity"
)

// ThresholdBillingWorkflow checks all subscriptions with an effective auto_invoice_threshold
// and creates mid-period invoices for those whose current-period usage has crossed the threshold.
// Intended to run every 5 minutes via a Temporal Schedule.
func ThresholdBillingWorkflow(ctx workflow.Context, _ cronModels.ThresholdBillingWorkflowInput) (*cronModels.ThresholdBillingWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting ThresholdBillingWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.ThresholdBillingWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessThresholdBilling).Get(ctx, &result); err != nil {
		log.Error("ThresholdBillingWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("ThresholdBillingWorkflow completed",
		"total_checked", result.TotalChecked,
		"total_invoiced", result.TotalInvoiced,
		"total_failed", result.TotalFailed)

	return &result, nil
}
```

- [ ] **Step 2: Verify compile**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/temporal/workflows/cron/threshold_billing_workflow.go
git commit -m "feat(temporal): add ThresholdBillingWorkflow"
```

---

### Task 10: Register Workflow and Activity

**Files:**
- Modify: `internal/temporal/registration.go`

- [ ] **Step 1: Register workflow in the cron task queue**

In `internal/temporal/registration.go`, find the `cronActivityBundle` struct definition and verify `subscription` is in it (it is — it's `*cronActivities.SubscriptionCronActivities`). No change needed there.

Find the section where cron workflows and activities are registered (look for `cronWorkflows.SubscriptionBillingPeriodsWorkflow`). Add the new workflow and activity alongside it:

In the `Workflows` slice for the cron task queue, add:
```go
cronWorkflows.ThresholdBillingWorkflow,
```

In the `Activities` slice for the cron task queue, add the activity method reference:
```go
cronBundle.subscription.ProcessThresholdBillingActivity,
```

- [ ] **Step 2: Verify compile**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Run vet**

```bash
go vet ./...
```

Expected: no issues.

- [ ] **Step 4: Commit**

```bash
git add internal/temporal/registration.go
git commit -m "feat(temporal): register ThresholdBillingWorkflow and ProcessThresholdBillingActivity"
```

---

### Task 11: Final Verification

- [ ] **Step 1: Run all tests**

```bash
make test
```

Expected: PASS (or any pre-existing failures unrelated to this change).

- [ ] **Step 2: Build the full server binary**

```bash
go build ./cmd/server/...
```

Expected: clean build.

- [ ] **Step 3: Verify migration applies cleanly**

```bash
make migrate-ent-dry-run
```

Expected: shows the two new column additions with no conflicts.

- [ ] **Step 4: Summary commit (if anything was missed)**

If there are any stray changes not committed yet:

```bash
git add -A
git commit -m "chore: finalize progressive billing implementation"
```

---

## Implementation Notes

- **`processOneThresholdSubscription`** calls `GetUsageBySubscription` which queries the meter_usage ClickHouse table. For subscriptions without any metered line items this returns `Amount: 0`, which is below any positive threshold — safe.
- **Idempotency key** format `threshold-{subID}-{periodStart.Unix()}` ensures that if the workflow fires twice before `current_period_start` is advanced (e.g., a retry after a partial failure), the second `CreateEmptyDraftInvoice` call returns the existing draft rather than creating a duplicate.
- **`current_period_end` is never modified** — the subscription's regular end-of-period billing continues unaffected. The threshold invoice is an intermediate invoice within the period.
- **`ComputeInvoice` may skip** (returns `skipped=true`) if all usage is covered by credits and the total is $0. In that case, `FinalizeInvoice` is not called but `current_period_start` is still advanced to avoid perpetually re-checking the same window.
- The Temporal Schedule (every 5 min) must be created separately in the Temporal UI or via `tctl schedule create`. The workflow is registered and ready; the schedule itself is infrastructure configuration outside this code change.
