# Grouped Invoicing E2E Test Plan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix `GetPreviewInvoice` to return a clubbed view for parent subscriptions, then run the full E2E test suite against the live server, and add service-layer test cases.

**Architecture:** `GetPreviewInvoice` (and `GetInternalPreviewInvoice`) in `internal/service/invoice.go` are extended to detect `subscription_type=parent`, fetch grouped children via `SubRepo.List`, and call `PrepareGroupedInvoiceRequest` instead of `PrepareSubscriptionInvoiceRequest`. E2E tests run via curl against `http://localhost:8080`. Service tests are added to `internal/service/subscription_grouped_invoicing_test.go` and a new `internal/service/subscription_create_grouped_test.go`.

**Tech Stack:** Go 1.23, Gin, Ent ORM, PostgreSQL, curl for E2E

---

## File Map

| File | Change |
|---|---|
| `internal/service/invoice.go` | Fix `GetPreviewInvoice` and `GetInternalPreviewInvoice` for parent+grouped children |
| `internal/service/subscription_grouped_invoicing_test.go` | Add `TestGetGroupedInvoicingSubscriptions_*` tests |
| `internal/service/subscription_create_grouped_test.go` | New file — create-time grouped invoicing + delegated service tests |
| `internal/service/subscription_modification_grouped_test.go` | New file — modify API preview/execute service tests |

---

## Task 1: Fix GetPreviewInvoice for parent subscriptions

**Files:**
- Modify: `internal/service/invoice.go` (functions `GetPreviewInvoice` at ~line 2024 and `GetInternalPreviewInvoice` at ~line 2079)

The fix: after fetching `sub`, if it is a `parent` type, list its `grouped_invoicing` children via `SubRepo.List` and call `PrepareGroupedInvoiceRequest` instead of `PrepareSubscriptionInvoiceRequest`.

- [ ] **Step 1: Update `GetPreviewInvoice`**

Replace the single-subscription prepare block with a branching helper. In `internal/service/invoice.go`, replace the `GetPreviewInvoice` function body:

```go
func (s *invoiceService) GetPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error) {
	billingService := NewBillingService(s.ServiceParams)

	sub, _, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	if req.PeriodStart == nil {
		req.PeriodStart = &sub.CurrentPeriodStart
	}
	if req.PeriodEnd == nil {
		req.PeriodEnd = &sub.CurrentPeriodEnd
	}

	invReq, err := s.prepareInvoiceRequestForPreview(ctx, billingService, sub, *req.PeriodStart, *req.PeriodEnd, types.ReferencePointPreview)
	if err != nil {
		return nil, err
	}

	s.Logger.InfowCtx(ctx, "prepared invoice request for preview", "invoice_request", invReq)

	if req.HideZeroChargesLineItems {
		invReq.LineItems = lo.Filter(invReq.LineItems, func(item dto.CreateInvoiceLineItemRequest, _ int) bool {
			return !item.Amount.IsZero()
		})
	}

	inv, err := invReq.ToInvoice(ctx)
	if err != nil {
		return nil, err
	}

	response := dto.NewInvoiceResponse(inv)
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}
	response.WithCustomer(&dto.CustomerResponse{Customer: customer})
	return response, nil
}
```

- [ ] **Step 2: Update `GetInternalPreviewInvoice`**

Same pattern — replace the prepare block in `GetInternalPreviewInvoice`:

```go
func (s *invoiceService) GetInternalPreviewInvoice(ctx context.Context, req dto.GetPreviewInvoiceRequest) (*dto.InvoiceResponse, error) {
	billingService := NewBillingService(s.ServiceParams)

	sub, _, err := s.SubRepo.GetWithLineItems(ctx, req.SubscriptionID)
	if err != nil {
		return nil, err
	}

	if req.PeriodStart == nil {
		req.PeriodStart = &sub.CurrentPeriodStart
	}
	if req.PeriodEnd == nil {
		req.PeriodEnd = &sub.CurrentPeriodEnd
	}

	invReq, err := s.prepareInvoiceRequestForPreview(ctx, billingService, sub, *req.PeriodStart, *req.PeriodEnd, types.ReferencePointInternalPreview)
	if err != nil {
		return nil, err
	}

	s.Logger.InfowCtx(ctx, "prepared invoice request for internal preview", "invoice_request", invReq)

	if req.HideZeroChargesLineItems {
		invReq.LineItems = lo.Filter(invReq.LineItems, func(item dto.CreateInvoiceLineItemRequest, _ int) bool {
			return !item.Amount.IsZero()
		})
	}

	inv, err := invReq.ToInvoice(ctx)
	if err != nil {
		return nil, err
	}

	response := dto.NewInvoiceResponse(inv)
	customer, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return nil, err
	}
	response.WithCustomer(&dto.CustomerResponse{Customer: customer})
	return response, nil
}
```

- [ ] **Step 3: Add `prepareInvoiceRequestForPreview` helper**

Add this private helper just before `GetPreviewInvoice`:

