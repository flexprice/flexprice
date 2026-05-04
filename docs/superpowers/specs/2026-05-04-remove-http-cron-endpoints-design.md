# Remove HTTP Cron Endpoints — Design Spec

**Date:** 2026-05-04  
**Status:** Approved

## Problem

The codebase has two parallel mechanisms for running recurring jobs:

1. **HTTP cron endpoints** (`/v1/cron/*`) — legacy, manually triggered via external scheduler or CI
2. **Temporal server schedules** — automated, fault-tolerant, observable via Temporal UI

All 6 recurring jobs already have matching Temporal schedules running in production. The HTTP endpoints are dead weight: extra surface area, extra DI wiring, and (as demonstrated by the invoice finalization bug) a source of subtle behavioral divergence when the two paths have different logic.

The `void-old-pending` job has no Temporal equivalent yet. Kafka lag monitoring is being dropped entirely (no business requirement to preserve it).

## Goal

Eliminate the entire HTTP cron layer. Consolidate all recurring jobs into Temporal server schedules. Leave no orphaned routes, handlers, or handler-only service methods.

## New Temporal Pieces — `void-old-pending`

### Service method

**File:** `internal/service/invoice.go`  
**Method:** `VoidOldPendingInvoices(ctx context.Context) error`

Extract all logic from the current HTTP handler and its private helpers into this service method. Logic:

1. Iterate all tenant + environment pairs via `tenantService.GetAllTenants` + `environmentService.GetEnvironments`
2. For each environment, query incomplete subscriptions created more than 24 hours ago
3. For each subscription, count its pending invoices (Draft + Finalized, no payment filter):
   - **0 invoices** → cancel subscription immediately
   - **1 invoice** → void if eligible (Stripe-synced, no partial payment in Flexprice or Stripe), then cancel subscription
   - **2+ invoices** → skip (too complex for automatic handling)
4. Cancellation params: `CancellationTypeImmediate`, `ProrationBehaviorNone`, reason = "Automatic cancellation due to old pending invoices"

The following private helpers from `internal/api/cron/invoice.go` move into the service method (inlined or kept as unexported helpers on the service):
`processAllTenantsAndEnvironments`, `processIncompleteSubscriptionsForEnvironment`, `processOldIncompleteSubscription`, `processSingleInvoice`, `checkStripePartialPayment`, `voidInvoiceInStripe`

### Temporal activity

**File:** `internal/temporal/activities/cron/invoice_activities.go` (new)  
**Method:** `VoidOldPendingInvoicesActivity(ctx) error`  
Calls `InvoiceService.VoidOldPendingInvoices(ctx)`. Same pattern as all other cron activities.

### Temporal workflow

**File:** `internal/temporal/workflows/cron/void_old_pending_invoices_workflow.go` (new)  
**Name:** `VoidOldPendingInvoicesWorkflow`  
Executes `VoidOldPendingInvoicesActivity` with:
- `StartToCloseTimeout`: 30 minutes
- `MaximumAttempts`: 1 retry
Follows the same pattern as `SubscriptionAutoCancellationWorkflow`.

### Schedule

**ID:** `ScheduleIDVoidOldPendingInvoices = "void-old-pending-invoices"`  
**Interval:** every 1 hour (the 24-hour age threshold makes sub-hourly redundant)  
**Task queue:** `TemporalTaskQueueCron`  
Registered in both `AllTemporalScheduleConfigs` (schedules.go) and `AllTemporalServerScheduleIDs` (types/schedule.go).

### Registration

Wire `InvoiceCronActivities` into `RegisterWorkflowsAndActivities` in `internal/temporal/registration.go`. Register `VoidOldPendingInvoicesWorkflow` in the workflow registration block.

## Deletions

### `internal/api/cron/` — entire directory

| File | Lines | Note |
|---|---|---|
| `subscription.go` | 99 | Covered by existing Temporal schedules |
| `wallet.go` | 443 | Covered by existing Temporal schedule |
| `creditgrant.go` | 44 | Covered by existing Temporal schedule |
| `invoice.go` | 557 | Logic moves to service method |
| `kafka_lag_monitoring.go` | 46 | Dropped entirely — no migration |

### `internal/api/router.go`

Remove the entire `/v1/cron` route group (~lines 617–642) and all handler injections in the router constructor.

### `cmd/server/main.go`

Remove `fx.Provide` / `fx.Invoke` entries for all cron handlers:
`InvoiceHandler`, `SubscriptionHandler`, `WalletCronHandler`, `CreditGrantCronHandler`, `KafkaLagMonitoringHandler`

### `internal/service/event.go`

Delete `MonitorKafkaLag` method — only caller is the deleted Kafka handler.

## What Stays Unchanged

All service methods called by existing Temporal activities remain untouched:
- `UpdateBillingPeriods`
- `ProcessTrialEndDue`
- `ProcessAutoCancellationSubscriptions`
- `ProcessSubscriptionRenewalDueAlert`
- `ExpireCredits`
- `ProcessScheduledCreditGrantApplications`

## Temporal Schedule Coverage After Migration

| Job | Schedule ID | Interval |
|---|---|---|
| Credit grant processing | `credit-grants-processing` | 15 min |
| Subscription auto-cancellation | `subscription-auto-cancellation` | 15 min |
| Wallet credit expiry | `wallet-credit-expiry` | 15 min |
| Subscription billing | `subscription-billing` | 15 min |
| Subscription renewal alerts | `subscription-renewal-due-alerts` | 15 min |
| Subscription trial end | `subscription-trial-end-due` | 15 min |
| Outbound webhook stale retry | `webhook-stale-retry` | 15 min |
| Void old pending invoices | `void-old-pending-invoices` | 1 hour |

## Out of Scope

- Dynamic per-tenant schedules created via `/v1/subscriptions/temporal/schedule-*` — these are not cron endpoints, they stay
- The `ScheduledTaskService` and export workflows — unrelated
