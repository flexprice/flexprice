# Invoice Generation Flow

This document describes the invoice generation flow in Flexprice, covering subscription invoices (sync + async paths), one-off/credit invoices, the `ComputeInvoice` internal flow, and the invoice status lifecycle.

---

## 1. Subscription Invoice — Sync Path

The synchronous path used when creating a subscription invoice via `CreateSubscriptionInvoice`.

```mermaid
flowchart TB
    subgraph Entry["Sync Path Entry"]
        API["API Handler / Subscription Service"]
    end

    subgraph CSI["CreateSubscriptionInvoice()"]
        direction TB
        CSI_SIG["invoiceService.CreateSubscriptionInvoice(
            ctx context.Context,
            req *dto.CreateSubscriptionInvoiceRequest,
            paymentParams *dto.PaymentParameters,
            flowType types.InvoiceFlowType,
            isDraftSubscription bool,
        ) (*dto.InvoiceResponse, *subscription.Subscription, error)"]

        VAL["1. Validate request"]
        GET_SUB["2. SubRepo.GetWithLineItems(ctx, req.SubscriptionID)"]
        CHECK_DRAFT["3. Reject if sub is DRAFT && !isDraftSubscription"]
        CALL_DRAFT["4. CreateDraftInvoiceForSubscription()"]
        CALL_COMPUTE["5. ComputeInvoice(ctx, draft.ID, nil)"]
        SKIP_CHECK{"skipped?"}
        CALL_PROC["6. ProcessDraftInvoice(ctx, draft.ID, paymentParams, sub, flowType)
        ⚠ checks LastComputedAt != nil"]
        REFETCH["7. InvoiceRepo.Get(ctx, draft.ID)"]
        RET_NIL["return (nil, subscription, nil)"]
        RET_INV["return (InvoiceResponse, subscription, nil)"]

        CSI_SIG --> VAL --> GET_SUB --> CHECK_DRAFT --> CALL_DRAFT --> CALL_COMPUTE --> SKIP_CHECK
        SKIP_CHECK -->|Yes| RET_NIL
        SKIP_CHECK -->|No| CALL_PROC --> REFETCH --> RET_INV
    end

    subgraph CDIFS["CreateDraftInvoiceForSubscription()"]
        direction TB
        CDIFS_SIG["invoiceService.CreateDraftInvoiceForSubscription(
            ctx, subscriptionID string,
            periodStart, periodEnd time.Time,
            referencePoint types.InvoiceReferencePoint,
        ) (*dto.InvoiceResponse, error)"]

        CDIFS_GET["Get subscription"]
        CDIFS_BUILD["Build CreateInvoiceRequest{
            CustomerID, SubscriptionID,
            InvoiceType: SUBSCRIPTION,
            Currency, BillingPeriod,
            AmountDue/Total/Subtotal: 0,
            PeriodStart, PeriodEnd,
            BillingReason: SUBSCRIPTION_CYCLE
        }"]
        CDIFS_CALL["Delegate → CreateDraftInvoice(ctx, req)"]

        CDIFS_SIG --> CDIFS_GET --> CDIFS_BUILD --> CDIFS_CALL
    end

    subgraph CDI["CreateDraftInvoice()"]
        direction TB
        CDI_SIG["invoiceService.CreateDraftInvoice(
            ctx context.Context,
            req dto.CreateInvoiceRequest,
        ) (*dto.InvoiceResponse, error)"]

        CDI_FORCE["Force: SkipInvoiceNumber=true,
        SuppressWebhook=true, Status=DRAFT"]
        CDI_TX["BEGIN TRANSACTION"]
        CDI_IDEMP["Generate idempotency key"]
        CDI_CHECK["GetByIdempotencyKey()"]
        CDI_EXISTS{"Exists?"}
        CDI_EXIST_DRAFT{"Status=DRAFT?"}
        CDI_RETURN_EXIST["Return existing draft"]
        CDI_ERR_EXIST["Error: already exists"]
        CDI_SEQ["GetNextBillingSequence()"]
        CDI_BUILD["Convert to domain Invoice"]
        CDI_VALIDATE["inv.Validate()"]
        CDI_CREATE["CreateWithLineItems()"]
        CDI_COMMIT["COMMIT"]

        CDI_SIG --> CDI_FORCE --> CDI_TX --> CDI_IDEMP --> CDI_CHECK --> CDI_EXISTS
        CDI_EXISTS -->|Yes| CDI_EXIST_DRAFT
        CDI_EXIST_DRAFT -->|Yes| CDI_RETURN_EXIST
        CDI_EXIST_DRAFT -->|No| CDI_ERR_EXIST
        CDI_EXISTS -->|No| CDI_SEQ --> CDI_BUILD --> CDI_VALIDATE --> CDI_CREATE --> CDI_COMMIT
    end

    subgraph PROC["ProcessDraftInvoice()"]
        direction TB
        PROC_SIG["invoiceService.ProcessDraftInvoice(
            ctx, id string,
            paymentParams *dto.PaymentParameters,
            sub *subscription.Subscription,
            flowType types.InvoiceFlowType,
        ) error"]

        PROC_GET["Get invoice, check status=DRAFT"]
        PROC_GUARD["⚠ Guard: LastComputedAt != nil"]
        PROC_FIN["performFinalizeInvoiceActions()
        → Status=FINALIZED, FinalizedAt=now
        → Webhook: InvoiceUpdateFinalized"]
        PROC_STRIPE["syncInvoiceToStripeIfEnabled()"]
        PROC_RAZOR["syncInvoiceToRazorpayIfEnabled()"]
        PROC_MOYASAR["triggerMoyasarInvoiceSyncWorkflow()"]
        PROC_HUBSPOT["triggerHubSpotInvoiceSyncWorkflow()"]
        PROC_CHARGEBEE["syncInvoiceToChargebeeIfEnabled()"]
        PROC_QB["syncInvoiceToQuickBooksIfEnabled()"]
        PROC_NOMOD["triggerNomodInvoiceSyncWorkflow()"]
        PROC_PADDLE["triggerPaddleInvoiceSyncWorkflow()"]
        PROC_PAY["attemptPaymentForSubscriptionInvoice()"]

        PROC_SIG --> PROC_GET --> PROC_GUARD --> PROC_FIN
        PROC_FIN --> PROC_STRIPE --> PROC_RAZOR --> PROC_MOYASAR
        PROC_MOYASAR --> PROC_HUBSPOT --> PROC_CHARGEBEE --> PROC_QB
        PROC_QB --> PROC_NOMOD --> PROC_PADDLE --> PROC_PAY
    end

    API --> CSI_SIG
    CALL_DRAFT -.-> CDIFS_SIG
    CDIFS_CALL -.-> CDI_SIG
    CALL_PROC -.-> PROC_SIG
```