```go
// prepareInvoiceRequestForPreview builds a CreateInvoiceRequest for preview endpoints.
// For parent subscriptions it fetches grouped_invoicing children and returns a merged
// clubbed request; for all other types it falls back to PrepareSubscriptionInvoiceRequest.
func (s *invoiceService) prepareInvoiceRequestForPreview(
	ctx context.Context,
	billingService BillingService,
	sub *subscription.Subscription,
	periodStart, periodEnd time.Time,
	referencePoint types.InvoiceReferencePoint,
) (*dto.CreateInvoiceRequest, error) {
	if sub.SubscriptionType == types.SubscriptionTypeParent {
		filter := types.NewNoLimitSubscriptionFilter()
		filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
		filter.ParentSubscriptionIDs = []string{sub.ID}
		filter.SubscriptionTypes = []types.SubscriptionType{types.SubscriptionTypeGroupedInvoicing}
		filter.SubscriptionStatus = []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
			types.SubscriptionStatusDraft,
		}
		children, err := s.SubRepo.List(ctx, filter)
		if err != nil {
			return nil, err
		}
		if len(children) > 0 {
			return billingService.PrepareGroupedInvoiceRequest(ctx, &dto.PrepareGroupedInvoiceRequestParams{
				ParentSubscription: sub,
				ChildSubscriptions: children,
				PeriodStart:        periodStart,
				PeriodEnd:          periodEnd,
				ReferencePoint:     referencePoint,
			})
		}
	}
	return billingService.PrepareSubscriptionInvoiceRequest(ctx, &dto.PrepareSubscriptionInvoiceRequestParams{
		Subscription:   sub,
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		ReferencePoint: referencePoint,
	})
}
```

- [ ] **Step 4: Build**

Run:
```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/sweet-volhard-a68c59 && go build ./internal/service/...
```
Expected: no errors.

- [ ] **Step 5: Run existing tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/sweet-volhard-a68c59 && go test ./internal/service/... -count=1 -timeout 120s
```
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/service/invoice.go
git commit -m "feat(invoice): return clubbed view for parent subscription preview"
```

---

## Task 2: Start server and run E2E Phase 0 (Setup)

**Files:** None (curl only)

**Pre-condition:** Docker infrastructure (postgres, kafka, clickhouse) is running.

- [ ] **Step 1: Kill any existing server and start fresh**

```bash
pkill -f "go run\|flexprice-server\|cmd/server/main.go" 2>/dev/null; sleep 1
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/sweet-volhard-a68c59
make run &
sleep 8
curl -s http://localhost:8080/health | grep -q "ok\|healthy\|200" && echo "SERVER UP" || echo "SERVER DOWN - check logs"
```

- [ ] **Step 2: Create Plan A (flat fee $10/month)**

```bash
# Step A: Create plan
PLAN=$(curl -s -X POST http://localhost:8080/v1/plans \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d '{"name":"gi-plan","lookup_key":"gi-plan"}')
echo "PLAN: $PLAN"
PLAN_ID=$(echo $PLAN | jq -r '.id')
echo "PLAN_ID=$PLAN_ID"

# Step B: Create flat fee price on the plan
PRICE=$(curl -s -X POST "http://localhost:8080/v1/plans/$PLAN_ID/prices" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d '{
    "amount": "10",
    "currency": "usd",
    "type": "flat_fee",
    "billing_cadence": "recurring",
    "billing_period": "monthly",
    "billing_period_count": 1,
    "billing_model": "flat_fee",
    "invoice_cadence": "arrear"
  }')
echo "PRICE: $PRICE"
PRICE_ID=$(echo $PRICE | jq -r '.id')
echo "PRICE_ID=$PRICE_ID"
```

Expected: Both return JSON with `id` fields populated.

- [ ] **Step 3: Create 3 customers**

```bash
CUST_P=$(curl -s -X POST http://localhost:8080/v1/customers \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d '{"name":"GI Parent","email":"gi-parent@test.com","external_id":"gi-parent-cust"}')
echo "CUST_P: $CUST_P"
CUST_P_ID=$(echo $CUST_P | jq -r '.id')

CUST_C1=$(curl -s -X POST http://localhost:8080/v1/customers \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d '{"name":"GI Child 1","email":"gi-child-1@test.com","external_id":"gi-child-1"}')
echo "CUST_C1: $CUST_C1"
CUST_C1_ID=$(echo $CUST_C1 | jq -r '.id')

CUST_C2=$(curl -s -X POST http://localhost:8080/v1/customers \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d '{"name":"GI Child 2","email":"gi-child-2@test.com","external_id":"gi-child-2"}')
echo "CUST_C2: $CUST_C2"
CUST_C2_ID=$(echo $CUST_C2 | jq -r '.id')

echo "IDs: P=$CUST_P_ID C1=$CUST_C1_ID C2=$CUST_C2_ID"
```

Expected: Three customer objects, each with unique `id`.

---

## Task 3: E2E Phase 1 — Create-Time Subscription Types

All commands use variables set in Task 2. Save all IDs as you go.

- [ ] **Step 1: TC 1.1 — Standalone baseline**

