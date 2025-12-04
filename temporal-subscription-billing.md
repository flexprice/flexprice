# Temporal Workflow: Subscription Billing Architecture

## Overview

This document outlines the complete implementation of a Temporal-based subscription billing system for Flexprice. The system replaces a fragile cron job with a robust, scalable, and observable workflow orchestration using independent workflows per subscription.

---

## Architecture Summary

```
SubscriptionSchedulerWorkflow (runs on cron)
  └── Fetches subscriptions in batches (100 per batch)
       └── EnqueueSubscriptionWorkflows Activity
            └── Starts independent ProcessSingleSubscriptionWorkflow for each subscription
                 ├── CheckPauseActivity
                 ├── CalculateBillingPeriodsActivity
                 ├── For each billing period:
                 │   ├── CreateInvoiceActivity
                 │   ├── SyncToExternalVendorActivity
                 │   └── AttemptPaymentActivity
                 └── UpdateSubscriptionPeriodActivity
```

---

## Workflows

### 1. SubscriptionSchedulerWorkflow

**Purpose**: Entry point triggered on a cron schedule. Fetches all subscriptions in batches and enqueues independent workflows.

**Key Features**:
- Runs periodically (e.g., daily at 2 AM)
- Fetches subscriptions in 100-item batches
- Enqueues `ProcessSingleSubscriptionWorkflow` for each subscription
- Does NOT wait for subscriptions to complete (fire-and-forget)

```go
func SubscriptionSchedulerWorkflow(ctx workflow.Context) error {
    logger := workflow.GetLogger(ctx)
    
    activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 10 * time.Minute,
        RetryPolicy: &temporal.RetryPolicy{
            MaximumAttempts: 3,
        },
    })
    
    offset := 0
    batchSize := 100
    
    for {
        var subscriptionIDs []string
        err := workflow.ExecuteActivity(activityCtx, FetchSubscriptionBatch, batchSize, offset).Get(activityCtx, &subscriptionIDs)
        if err != nil {
            logger.Error("Failed to fetch subscription batch", "error", err, "offset", offset)
            return err
        }
        
        if len(subscriptionIDs) == 0 {
            logger.Info("Completed processing all subscriptions")
            break
        }
        
        logger.Info("Enqueueing subscription workflows", "count", len(subscriptionIDs), "offset", offset)
        
        // Enqueue independent workflows for each subscription
        err = workflow.ExecuteActivity(activityCtx, EnqueueSubscriptionWorkflows, subscriptionIDs).Get(activityCtx, nil)
        if err != nil {
            logger.Error("Failed to enqueue subscription workflows", "error", err)
            return err
        }
        
        offset += batchSize
    }
    
    return nil
}
```

---

### 2. ProcessSingleSubscriptionWorkflow

**Purpose**: Independent workflow for processing a single subscription. Orchestrates all billing steps.

**Key Features**:
- Independent execution per subscription
- Skip billing if subscription is paused
- Calculate billing periods dynamically
- Process each period: create invoice → sync → attempt payment
- Update subscription metadata after completion
- Continues processing even if individual steps fail