---

## 2. Subscription Invoice — Async Path (Temporal Workflows)

The asynchronous path used for periodic billing via Temporal.

```mermaid
flowchart TB
    subgraph CRON["Trigger: Cron / Scheduler"]
        TRIGGER["ProcessSubscriptionBillingWorkflow triggered"]
    end

    subgraph PSBW["ProcessSubscriptionBillingWorkflow"]
        direction TB
        PSBW_IN["Input: ProcessSubscriptionBillingWorkflowInput{
            SubscriptionID, TenantID,
            EnvironmentID, UserID
        }"]

        S1["Step 1: CheckDraftSubscriptionActivity"]
        S1_CHECK{"isDraft?"}
        S1_EXIT["Exit workflow"]

        S2["Step 2: CalculatePeriodsActivity
        → returns []Period"]

        S3["Step 3: CreateDraftInvoicesActivity
        → for each period (except last):
          invoiceService.CreateDraftInvoiceForSubscription()
        → returns InvoiceIDs[]"]

        S4["Step 4: UpdateCurrentPeriodActivity
        → set subscription to last period"]

        S5["Step 5: CheckCancellationActivity"]
        S6["Step 6: ProcessPendingPlanChangesActivity"]

        S7["Step 7: TriggerInvoiceWorkflowActivity
        → for each invoiceID: fire-and-forget
          ProcessInvoiceWorkflow"]

        PSBW_IN --> S1 --> S1_CHECK
        S1_CHECK -->|Yes| S1_EXIT
        S1_CHECK -->|No| S2 --> S3 --> S4 --> S5 --> S6 --> S7
    end

    subgraph PIW["ProcessInvoiceWorkflow (per invoice)"]
        direction TB
        PIW_IN["Input: ProcessInvoiceWorkflowInput{
            InvoiceID, TenantID,
            EnvironmentID, UserID
        }"]

        A0["Step 0: ComputeInvoiceActivity
        → invoiceService.ComputeInvoice(ctx, invoiceID, nil)
        → sets LastComputedAt
        → returns {Skipped: bool}"]
        A0_CHECK{"Skipped?"}
        A0_EXIT["Exit workflow (no-op)"]

        A1["Step 1: FinalizeInvoiceActivity
        → invoiceService.FinalizeInvoice(ctx, invoiceID)"]

        A2["Step 2: SyncInvoiceToVendorActivity
        → invoiceService.SyncInvoiceToExternalVendors(ctx, invoiceID)
        → Stripe, Razorpay, Chargebee, QuickBooks,
          HubSpot, Nomod, Paddle, Moyasar"]

        A3["Step 3: AttemptInvoicePaymentActivity
        → invoiceService.AttemptPayment(ctx, invoiceID)"]

        PIW_IN --> A0 --> A0_CHECK
        A0_CHECK -->|Yes| A0_EXIT
        A0_CHECK -->|No| A1 --> A2 --> A3
    end

    TRIGGER --> PSBW_IN
    S7 -.->|fire-and-forget| PIW_IN
```