```bash
SUB_STANDALONE=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"customer_id\": \"$CUST_P_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"usd\",
    \"billing_period\": \"monthly\",
    \"billing_period_count\": 1,
    \"billing_cadence\": \"recurring\",
    \"billing_cycle\": \"anniversary\",
    \"collection_method\": \"send_invoice\"
  }")
echo "TC1.1 STANDALONE: $SUB_STANDALONE"
echo $SUB_STANDALONE | jq '{id, subscription_type, parent_subscription_id}'
```

Expected: `subscription_type=standalone`, `parent_subscription_id=null`.

- [ ] **Step 2: TC 1.2 — Parent subscription**

```bash
SUB_PARENT=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"customer_id\": \"$CUST_P_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"usd\",
    \"billing_period\": \"monthly\",
    \"billing_period_count\": 1,
    \"billing_cadence\": \"recurring\",
    \"billing_cycle\": \"anniversary\",
    \"collection_method\": \"send_invoice\",
    \"inheritance\": {
      \"invoicing_behavior\": \"parent\"
    }
  }")
echo "TC1.2 PARENT: $SUB_PARENT"
echo $SUB_PARENT | jq '{id, subscription_type}'
SUB_PARENT_ID=$(echo $SUB_PARENT | jq -r '.id')
echo "SUB_PARENT_ID=$SUB_PARENT_ID"
```

Expected: `subscription_type=parent`.

- [ ] **Step 3: TC 1.3 — Grouped child at creation time**

```bash
SUB_CHILD_GI=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"customer_id\": \"$CUST_C1_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"usd\",
    \"billing_period\": \"monthly\",
    \"billing_period_count\": 1,
    \"billing_cadence\": \"recurring\",
    \"billing_cycle\": \"anniversary\",
    \"collection_method\": \"send_invoice\",
    \"inheritance\": {
      \"invoicing_behavior\": \"grouped_invoicing\",
      \"parent_subscription_id\": \"$SUB_PARENT_ID\"
    }
  }")
echo "TC1.3 GROUPED_CHILD: $SUB_CHILD_GI"
echo $SUB_CHILD_GI | jq '{id, subscription_type, parent_subscription_id}'
SUB_CHILD_GI_ID=$(echo $SUB_CHILD_GI | jq -r '.id')
```

Expected: `subscription_type=grouped_invoicing`, `parent_subscription_id=<SUB_PARENT_ID>`.

- [ ] **Step 4: TC 1.4 — Parent attaches existing standalones**

```bash
# First create 2 standalones for C1 and C2
SA_1=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_C1_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"monthly\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"collection_method\":\"send_invoice\"}")
SA_1_ID=$(echo $SA_1 | jq -r '.id')

SA_2=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_C2_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"monthly\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"collection_method\":\"send_invoice\"}")
SA_2_ID=$(echo $SA_2 | jq -r '.id')

echo "SA_1_ID=$SA_1_ID  SA_2_ID=$SA_2_ID"

# Now create parent that attaches them
SUB_PARENT2=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"customer_id\": \"$CUST_P_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"usd\",
    \"billing_period\": \"monthly\",
    \"billing_period_count\": 1,
    \"billing_cadence\": \"recurring\",
    \"billing_cycle\": \"anniversary\",
    \"collection_method\": \"send_invoice\",
    \"inheritance\": {
      \"invoicing_behavior\": \"parent\",
      \"sub_ids_for_grouped_invoicing\": [\"$SA_1_ID\", \"$SA_2_ID\"]
    }
  }")
SUB_PARENT2_ID=$(echo $SUB_PARENT2 | jq -r '.id')
echo "TC1.4 PARENT2: $SUB_PARENT2_ID"

# Verify both standalones flipped
echo "SA_1 after:" && curl -s "http://localhost:8080/v1/subscriptions/$SA_1_ID" -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" | jq '{subscription_type, parent_subscription_id}'
echo "SA_2 after:" && curl -s "http://localhost:8080/v1/subscriptions/$SA_2_ID" -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" | jq '{subscription_type, parent_subscription_id}'
```

Expected: Both show `subscription_type=grouped_invoicing`, `parent_subscription_id=<SUB_PARENT2_ID>`.

- [ ] **Step 5: TC 1.5 — Delegated subscription**

```bash
SUB_DELEGATED=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"customer_id\": \"$CUST_C1_ID\",
    \"plan_id\": \"$PLAN_ID\",
    \"currency\": \"usd\",
    \"billing_period\": \"monthly\",
    \"billing_period_count\": 1,
    \"billing_cadence\": \"recurring\",
    \"billing_cycle\": \"anniversary\",
    \"collection_method\": \"send_invoice\",
    \"inheritance\": {
      \"invoicing_behavior\": \"delegated\",
      \"invoicing_customer_external_id\": \"gi-parent-cust\"
    }
  }")
echo "TC1.5 DELEGATED: $SUB_DELEGATED"
echo $SUB_DELEGATED | jq '{id, subscription_type, invoicing_customer_id}'
```