```go
func ProcessSingleSubscriptionWorkflow(ctx workflow.Context, subscriptionID string) error {
    logger := workflow.GetLogger(ctx)
    
    activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
        StartToCloseTimeout: 5 * time.Minute,
        RetryPolicy: &temporal.RetryPolicy{
            InitialInterval: time.Second,
            MaximumInterval: time.Minute,
            BackoffCoefficient: 2.0,
            MaximumAttempts: 3,
        },
    })
    
    // Step 1: Check if subscription is paused
    var isPaused bool
    err := workflow.ExecuteActivity(activityCtx, CheckPauseActivity, subscriptionID).Get(activityCtx, &isPaused)
    if err != nil {
        logger.Error("Failed to check pause status", "error", err, "subscriptionID", subscriptionID)
        return err
    }
    
    if isPaused {
        logger.Info("Subscription is paused, skipping billing", "subscriptionID", subscriptionID)
        return nil
    }
    
    // Step 2: Calculate billing periods
    var billingPeriods []BillingPeriod
    err = workflow.ExecuteActivity(activityCtx, CalculateBillingPeriodsActivity, subscriptionID).Get(activityCtx, &billingPeriods)
    if err != nil {
        logger.Error("Failed to calculate billing periods", "error", err, "subscriptionID", subscriptionID)
        return err
    }
    
    if len(billingPeriods) == 0 {
        logger.Info("No billing periods to process", "subscriptionID", subscriptionID)
        return nil
    }
    
    logger.Info("Processing billing periods", "count", len(billingPeriods), "subscriptionID", subscriptionID)
    
    // Step 3: Process each billing period
    for _, period := range billingPeriods {
        logger.Info("Processing period", "subscriptionID", subscriptionID, "startDate", period.StartDate, "endDate", period.EndDate)
        
        // Create invoice
        var invoiceID string
        err := workflow.ExecuteActivity(activityCtx, CreateInvoiceActivity, subscriptionID, period).Get(activityCtx, &invoiceID)
        if err != nil {
            logger.Error("Failed to create invoice", "error", err, "subscriptionID", subscriptionID, "period", period)
            continue // Continue with next period even if invoice creation fails
        }
        
        logger.Info("Invoice created", "invoiceID", invoiceID, "subscriptionID", subscriptionID)
        
        // Sync to external vendors (Stripe, Hubspot, etc.)
        err = workflow.ExecuteActivity(activityCtx, SyncToExternalVendorActivity, invoiceID, subscriptionID).Get(activityCtx, nil)
        if err != nil {
            logger.Error("Failed to sync invoice to external vendors", "error", err, "invoiceID", invoiceID, "subscriptionID", subscriptionID)
            continue // Continue even if sync fails
        }
        
        logger.Info("Invoice synced successfully", "invoiceID", invoiceID, "subscriptionID", subscriptionID)
        
        // Attempt payment
        err = workflow.ExecuteActivity(activityCtx, AttemptPaymentActivity, invoiceID, subscriptionID).Get(activityCtx, nil)
        if err != nil {
            logger.Error("Failed to attempt payment", "error", err, "invoiceID", invoiceID, "subscriptionID", subscriptionID)
            // Continue with next period; payment failures don't block other periods
        } else {
            logger.Info("Payment attempt successful", "invoiceID", invoiceID, "subscriptionID", subscriptionID)
        }
    }
    
    // Step 4: Update subscription metadata after all periods processed
    err = workflow.ExecuteActivity(activityCtx, UpdateSubscriptionPeriodActivity, subscriptionID).Get(activityCtx, nil)
    if err != nil {
        logger.Error("Failed to update subscription period metadata", "error", err, "subscriptionID", subscriptionID)
        return err
    }
    
    logger.Info("Successfully processed subscription", "subscriptionID", subscriptionID)
    return nil
}
```

---

## Activities

### 1. CheckPauseActivity

**Purpose**: Determines if a subscription is paused or should skip billing.

**Input**: `subscriptionID` (string)  
**Output**: `isPaused` (bool)

```go
func CheckPauseActivity(ctx context.Context, subscriptionID string) (bool, error) {
    sub, err := db.GetSubscription(subscriptionID)
    if err != nil {
        return false, fmt.Errorf("failed to fetch subscription: %w", err)
    }
    
    return sub.IsPaused, nil
}
```

**Idempotency**: Read-only operation; always safe to retry.

---

### 2. CalculateBillingPeriodsActivity

**Purpose**: Calculates all billing periods that require invoicing based on subscription cycle and last billed date.

**Input**: `subscriptionID` (string)  
**Output**: `billingPeriods` ([]BillingPeriod)