---

## 3. ComputeInvoice — Detailed Internal Flow

Shows both Branch A (subscription) and Branch B (one-off/credit) with the `LastComputedAt` timestamp.

```mermaid
flowchart TB
    START["ComputeInvoice(
        ctx context.Context,
        invoiceID string,
        req *dto.CreateInvoiceRequest,
    ) (skipped bool, err error)"]

    TX["BEGIN TRANSACTION"]
    LOCK["InvoiceRepo.GetForUpdate(txCtx, invoiceID)
    → SELECT ... FOR UPDATE (row lock)"]

    IDEMP_CHECK{"inv.InvoiceNumber != nil
    OR inv.InvoiceStatus != DRAFT?"}
    IDEMP_RET["Return early (idempotent)
    skipped = (status == SKIPPED)"]

    TYPE_CHECK{"inv.InvoiceType ==
    SUBSCRIPTION?"}

    subgraph SUB_BRANCH["Branch A: Subscription Invoice"]
        direction TB
        SUB_GET["SubRepo.GetWithLineItems(txCtx, *inv.SubscriptionID)"]
        SUB_REF["Determine referencePoint:
        PRORATION → ReferencePointCancel
        else → ReferencePointPeriodEnd"]
        SUB_BILLING["billingService.PrepareSubscriptionInvoiceRequest(
            txCtx, sub, *inv.PeriodStart, *inv.PeriodEnd, refPoint,
        ) → *dto.CreateInvoiceRequest with computed line items"]
        SUB_TOTAL["total = applyReq.Subtotal"]
        SUB_REMOVE["Remove existing line items"]
        SUB_ADD["Add new computed line items"]
        SUB_UPDATE["Update: Subtotal, Total, AmountDue,
        AmountRemaining, Description, DueDate"]

        SUB_GET --> SUB_REF --> SUB_BILLING --> SUB_TOTAL --> SUB_REMOVE --> SUB_ADD --> SUB_UPDATE
    end

    subgraph ONEOFF_BRANCH["Branch B: One-Off / Credit Invoice"]
        direction TB
        ONEOFF_TOTAL["total = inv.Total (pre-set at draft creation)"]
        ONEOFF_REQ["applyReq = req (caller-provided coupons/taxes)"]
        ONEOFF_TOTAL --> ONEOFF_REQ
    end

    ZERO_CHECK{"total.IsZero()?"}

    subgraph SKIP_FLOW["Zero-Dollar → SKIPPED"]
        direction TB
        SKIP_COMPUTED["inv.LastComputedAt = now"]
        SKIP_STATUS["inv.InvoiceStatus = SKIPPED"]
        SKIP_UPDATE["InvoiceRepo.Update(txCtx, inv)"]
        SKIP_RET["Return (skipped=true, nil)"]
        SKIP_COMPUTED --> SKIP_STATUS --> SKIP_UPDATE --> SKIP_RET
    end

    subgraph ASSIGN_FLOW["Non-Zero → Assign Number & Apply Adjustments"]
        direction TB
        GET_NUMBER["GetNextInvoiceNumber(txCtx, &invoiceConfig)
        → e.g. INV-202501-00001"]
        SET_NUMBER["inv.InvoiceNumber = &invoiceNumber"]

        APPLY_CHECK{"applyReq != nil?"}

        CREDITS["applyCreditsAndCouponsToInvoice(txCtx, inv, *applyReq)
        → Coupons: TotalDiscount
        → Credits: TotalPrepaidCreditsApplied
        → Total = Subtotal - Discount - Credits"]

        TAXES["applyTaxesToInvoice(txCtx, inv, *applyReq)
        → TotalTax
        → Total = Subtotal - Discount - Credits + Tax"]

        SET_COMPUTED["inv.LastComputedAt = now"]
        FINAL_UPDATE["InvoiceRepo.Update(txCtx, inv)"]

        GET_NUMBER --> SET_NUMBER --> APPLY_CHECK
        APPLY_CHECK -->|Yes| CREDITS --> TAXES --> SET_COMPUTED --> FINAL_UPDATE
        APPLY_CHECK -->|No| SET_COMPUTED --> FINAL_UPDATE
    end

    COMMIT["COMMIT TRANSACTION"]
    RET["Return (skipped=false, nil)"]

    START --> TX --> LOCK --> IDEMP_CHECK
    IDEMP_CHECK -->|Yes| IDEMP_RET
    IDEMP_CHECK -->|No| TYPE_CHECK
    TYPE_CHECK -->|Subscription| SUB_GET
    TYPE_CHECK -->|One-Off/Credit| ONEOFF_TOTAL
    SUB_UPDATE --> ZERO_CHECK
    ONEOFF_REQ --> ZERO_CHECK
    ZERO_CHECK -->|Yes| SKIP_COMPUTED
    ZERO_CHECK -->|No| GET_NUMBER
    FINAL_UPDATE --> COMMIT --> RET
```