Expected: `subscription_type=delegated`, `invoicing_customer_id=<CUST_P_ID>`.

---

## Task 4: E2E Phase 2 — Modify API

Using `SUB_PARENT_ID` (from TC 1.2) and a fresh standalone child.

- [ ] **Step 1: Create fresh standalone child for modify tests**

```bash
MOD_CHILD=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_C2_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"monthly\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"collection_method\":\"send_invoice\"}")
MOD_CHILD_ID=$(echo $MOD_CHILD | jq -r '.id')
echo "MOD_CHILD_ID=$MOD_CHILD_ID"
```

- [ ] **Step 2: TC 2.1 — Preview add (dry run)**

```bash
curl -s -X POST "http://localhost:8080/v1/subscriptions${SUB_PARENT_ID}/modify/preview" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"grouped_invoicing_add\",
    \"grouped_invoicing_params\": {
      \"parent_subscription_id\": \"$SUB_PARENT_ID\",
      \"child_subscription_ids\": [\"$MOD_CHILD_ID\"]
    }
  }"

# Verify no write happened
curl -s "http://localhost:8080/v1/subscriptions/$MOD_CHILD_ID" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" | jq '{subscription_type}'
```

Expected: Preview response with changed subscriptions. Child still shows `standalone`.

- [ ] **Step 3: TC 2.2 — Execute add**

```bash
curl -s -X POST "http://localhost:8080/v1/subscriptions${SUB_PARENT_ID}/modify/execute" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"grouped_invoicing_add\",
    \"grouped_invoicing_params\": {
      \"parent_subscription_id\": \"$SUB_PARENT_ID\",
      \"child_subscription_ids\": [\"$MOD_CHILD_ID\"]
    }
  }"

curl -s "http://localhost:8080/v1/subscriptions/$MOD_CHILD_ID" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" | jq '{subscription_type, parent_subscription_id}'
```

Expected: `subscription_type=grouped_invoicing`, `parent_subscription_id=<SUB_PARENT_ID>`.

- [ ] **Step 4: TC 2.3 — Preview remove (dry run)**

```bash
curl -s -X POST "http://localhost:8080/v1/subscriptions${SUB_PARENT_ID}/modify/preview" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"grouped_invoicing_remove\",
    \"grouped_invoicing_params\": {
      \"child_subscription_ids\": [\"$MOD_CHILD_ID\"]
    }
  }"

curl -s "http://localhost:8080/v1/subscriptions/$MOD_CHILD_ID" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" | jq '{subscription_type}'
```

Expected: Preview response. Child still shows `grouped_invoicing`.

- [ ] **Step 5: TC 2.4 — Execute remove**

```bash
curl -s -X POST "http://localhost:8080/v1/subscriptions${SUB_PARENT_ID}/modify/execute" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{
    \"type\": \"grouped_invoicing_remove\",
    \"grouped_invoicing_params\": {
      \"child_subscription_ids\": [\"$MOD_CHILD_ID\"]
    }
  }"

curl -s "http://localhost:8080/v1/subscriptions/$MOD_CHILD_ID" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" | jq '{subscription_type, parent_subscription_id}'
```

Expected: `subscription_type=standalone`, `parent_subscription_id=null`.

---

## Task 5: E2E Phase 3 — Preview Invoice

Using `SUB_PARENT_ID` (TC 1.2 parent) which has `SUB_CHILD_GI_ID` attached (TC 1.3).

- [ ] **Step 1: TC 3.1 — Preview parent-only (no children)**

```bash
# Use SUB_STANDALONE (TC 1.1) as the control — a plain sub with no children
curl -s -X POST http://localhost:8080/v1/invoices/preview \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"subscription_id\": \"$SUB_STANDALONE\"}" | jq '{line_items_count: (.line_items | length), subtotal, amount_due}'
```

Expected: line items from parent's own plan only.

- [ ] **Step 2: TC 3.2 — Preview clubbed invoice (parent + child)**

```bash
# SUB_PARENT_ID has SUB_CHILD_GI_ID attached from TC 1.3
curl -s -X POST http://localhost:8080/v1/invoices/preview \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"subscription_id\": \"$SUB_PARENT_ID\"}" | jq '{line_items_count: (.line_items | length), subtotal, amount_due}'
```

Expected: `line_items_count >= 2` (parent + child), `subtotal` = sum of both ($20 if both have $10 flat fee).

- [ ] **Step 3: TC 3.3 — Preview child's own invoice**

```bash
curl -s -X POST http://localhost:8080/v1/invoices/preview \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"subscription_id\": \"$SUB_CHILD_GI_ID\"}" | jq '{line_items_count: (.line_items | length), subtotal}'
```

Expected: child's own line items only (`line_items_count=1`, `subtotal=10`).

---

## Task 6: E2E Phase 4 — Validation Errors

Each step expects HTTP 400.

- [ ] **Step 1: TC 4.1 — grouped_invoicing without parent_subscription_id**

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_C1_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"monthly\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"inheritance\":{\"invoicing_behavior\":\"grouped_invoicing\"}}"
```

Expected: `400`.

- [ ] **Step 2: TC 4.2 — delegated without invoicing_customer_external_id**

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_C1_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"monthly\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"inheritance\":{\"invoicing_behavior\":\"delegated\"}}"
```