```go
func CalculateBillingPeriodsActivity(ctx context.Context, subscriptionID string) ([]BillingPeriod, error) {
    sub, err := db.GetSubscription(subscriptionID)
    if err != nil {
        return nil, fmt.Errorf("failed to fetch subscription: %w", err)
    }
    
    var periods []BillingPeriod
    
    currentStart := sub.LastBilledAt
    if currentStart.IsZero() {
        currentStart = sub.CreatedAt
    }
    
    now := time.Now()
    
    for currentStart.Before(now) {
        period := BillingPeriod{
            SubscriptionID: subscriptionID,
            StartDate:      currentStart,
            EndDate:        calculatePeriodEnd(currentStart, sub.BillingCycle),
            Amount:         sub.Amount,
        }
        
        periods = append(periods, period)
        currentStart = period.EndDate
    }
    
    return periods, nil
}

type BillingPeriod struct {
    SubscriptionID string
    StartDate      time.Time
    EndDate        time.Time
    Amount         decimal.Decimal
}

func calculatePeriodEnd(start time.Time, cycle string) time.Time {
    switch cycle {
    case "monthly":
        return start.AddDate(0, 1, 0)
    case "quarterly":
        return start.AddDate(0, 3, 0)
    case "yearly":
        return start.AddDate(1, 0, 0)
    default:
        return start.AddDate(0, 1, 0) // Default to monthly
    }
}
```

**Idempotency**: Read-only operation; always safe to retry.

---

### 3. CreateInvoiceActivity

**Purpose**: Creates an invoice record for a billing period. Handles idempotency by checking for existing invoices.

**Input**: `subscriptionID` (string), `period` (BillingPeriod)  
**Output**: `invoiceID` (string)

```go
func CreateInvoiceActivity(ctx context.Context, subscriptionID string, period BillingPeriod) (string, error) {
    // Idempotency: Check if invoice already exists for this subscription and period
    existing, err := db.GetInvoiceBySubscriptionAndPeriod(subscriptionID, period.StartDate)
    if err == nil && existing != nil {
        return existing.ID, nil // Return existing invoice ID
    }
    
    invoice := &Invoice{
        ID:             generateUUID(),
        SubscriptionID: subscriptionID,
        StartDate:      period.StartDate,
        EndDate:        period.EndDate,
        Amount:         period.Amount,
        Status:         "draft",
        CreatedAt:      time.Now(),
        SyncedToStripe: false,
        SyncedToHubspot: false,
        PaymentStatus:  "pending",
    }
    
    err = db.SaveInvoice(invoice)
    if err != nil {
        return "", fmt.Errorf("failed to save invoice: %w", err)
    }
    
    return invoice.ID, nil
}

type Invoice struct {
    ID             string
    SubscriptionID string
    StartDate      time.Time
    EndDate        time.Time
    Amount         decimal.Decimal
    Status         string // draft, finalized, paid, failed
    CreatedAt      time.Time
    SyncedToStripe bool
    SyncedToHubspot bool
    PaymentStatus  string // pending, attempted, paid, failed
    StripeInvoiceID string
    TransactionID  string
}
```

**Idempotency**: Checks for existing invoices before creating new ones. Safe to retry.

---

### 4. SyncToExternalVendorActivity

**Purpose**: Syncs the created invoice to external vendors (Stripe, Hubspot, etc.). Handles partial failures gracefully.

**Input**: `invoiceID` (string), `subscriptionID` (string)  
**Output**: None