---

## 4. One-Off / Credit Invoice Flow

The `CreateOneOffInvoice` entry point for creating one-off and credit invoices.

```mermaid
flowchart TB
    API["POST /invoices → InvoiceHandler.CreateOneOffInvoice(c *gin.Context)"]

    subgraph SVC["invoiceService.CreateOneOffInvoice(ctx, req dto.CreateInvoiceRequest)"]
        direction TB

        COUPON_VAL["Step 1: Validate Coupons
        for each req.Coupons:
          CouponRepo.Get(couponID)
          CouponValidationService.ValidateCoupon()
        → req.InvoiceCoupons = valid coupons"]

        TAX_PREP["Step 2: Prepare Tax Rates"]
        TAX_BRANCH{"TaxRateOverrides?"}
        TAX_OVER["TaxService.PrepareTaxRatesForInvoice(req)"]
        TAX_IDS{"TaxRate IDs?"}
        TAX_GET["For each: TaxService.GetTaxRate(id)"]
        TAX_NONE["No tax rates"]
        TAX_SET["req.PreparedTaxRates = finalTaxRates"]

        DRAFT["Step 3: CreateDraftInvoice(ctx, req)
        → DRAFT invoice with line items
        → AmountDue/Total/Subtotal = req values
        → No invoice number yet"]

        COMPUTE["Step 4: ComputeInvoice(ctx, draft.ID, &req)
        Branch B: total = inv.Total (pre-set)
        → Assign invoice number
        → applyCreditsAndCouponsToInvoice()
        → applyTaxesToInvoice()
        → set LastComputedAt = now"]

        SKIP_CHECK{"skipped? (zero-dollar)"}

        SKIP_RET["Return SKIPPED invoice
        (no finalization, no vendor sync)"]

        FINALIZE["Step 5: FinalizeInvoice(ctx, draft.ID)
        → Status = FINALIZED
        → FinalizedAt = now"]

        WEBHOOK["publishInternalWebhookEvent(
        WebhookEventInvoiceUpdateFinalized)"]

        RETURN["Return InvoiceResponse (FINALIZED)"]

        COUPON_VAL --> TAX_PREP --> TAX_BRANCH
        TAX_BRANCH -->|Yes| TAX_OVER --> TAX_SET
        TAX_BRANCH -->|No| TAX_IDS
        TAX_IDS -->|Yes| TAX_GET --> TAX_SET
        TAX_IDS -->|No| TAX_NONE --> TAX_SET
        TAX_SET --> DRAFT --> COMPUTE --> SKIP_CHECK
        SKIP_CHECK -->|Yes| SKIP_RET
        SKIP_CHECK -->|No| FINALIZE --> WEBHOOK --> RETURN
    end

    API --> COUPON_VAL
```

### Key Differences: One-Off/Credit vs Subscription

| Aspect | ONE_OFF / CREDIT | SUBSCRIPTION |
|--------|-----------------|--------------|
| Total source | Pre-set in request | Computed from subscription usage |
| ComputeInvoice branch | Branch B (uses `inv.Total`) | Branch A (calls `PrepareSubscriptionInvoiceRequest`) |
| Coupons/Taxes | From request (`PreparedTaxRates`, `InvoiceCoupons`) | From billing service |
| After compute | `FinalizeInvoice()` directly | `ProcessDraftInvoice()` (finalize + sync + payment) |
| Vendor sync | None | Stripe, Razorpay, Chargebee, QuickBooks, etc. |
| Auto-payment | None | `attemptPaymentForSubscriptionInvoice()` |
| Negative amounts | CREDIT type allows negative `AmountDue` | Not applicable |