Expected: `400`.

- [ ] **Step 3: TC 4.3 — parent with parent_subscription_id set**

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_P_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"monthly\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"inheritance\":{\"invoicing_behavior\":\"parent\",\"parent_subscription_id\":\"some-id\"}}"
```

Expected: `400`.

- [ ] **Step 4: TC 4.4 — Add child with wrong billing period**

```bash
# Create a yearly standalone child
WRONG_PERIOD_CHILD=$(curl -s -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_C2_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"annual\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"collection_method\":\"send_invoice\"}")
WRONG_PERIOD_CHILD_ID=$(echo $WRONG_PERIOD_CHILD | jq -r '.id')

curl -s -w "\nHTTP:%{http_code}" -X POST "http://localhost:8080/v1/subscriptions${SUB_PARENT_ID}/modify/execute" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"type\":\"grouped_invoicing_add\",\"grouped_invoicing_params\":{\"parent_subscription_id\":\"$SUB_PARENT_ID\",\"child_subscription_ids\":[\"$WRONG_PERIOD_CHILD_ID\"]}}"
```

Expected: HTTP `400`, error about billing period mismatch.

- [ ] **Step 5: TC 4.5 — Add already-grouped child**

```bash
# SUB_CHILD_GI_ID is already grouped under SUB_PARENT_ID
curl -s -w "\nHTTP:%{http_code}" -X POST "http://localhost:8080/v1/subscriptions${SUB_PARENT_ID}/modify/execute" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"type\":\"grouped_invoicing_add\",\"grouped_invoicing_params\":{\"parent_subscription_id\":\"$SUB_PARENT_ID\",\"child_subscription_ids\":[\"$SUB_CHILD_GI_ID\"]}}"
```

Expected: HTTP `400`, error about child must be standalone.

- [ ] **Step 6: TC 4.6 — Remove standalone (not grouped)**

```bash
# MOD_CHILD_ID was reset to standalone by TC 2.4
curl -s -w "\nHTTP:%{http_code}" -X POST "http://localhost:8080/v1/subscriptions${SUB_PARENT_ID}/modify/execute" \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"type\":\"grouped_invoicing_remove\",\"grouped_invoicing_params\":{\"child_subscription_ids\":[\"$MOD_CHILD_ID\"]}}"
```

Expected: HTTP `400`, error about not of type grouped_invoicing.

- [ ] **Step 7: TC 4.7 — standalone with sub_ids_for_grouped_invoicing**

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/v1/subscriptions \
  -H "x-api-key: sk_01KR3PD8EKA6AF9S6E1K29XH5T" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\":\"$CUST_P_ID\",\"plan_id\":\"$PLAN_ID\",\"currency\":\"usd\",\"billing_period\":\"monthly\",\"billing_period_count\":1,\"billing_cadence\":\"recurring\",\"billing_cycle\":\"anniversary\",\"inheritance\":{\"invoicing_behavior\":\"standalone\",\"sub_ids_for_grouped_invoicing\":[\"some-id\"]}}"
```

Expected: `400`.

---

## Task 7: Service-Layer Tests — Create-Time Grouped Invoicing

**Files:**
- Create: `internal/service/subscription_create_grouped_test.go`

- [ ] **Step 1: Write the test file**