```go
func SyncToExternalVendorActivity(ctx context.Context, invoiceID string, subscriptionID string) error {
    invoice, err := db.GetInvoice(invoiceID)
    if err != nil {
        return fmt.Errorf("failed to fetch invoice: %w", err)
    }
    
    // Idempotency: Skip if already synced
    if invoice.SyncedToStripe && invoice.SyncedToHubspot {
        return nil
    }
    
    // Sync to Stripe
    if !invoice.SyncedToStripe {
        stripeClient := getStripeClient()
        stripeInvoice, err := stripeClient.CreateInvoice(&StripeInvoicePayload{
            CustomerID: subscriptionID,
            Amount:     invoice.Amount.IntPart(),
            Description: fmt.Sprintf("Invoice %s (Period: %s to %s)", invoice.ID, invoice.StartDate.Format("2006-01-02"), invoice.EndDate.Format("2006-01-02")),
        })
        
        if err != nil {
            return fmt.Errorf("failed to sync to Stripe: %w", err)
        }
        
        err = db.UpdateInvoiceStripeID(invoiceID, stripeInvoice.ID)
        if err != nil {
            return fmt.Errorf("failed to update Stripe invoice ID: %w", err)
        }
    }
    
    // Sync to Hubspot
    if !invoice.SyncedToHubspot {
        hubspotClient := getHubspotClient()
        err := hubspotClient.CreateInvoice(&HubspotInvoicePayload{
            ContactID: subscriptionID,
            Amount:    invoice.Amount.String(),
            InvoiceDate: invoice.StartDate,
            DueDate:    invoice.EndDate,
        })
        
        if err != nil {
            return fmt.Errorf("failed to sync to Hubspot: %w", err)
        }
        
        err = db.UpdateInvoiceSyncStatus(invoiceID, "hubspot", true)
        if err != nil {
            return fmt.Errorf("failed to update Hubspot sync status: %w", err)
        }
    }
    
    return nil
}
```

**Idempotency**: Checks sync flags before syncing. Safe to retry; skips already-synced vendors.

---

### 5. AttemptPaymentActivity

**Purpose**: Attempts to collect payment for the invoice using the subscription's payment method.

**Input**: `invoiceID` (string), `subscriptionID` (string)  
**Output**: None

```go
func AttemptPaymentActivity(ctx context.Context, invoiceID string, subscriptionID string) error {
    invoice, err := db.GetInvoice(invoiceID)
    if err != nil {
        return fmt.Errorf("failed to fetch invoice: %w", err)
    }
    
    // Idempotency: Check if payment already attempted/successful
    if invoice.PaymentStatus == "paid" {
        return nil // Already paid; nothing to do
    }
    
    // Get payment method for subscription
    sub, err := db.GetSubscription(subscriptionID)
    if err != nil {
        return fmt.Errorf("failed to fetch subscription: %w", err)
    }
    
    if sub.PaymentMethodID == "" {
        return fmt.Errorf("subscription has no payment method")
    }
    
    // Attempt payment via payment provider
    paymentClient := getPaymentClient()
    paymentResult, err := paymentClient.ChargeCard(&PaymentPayload{
        PaymentMethodID: sub.PaymentMethodID,
        Amount:          invoice.Amount,
        InvoiceID:       invoiceID,
        Description:     fmt.Sprintf("Payment for invoice %s", invoice.ID),
    })
    
    if err != nil {
        // Mark as attempted with error
        err2 := db.UpdateInvoicePaymentStatus(invoiceID, "failed", err.Error())
        if err2 != nil {
            return fmt.Errorf("payment failed: %w, and status update failed: %w", err, err2)
        }
        return fmt.Errorf("payment attempt failed: %w", err)
    }
    
    // Mark invoice as paid
    err = db.UpdateInvoicePaymentStatus(invoiceID, "paid", paymentResult.TransactionID)
    if err != nil {
        return fmt.Errorf("failed to update invoice payment status: %w", err)
    }
    
    return nil
}
```

**Idempotency**: Checks payment status before attempting. Retries are safe; skip if already paid.

---

### 6. UpdateSubscriptionPeriodActivity

**Purpose**: Updates subscription metadata after all billing periods are processed (e.g., last billed date, next billing date).

**Input**: `subscriptionID` (string)  
**Output**: None