---

## 5. OneOff vs Subscription — Comparison

```mermaid
flowchart TD
    A["Invoice Created"] --> B{"Invoice Type?"}

    B -->|SUBSCRIPTION| C["ComputeInvoice — Branch A"]
    C --> C1["Fetch subscription + line items"]
    C1 --> C2["billingService.PrepareSubscriptionInvoiceRequest()
    → computes line items from usage"]
    C2 --> C3["total = computed subtotal"]
    C3 --> D["Assign number, apply coupons/taxes, set LastComputedAt"]
    D --> E["ProcessDraftInvoice (checks LastComputedAt != nil)"]
    E --> E1["FinalizeInvoice"]
    E1 --> E2["Sync to vendors: Stripe, Razorpay, Chargebee, etc."]
    E2 --> E3["attemptPaymentForSubscriptionInvoice"]

    B -->|ONE_OFF / CREDIT| F["ComputeInvoice — Branch B"]
    F --> F1["total = inv.Total (pre-set in request)"]
    F1 --> D2["Assign number, apply coupons/taxes, set LastComputedAt"]
    D2 --> G["FinalizeInvoice (directly, NO ProcessDraftInvoice)"]
    G --> H["Return — no vendor sync, no auto-payment"]
```

---

## 6. Key DTO Structs

```mermaid
classDiagram
    class CreateInvoiceRequest {
        +*string InvoiceNumber
        +string CustomerID
        +*string SubscriptionID
        +*string IdempotencyKey
        +InvoiceType InvoiceType
        +string Currency
        +Decimal AmountDue
        +Decimal Total
        +Decimal Subtotal
        +string Description
        +*time.Time DueDate
        +*string BillingPeriod
        +*time.Time PeriodStart
        +*time.Time PeriodEnd
        +InvoiceBillingReason BillingReason
        +*InvoiceStatus InvoiceStatus
        +*PaymentStatus PaymentStatus
        +[]CreateInvoiceLineItemRequest LineItems
        +[]InvoiceCoupon InvoiceCoupons
        +[]*TaxRateOverride TaxRateOverrides
        +[]*TaxRateResponse PreparedTaxRates
        +Metadata Metadata
        +bool SkipInvoiceNumber
        +bool SuppressWebhook
    }

    class CreateSubscriptionInvoiceRequest {
        +string SubscriptionID
        +time.Time PeriodStart
        +time.Time PeriodEnd
        +bool IsPreview
        +InvoiceReferencePoint ReferencePoint
        +Validate() error
    }

    class PaymentParameters {
        +*CollectionMethod CollectionMethod
        +*PaymentBehavior PaymentBehavior
        +*string PaymentMethodID
    }

    class InvoiceResponse {
        +string ID
        +string CustomerID
        +*string SubscriptionID
        +*string InvoiceNumber
        +InvoiceType InvoiceType
        +InvoiceStatus InvoiceStatus
        +PaymentStatus PaymentStatus
        +Decimal AmountDue
        +Decimal AmountPaid
        +Decimal AmountRemaining
        +Decimal Total
        +Decimal Subtotal
        +Decimal TotalTax
        +Decimal TotalDiscount
        +Decimal TotalPrepaidCreditsApplied
        +*time.Time LastComputedAt
        +[]InvoiceLineItemResponse LineItems
    }

    class ComputeInvoiceActivityInput {
        +string InvoiceID
        +string TenantID
        +string EnvironmentID
        +string UserID
    }

    class ComputeInvoiceActivityOutput {
        +bool Skipped
    }

    CreateSubscriptionInvoiceRequest --> CreateInvoiceRequest : builds via\nPrepareSubscriptionInvoiceRequest
    CreateInvoiceRequest --> InvoiceResponse : creates
    ComputeInvoiceActivityInput --> ComputeInvoiceActivityOutput : produces
```

---

## 7. Invoice Status Lifecycle

```mermaid
stateDiagram-v2
    [*] --> DRAFT : CreateDraftInvoice()

    DRAFT --> SKIPPED : ComputeInvoice() [total == 0, sets LastComputedAt]
    DRAFT --> DRAFT : ComputeInvoice() [total > 0, assigns InvoiceNumber, sets LastComputedAt]
    DRAFT --> FINALIZED : FinalizeInvoice() / performFinalizeInvoiceActions() [requires LastComputedAt != nil for ProcessDraftInvoice path]

    FINALIZED --> VOIDED : VoidInvoice()

    SKIPPED --> [*] : Terminal state
    VOIDED --> [*] : Terminal state
```