```go
package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type SubscriptionCreateGroupedTestSuite struct {
	testutil.BaseServiceTestSuite
	service SubscriptionService
	now     time.Time
	plan    *plan.Plan
	custP   *customer.Customer // parent customer
	custC1  *customer.Customer // child 1
	custC2  *customer.Customer // child 2
}

func TestSubscriptionCreateGroupedSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionCreateGroupedTestSuite))
}

func (s *SubscriptionCreateGroupedTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	ctx := s.GetContext()
	s.now = time.Now().UTC().Truncate(time.Second)

	params := ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		UserRepo:                     s.GetStores().UserRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:              s.GetStores().SubscriptionScheduleRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		SecretRepo:                   s.GetStores().SecretRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		TaskRepo:                     s.GetStores().TaskRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		CreditNoteRepo:               s.GetStores().CreditNoteRepo,
		CreditNoteLineItemRepo:       s.GetStores().CreditNoteLineItemRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		FeatureUsageRepo:             s.GetStores().FeatureUsageRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
	}
	s.service = NewSubscriptionService(params)

	// plan
	s.plan = &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "GI Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PlanRepo.Create(ctx, s.plan))

	// flat-fee price on the plan
	p := &price.Price{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		PlanID:             lo.ToPtr(s.plan.ID),
		Amount:             lo.ToPtr("10"),
		Currency:           "usd",
		Type:               types.PRICE_TYPE_FLAT_FEE,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingModel:       types.BILLING_MODEL_FLAT_FEE,
		InvoiceCadence:     types.InvoiceCadenceArrear,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().PriceRepo.Create(ctx, p))

	makeCustomer := func(name, extID string) *customer.Customer {
		c := &customer.Customer{
			ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
			Name:       name,
			Email:      extID + "@test.com",
			ExternalID: extID,
			BaseModel:  types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().CustomerRepo.Create(ctx, c))
		return c
	}
	s.custP = makeCustomer("Parent", "gi-svc-parent")
	s.custC1 = makeCustomer("Child1", "gi-svc-child1")
	s.custC2 = makeCustomer("Child2", "gi-svc-child2")
}

func (s *SubscriptionCreateGroupedTestSuite) baseReq(custID string) dto.CreateSubscriptionRequest {
	return dto.CreateSubscriptionRequest{
		CustomerID:         custID,
		PlanID:             s.plan.ID,
		Currency:           "usd",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCadence:     types.BILLING_CADENCE_RECURRING,
		BillingCycle:       types.BillingCycleAnniversary,
		StartDate:          lo.ToPtr(s.now),
		CollectionMethod:   lo.ToPtr(types.CollectionMethodSendInvoice),
	}
}

// TC 5.1 — grouped_invoicing child created at subscription creation time
func (s *SubscriptionCreateGroupedTestSuite) TestCreateSubscription_GroupedInvoicingChild() {
	ctx := s.GetContext()

	parentReq := s.baseReq(s.custP.ID)
	parentReq.Inheritance = &dto.SubscriptionInheritanceConfig{InvoicingBehavior: types.SubscriptionTypeParent}
	parentResp, err := s.service.CreateSubscription(ctx, parentReq)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeParent, parentResp.SubscriptionType)

	childReq := s.baseReq(s.custC1.ID)
	childReq.Inheritance = &dto.SubscriptionInheritanceConfig{
		InvoicingBehavior:    types.SubscriptionTypeGroupedInvoicing,
		ParentSubscriptionID: parentResp.ID,
	}
	childResp, err := s.service.CreateSubscription(ctx, childReq)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeGroupedInvoicing, childResp.SubscriptionType)
	s.Require().NotNil(childResp.ParentSubscriptionID)
	s.Equal(parentResp.ID, *childResp.ParentSubscriptionID)
}

// TC 5.2 — parent attaches existing standalones via SubIDsForGroupedInvoicing
func (s *SubscriptionCreateGroupedTestSuite) TestCreateSubscription_ParentAttachesExistingStandalones() {
	ctx := s.GetContext()

	sa1Resp, err := s.service.CreateSubscription(ctx, s.baseReq(s.custC1.ID))
	s.Require().NoError(err)
	sa2Resp, err := s.service.CreateSubscription(ctx, s.baseReq(s.custC2.ID))
	s.Require().NoError(err)

	parentReq := s.baseReq(s.custP.ID)
	parentReq.Inheritance = &dto.SubscriptionInheritanceConfig{
		InvoicingBehavior:         types.SubscriptionTypeParent,
		SubIDsForGroupedInvoicing: []string{sa1Resp.ID, sa2Resp.ID},
	}
	parentResp, err := s.service.CreateSubscription(ctx, parentReq)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeParent, parentResp.SubscriptionType)

	assertGrouped := func(subID, parentID string) {
		updated, err := s.GetStores().SubscriptionRepo.Get(ctx, subID)
		s.Require().NoError(err)
		s.Equal(types.SubscriptionTypeGroupedInvoicing, updated.SubscriptionType)
		s.Require().NotNil(updated.ParentSubscriptionID)
		s.Equal(parentID, *updated.ParentSubscriptionID)
	}
	assertGrouped(sa1Resp.ID, parentResp.ID)
	assertGrouped(sa2Resp.ID, parentResp.ID)
}

// TC 5.3 — delegated subscription resolves invoicing_customer_external_id to internal ID
func (s *SubscriptionCreateGroupedTestSuite) TestCreateSubscription_DelegatedSetsInvoicingCustomerID() {
	ctx := s.GetContext()

	req := s.baseReq(s.custC1.ID)
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		InvoicingBehavior:           types.SubscriptionTypeDelegated,
		InvoicingCustomerExternalID: lo.ToPtr(s.custP.ExternalID),
	}
	resp, err := s.service.CreateSubscription(ctx, req)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeDelegated, resp.SubscriptionType)

	raw, err := s.GetStores().SubscriptionRepo.Get(ctx, resp.ID)
	s.Require().NoError(err)
	s.Require().NotNil(raw.InvoicingCustomerID)
	s.Equal(s.custP.ID, *raw.InvoicingCustomerID)
}

// TC 5.1b — grouped_invoicing without parent_subscription_id is rejected
func (s *SubscriptionCreateGroupedTestSuite) TestCreateSubscription_GroupedInvoicingWithoutParent_Rejected() {
	ctx := s.GetContext()
	req := s.baseReq(s.custC1.ID)
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		InvoicingBehavior: types.SubscriptionTypeGroupedInvoicing,
		// ParentSubscriptionID intentionally omitted
	}
	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)
}

// TC 5.2b — delegated without invoicing_customer_external_id is rejected
func (s *SubscriptionCreateGroupedTestSuite) TestCreateSubscription_DelegatedWithoutInvoicingCustomer_Rejected() {
	ctx := s.GetContext()
	req := s.baseReq(s.custC1.ID)
	req.Inheritance = &dto.SubscriptionInheritanceConfig{
		InvoicingBehavior: types.SubscriptionTypeDelegated,
		// InvoicingCustomerExternalID intentionally omitted
	}
	_, err := s.service.CreateSubscription(ctx, req)
	s.Require().Error(err)
}
```