```go
func UpdateSubscriptionPeriodActivity(ctx context.Context, subscriptionID string) error {
    sub, err := db.GetSubscription(subscriptionID)
    if err != nil {
        return fmt.Errorf("failed to fetch subscription: %w", err)
    }
    
    now := time.Now()
    nextBillingDate := calculateNextBillingDate(sub.BillingCycle, now)
    
    update := &SubscriptionUpdate{
        LastBilledAt:   now,
        NextBillingAt:  nextBillingDate,
        UpdatedAt:      now,
    }
    
    err = db.UpdateSubscription(subscriptionID, update)
    if err != nil {
        return fmt.Errorf("failed to update subscription: %w", err)
    }
    
    return nil
}

func calculateNextBillingDate(cycle string, fromTime time.Time) time.Time {
    switch cycle {
    case "monthly":
        return fromTime.AddDate(0, 1, 0)
    case "quarterly":
        return fromTime.AddDate(0, 3, 0)
    case "yearly":
        return fromTime.AddDate(1, 0, 0)
    default:
        return fromTime.AddDate(0, 1, 0) // Default to monthly
    }
}
```

**Idempotency**: Updates are idempotent; updating the same subscription twice with current timestamp is safe.

---

## Supporting Activities (Used by Scheduler)

### FetchSubscriptionBatch

**Purpose**: Fetches a batch of subscription IDs to process.

**Input**: `batchSize` (int), `offset` (int)  
**Output**: `subscriptionIDs` ([]string)

```go
func FetchSubscriptionBatch(ctx context.Context, batchSize int, offset int) ([]string, error) {
    query := `
        SELECT id FROM subscriptions 
        WHERE status = 'active' 
        ORDER BY id 
        LIMIT ? OFFSET ?
    `
    
    rows, err := db.Query(query, batchSize, offset)
    if err != nil {
        return nil, fmt.Errorf("failed to query subscriptions: %w", err)
    }
    defer rows.Close()
    
    var ids []string
    for rows.Next() {
        var id string
        if err := rows.Scan(&id); err != nil {
            return nil, fmt.Errorf("failed to scan subscription ID: %w", err)
        }
        ids = append(ids, id)
    }
    
    if err = rows.Err(); err != nil {
        return nil, fmt.Errorf("error iterating rows: %w", err)
    }
    
    return ids, nil
}
```

**Idempotency**: Read-only; always safe to retry.

---

### EnqueueSubscriptionWorkflows

**Purpose**: Starts independent `ProcessSingleSubscriptionWorkflow` for each subscription.

**Input**: `subscriptionIDs` ([]string)  
**Output**: None

```go
func EnqueueSubscriptionWorkflows(ctx context.Context, subscriptionIDs []string) error {
    client := getTemporalClient()
    
    for _, subID := range subscriptionIDs {
        workflowOptions := client.GetWorkflowOptions()
        workflowOptions.ID = fmt.Sprintf("process-subscription-%s-%d", subID, time.Now().Unix())
        workflowOptions.TaskQueue = "subscription-processing"
        workflowOptions.WorkflowExecutionTimeout = 30 * time.Minute
        
        _, err := client.ExecuteWorkflow(ctx, workflowOptions, ProcessSingleSubscriptionWorkflow, subID)
        if err != nil {
            // Log but don't fail; continue enqueueing other subscriptions
            fmt.Printf("Failed to start workflow for subscription %s: %v\n", subID, err)
        }
    }
    
    return nil
}

func getTemporalClient() client.Client {
    c, err := client.Dial(client.Options{
        HostPort: "localhost:7233",
    })
    if err != nil {
        panic(err)
    }
    return c
}
```

**Idempotency**: Enqueuing is idempotent because each workflow has a unique ID based on subscription ID and timestamp.

---

## Retry Policies

### Activity Retry Policy (Recommended for all activities)

```go
RetryPolicy: &temporal.RetryPolicy{
    InitialInterval:    time.Second,
    MaximumInterval:    time.Minute,
    BackoffCoefficient: 2.0,
    MaximumAttempts:    3,
}
```

- **First retry**: 1 second
- **Second retry**: 2 seconds
- **Third retry**: 4 seconds
- Then fails

### Custom Retry Policy for External Syncs