- [ ] **Step 2: Run tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/sweet-volhard-a68c59 && go test ./internal/service/... -run TestSubscriptionCreateGroupedSuite -v -count=1 -timeout 120s
```

Expected: 5/5 PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/service/subscription_create_grouped_test.go
git commit -m "test(subscription): add create-time grouped invoicing and delegated service tests"
```

---

## Task 8: Service-Layer Tests — Modify API

**Files:**
- Create: `internal/service/subscription_modification_grouped_test.go`

- [ ] **Step 1: Write the test file**

```go
package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/suite"
)

type SubscriptionModifyGroupedTestSuite struct {
	testutil.BaseServiceTestSuite
	modService SubscriptionModificationService
	subSvc     *subscriptionService
	now        time.Time
	custP      *customer.Customer
	custC1     *customer.Customer
	parentSub  *subscription.Subscription
	childSub   *subscription.Subscription
}

func TestSubscriptionModifyGroupedSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionModifyGroupedTestSuite))
}

func (s *SubscriptionModifyGroupedTestSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	ctx := s.GetContext()
	s.now = time.Now().UTC().Truncate(time.Second)

	params := ServiceParams{
		Logger:                       s.GetLogger(),
		Config:                       s.GetConfig(),
		DB:                           s.GetDB(),
		TaxAssociationRepo:           s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                  s.GetStores().TaxRateRepo,
		AuthRepo:                     s.GetStores().AuthRepo,
		UserRepo:                     s.GetStores().UserRepo,
		EventRepo:                    s.GetStores().EventRepo,
		MeterRepo:                    s.GetStores().MeterRepo,
		PriceRepo:                    s.GetStores().PriceRepo,
		CustomerRepo:                 s.GetStores().CustomerRepo,
		PlanRepo:                     s.GetStores().PlanRepo,
		SubRepo:                      s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:     s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:        s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:              s.GetStores().SubscriptionScheduleRepo,
		WalletRepo:                   s.GetStores().WalletRepo,
		InvoiceLineItemRepo:          s.GetStores().InvoiceLineItemRepo,
		TenantRepo:                   s.GetStores().TenantRepo,
		InvoiceRepo:                  s.GetStores().InvoiceRepo,
		FeatureRepo:                  s.GetStores().FeatureRepo,
		EntitlementRepo:              s.GetStores().EntitlementRepo,
		PaymentRepo:                  s.GetStores().PaymentRepo,
		SecretRepo:                   s.GetStores().SecretRepo,
		EnvironmentRepo:              s.GetStores().EnvironmentRepo,
		TaskRepo:                     s.GetStores().TaskRepo,
		CreditGrantRepo:              s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo:   s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                   s.GetStores().CouponRepo,
		CouponAssociationRepo:        s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:        s.GetStores().CouponApplicationRepo,
		AddonAssociationRepo:         s.GetStores().AddonAssociationRepo,
		TaxAppliedRepo:               s.GetStores().TaxAppliedRepo,
		CreditNoteRepo:               s.GetStores().CreditNoteRepo,
		CreditNoteLineItemRepo:       s.GetStores().CreditNoteLineItemRepo,
		ConnectionRepo:               s.GetStores().ConnectionRepo,
		EntityIntegrationMappingRepo: s.GetStores().EntityIntegrationMappingRepo,
		SettingsRepo:                 s.GetStores().SettingsRepo,
		AlertLogsRepo:                s.GetStores().AlertLogsRepo,
		FeatureUsageRepo:             s.GetStores().FeatureUsageRepo,
		EventPublisher:               s.GetPublisher(),
		WebhookPublisher:             s.GetWebhookPublisher(),
		ProrationCalculator:          s.GetCalculator(),
		IntegrationFactory:           s.GetIntegrationFactory(),
	}
	s.modService = NewSubscriptionModificationService(params)
	s.subSvc = NewSubscriptionService(params).(*subscriptionService)

	makeCustomer := func(name, extID string) *customer.Customer {
		c := &customer.Customer{
			ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
			Name:       name,
			Email:      extID + "@test.com",
			ExternalID: extID,
			BaseModel:  types.GetDefaultBaseModel(ctx),
		}
		s.NoError(s.GetStores().CustomerRepo.Create(ctx, c))
		return c
	}
	s.custP = makeCustomer("ModParent", "mod-gi-parent")
	s.custC1 = makeCustomer("ModChild1", "mod-gi-child1")

	anchor := s.now
	s.parentSub = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         s.custP.ID,
		PlanID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeParent,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingAnchor:      anchor,
		StartDate:          anchor,
		CurrentPeriodStart: anchor,
		CurrentPeriodEnd:   anchor.AddDate(0, 1, 0),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.parentSub))

	s.childSub = &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         s.custC1.ID,
		PlanID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Currency:           "usd",
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingAnchor:      anchor,
		StartDate:          anchor,
		CurrentPeriodStart: anchor,
		CurrentPeriodEnd:   anchor.AddDate(0, 1, 0),
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().SubscriptionRepo.Create(ctx, s.childSub))
}

// TC 5.4 — Preview add returns changed subscriptions without writing
func (s *SubscriptionModifyGroupedTestSuite) TestModifySubscription_GroupedInvoicingAddPreview() {
	ctx := s.GetContext()

	req := &dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
		GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
			ParentSubscriptionID: s.parentSub.ID,
			ChildSubscriptionIDs: []string{s.childSub.ID},
		},
	}
	result, err := s.modService.Preview(ctx, s.parentSub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Len(result.ChangedResources.Subscriptions, 1)

	// no write
	unchanged, err := s.GetStores().SubscriptionRepo.Get(ctx, s.childSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeStandalone, unchanged.SubscriptionType)
	s.Nil(unchanged.ParentSubscriptionID)
}

// TC 5.5 — Execute add flips child to grouped_invoicing and sets parent link
func (s *SubscriptionModifyGroupedTestSuite) TestModifySubscription_GroupedInvoicingAddExecute() {
	ctx := s.GetContext()

	req := &dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeGroupedInvoicingAdd,
		GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
			ParentSubscriptionID: s.parentSub.ID,
			ChildSubscriptionIDs: []string{s.childSub.ID},
		},
	}
	result, err := s.modService.Execute(ctx, s.parentSub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(result)

	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, s.childSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeGroupedInvoicing, updated.SubscriptionType)
	s.Require().NotNil(updated.ParentSubscriptionID)
	s.Equal(s.parentSub.ID, *updated.ParentSubscriptionID)
}

// TC 5.6 — Execute remove resets child to standalone and clears parent link
func (s *SubscriptionModifyGroupedTestSuite) TestModifySubscription_GroupedInvoicingRemoveExecute() {
	ctx := s.GetContext()

	// First add the child
	s.NoError(s.subSvc.addToGroupedInvoicing(ctx, s.parentSub, s.childSub.ID))

	req := &dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeGroupedInvoicingRemove,
		GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
			ChildSubscriptionIDs: []string{s.childSub.ID},
		},
	}
	result, err := s.modService.Execute(ctx, s.parentSub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(result)

	updated, err := s.GetStores().SubscriptionRepo.Get(ctx, s.childSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeStandalone, updated.SubscriptionType)
	s.Nil(updated.ParentSubscriptionID)
}

// TC 5.6b — Preview remove returns changed subscriptions without writing
func (s *SubscriptionModifyGroupedTestSuite) TestModifySubscription_GroupedInvoicingRemovePreview() {
	ctx := s.GetContext()

	// First add the child
	s.NoError(s.subSvc.addToGroupedInvoicing(ctx, s.parentSub, s.childSub.ID))

	req := &dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeGroupedInvoicingRemove,
		GroupedInvoicingParams: &dto.SubModifyGroupedInvoicingParams{
			ChildSubscriptionIDs: []string{s.childSub.ID},
		},
	}
	result, err := s.modService.Preview(ctx, s.parentSub.ID, req)
	s.Require().NoError(err)
	s.Len(result.ChangedResources.Subscriptions, 1)

	// no write — still grouped
	unchanged, err := s.GetStores().SubscriptionRepo.Get(ctx, s.childSub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeGroupedInvoicing, unchanged.SubscriptionType)
}
```