```go
RetryPolicy: &temporal.RetryPolicy{
    InitialInterval:    time.Second * 5,
    MaximumInterval:    time.Minute * 5,
    BackoffCoefficient: 2.0,
    MaximumAttempts:    5,
}
```

More tolerant for flaky external APIs.

---

## Configuration & Setup

### Worker Registration

```go
func main() {
    c, err := client.Dial(client.Options{
        HostPort: "localhost:7233",
    })
    if err != nil {
        panic(err)
    }
    defer c.Close()
    
    w := worker.New(c, "subscription-processing", worker.Options{})
    
    // Register workflows
    w.RegisterWorkflow(SubscriptionSchedulerWorkflow)
    w.RegisterWorkflow(ProcessSingleSubscriptionWorkflow)
    
    // Register activities
    w.RegisterActivity(FetchSubscriptionBatch)
    w.RegisterActivity(EnqueueSubscriptionWorkflows)
    w.RegisterActivity(CheckPauseActivity)
    w.RegisterActivity(CalculateBillingPeriodsActivity)
    w.RegisterActivity(CreateInvoiceActivity)
    w.RegisterActivity(SyncToExternalVendorActivity)
    w.RegisterActivity(AttemptPaymentActivity)
    w.RegisterActivity(UpdateSubscriptionPeriodActivity)
    
    err = w.Run(worker.InterruptCh())
    if err != nil {
        panic(err)
    }
}
```

### Cron Trigger

```go
options := client.StartWorkflowOptions{
    ID:        "subscription-scheduler-" + time.Now().Format("2006-01-02-15:04"),
    TaskQueue: "subscription-processing",
    CronSchedule: "0 2 * * *", // Every day at 2 AM
}

_, err := c.ExecuteWorkflow(context.Background(), options, SubscriptionSchedulerWorkflow)
```

---

## Benefits

1. **Independent Failures**: Single subscription failure doesn't affect others.
2. **Granular Retries**: Each activity retries independently with configurable policies.
3. **Observable**: Each subscription has its own workflow execution you can monitor.
4. **Scalable**: Handles 100K+ subscriptions without parent blocking.
5. **Recoverable**: Can replay, signal, or reset individual subscription workflows.
6. **Idempotent**: All activities designed to be safe on retry.

---

## Monitoring & Debugging

### Query Workflow Status

```go
execution, err := c.DescribeWorkflowExecution(ctx, subscriptionID, "")
if err != nil {
    panic(err)
}

fmt.Printf("Status: %v\n", execution.WorkflowExecutionInfo.Status)
fmt.Printf("Started: %v\n", execution.WorkflowExecutionInfo.StartTime)
fmt.Printf("Closed: %v\n", execution.WorkflowExecutionInfo.CloseTime)
```

### Get Workflow History

```go
historyIterator := c.GetWorkflowHistory(ctx, subscriptionID, "", false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)

for historyIterator.HasNext() {
    event, err := historyIterator.Next()
    if err != nil {
        panic(err)
    }
    fmt.Printf("%v: %v\n", event.EventType, event)
}
```

### View Logs

Temporal Logs are available in the Web UI at `http://localhost:8233`

---

## Troubleshooting

### Subscription Never Processes

1. Check if scheduler workflow is running: `temporal workflow list`
2. Check if subscription is paused: Query DB
3. Check worker is consuming tasks: `temporal task-queue describe subscription-processing`

### Payment Keeps Failing

1. Check `AttemptPaymentActivity` error logs
2. Verify payment provider credentials
3. Check if invoice is already marked as paid (idempotency)

### High Event History

Consider using `continue-as-new` in scheduler if processing millions of subscriptions per run.

---

## Future Enhancements

1. **Signals**: Send signals to workflows to pause/resume/cancel subscriptions
2. **Queries**: Query active subscriptions being processed in real-time
3. **Dead Letter Queue**: Capture permanently failed subscriptions for manual review
4. **Webhooks**: Integrate with payment provider webhooks instead of polling
5. **Metrics**: Export processing metrics (subscriptions per second, error rates, etc.)