- [ ] **Step 2: Run tests**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/sweet-volhard-a68c59 && go test ./internal/service/... -run TestSubscriptionModifyGroupedSuite -v -count=1 -timeout 120s
```

Expected: 4/4 PASS.

- [ ] **Step 3: Run full suite**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice/.claude/worktrees/sweet-volhard-a68c59 && go test ./internal/service/... -count=1 -timeout 120s
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/service/subscription_modification_grouped_test.go
git commit -m "test(subscription): add modify grouped invoicing preview/execute service tests"
```

---

## Self-Review Checklist

- [x] **Spec coverage:** All 5 phases from spec covered: fix (Task 1), setup (Task 2), Phase 1 (Task 3), Phase 2 (Task 4), Phase 3 (Task 5), Phase 4 (Task 6), Phase 5 split into Tasks 7+8
- [x] **No placeholders:** All curl commands include exact headers, bodies, variable names
- [x] **Type consistency:** `types.SubscriptionTypeGroupedInvoicing`, `dto.SubscriptionModifyTypeGroupedInvoicingAdd/Remove`, `dto.SubModifyGroupedInvoicingParams` used consistently throughout
- [x] **Route correctness:** Modify routes use `${SUB_PARENT_ID}` (no slash between base and id — router registers `:id/modify/execute` without leading slash)
