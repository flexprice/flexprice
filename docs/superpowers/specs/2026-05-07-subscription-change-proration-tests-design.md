# Design: Comprehensive Subscription Change & Proration Tests

**Date:** 2026-05-07
**PR:** flexprice/flexprice#1733
**File:** `internal/service/subscription_change_test.go`

---

## Context

PR #1733 rewired how proration credits are handled during an immediate plan change. Previously, cancelling the old subscription with `create_prorations` issued a wallet credit top-up. Now the wallet top-up is skipped (`SkipProrationWalletCredit: true`) and the unused-credit amount is instead applied as `OpeningInvoiceAdjustmentAmount` directly against the new subscription's first invoice line items.

Key changes in the PR:

| Change | Effect |
|---|---|
| `SkipProrationWalletCredit` on `CancelSubscriptionRequest` | Prevents wallet top-up during plan-change cancellation |
| `OpeningInvoiceAdjustmentAmount` on `CreateSubscriptionRequest` | Passes proration credit to new sub's opening invoice |
| `applySubscriptionChangePreviewCredit` in `subscription_change.go` | Reduces next-invoice preview total when `change_at=immediate` |
| `applyFixedChargeAdjustmentToLineItems` in `billing.go` | Reduces fixed line items in order up to the credit cap |
| `IsFirstSubscriptionOpenInvoiceReason` in `types/invoice.go` | Now also returns `true` for `SUBSCRIPTION_UPDATE` (renamed from `TriggersSubscriptionActivationOnFullPayment`) |
| Opening invoice `BillingReason = SUBSCRIPTION_UPDATE` | New billing reason for plan-change opening invoice |

A normal cancel with `create_prorations` (no plan change) still issues a wallet credit — `SkipProrationWalletCredit` is only set by the plan-change path.

---

## Decisions

- **Test location:** All new tests added to the existing `SubscriptionChangeServiceTestSuite` in `internal/service/subscription_change_test.go`. The `suite.Run` registration at the bottom is uncommented.
- **Structure:** Option B — grouped `s.Run(...)` subtests. One top-level `TestXxx` method per scenario family. Shared fixtures set up once per family.
- **Deterministic proration:** Achieved by backdating `CurrentPeriodStart`/`CurrentPeriodEnd` on the subscription via `SubscriptionRepo.Update` after creation. Standard scenario: 15 days used of a 30-day period, producing exactly half the monthly price as credit.
- **Amount assertions:** Tolerance-based (`|actual − expected| < $0.01`) because proration uses wall-clock timestamps with sub-second precision.
- **Wallet assertions:** Use `WalletRepo.GetWalletsByFilter` with `QueryFilter` scoped to the test's tenant/environment (default filter in test context).

---

## Shared Helpers to Add

### `backdateSub`
```go
// backdateSub updates a subscription's billing period in the DB so that
// daysUsed days have already elapsed out of a totalDays-long period.
// Returns the updated subscription for convenience.
func (s *SubscriptionChangeServiceTestSuite) backdateSub(
    sub *subscription.Subscription,
    daysUsed, totalDays int,
) *subscription.Subscription
```
Implementation: sets `CurrentPeriodStart = now - daysUsed days`, `CurrentPeriodEnd = now + (totalDays - daysUsed) days`, calls `SubscriptionRepo.Update`.

### `getInvoicesForSub`
```go
// getInvoicesForSub lists all non-skipped invoices for a subscription, sorted by created_at asc.
func (s *SubscriptionChangeServiceTestSuite) getInvoicesForSub(subID string) []*invoice.Invoice
```
Implementation: calls `InvoiceRepo.List` with `SubscriptionIDs: []string{subID}` filter.

### `getWalletForCustomer`
```go
// getWalletForCustomer returns the first wallet for a customer, or nil if none exists.
func (s *SubscriptionChangeServiceTestSuite) getWalletForCustomer(customerID string) *walletdomain.Wallet
```
Implementation: calls `WalletRepo.GetWalletsByFilter` with default `QueryFilter`, scans for `CustomerID == customerID`.

### `assertAmountNear`
```go
// assertAmountNear fails if |actual - expected| >= tol.
func assertAmountNear(t *testing.T, expected, actual decimal.Decimal, tol float64, msgAndArgs ...any)
```
Implementation: computes absolute diff, calls `assert.True`.

---

## ServiceParams Addition

`SubScheduleRepo` must be wired into the `ServiceParams` struct built in `setupServices()`. It is required by `scheduleChangeForPeriodEnd` (called in the period-end test) but is currently absent from the test setup.

---

## Test Families

### Family 1 — `TestUpgradeWithCreateProrations`

**Scenario:** Customer on $600/month plan upgrades to $2000/month, immediately, with `create_prorations`.
**Backdate:** 15 days used of 30-day period → credit ≈ $300, adjusted opening invoice ≈ $1700.

Setup (runs once, shared across subtests via suite-level fields set at the top of the method):
- Create customer
- Create $600/month plan (fixed-fee, monthly, advance)
- Create $2000/month plan (fixed-fee, monthly, advance)
- Create subscription on $600 plan
- Backdate: 15 days used / 30 days total

Subtests:

| Subtest name | Assertions |
|---|---|
| `preview/shows_credit_in_proration_details` | `ProrationDetails != nil`; `CreditAmount` within $0.01 of $300 |
| `preview/next_invoice_total_is_netted` | `NextInvoicePreview.Total` within $0.01 of $1700; line-item amounts sum to `Subtotal` |
| `execute/old_sub_cancelled` | Old sub `SubscriptionStatus == CANCELLED`; `CancelledAt != nil` |
| `execute/new_sub_active_on_target_plan` | New sub `SubscriptionStatus == ACTIVE`; `PlanID == $2000-plan-id` |
| `execute/opening_invoice_billing_reason` | Opening invoice `BillingReason == SUBSCRIPTION_UPDATE` |
| `execute/opening_invoice_amount_netted` | Opening invoice `Amount` (or `Total`) within $0.01 of $1700 |
| `execute/no_wallet_credit_created` | `getWalletForCustomer` returns nil OR `wallet.Balance == 0` |

Execute subtests share one `ExecuteSubscriptionChange` call (stored in a local variable before the subtests run).

### Family 2 — `TestCancelWithCreateProrations`

**Scenario:** Customer on $600/month plan cancels mid-period with `create_prorations`. No plan change — this is the standard cancel path.
**Backdate:** 15 days used of 30 → expected wallet credit ≈ $300.

Setup:
- Create customer, $600/month plan, subscription
- Backdate: 15 days used / 30 days total

Subtests:

| Subtest name | Assertions |
|---|---|
| `cancel/response_total_credit_amount` | `CancelResponse.TotalCreditAmount` within $0.01 of $300 |
| `cancel/wallet_exists_for_customer` | `getWalletForCustomer` returns non-nil |
| `cancel/wallet_balance_matches_credit` | `wallet.Balance` within $0.01 of $300 |
| `cancel/subscription_is_cancelled` | Sub `SubscriptionStatus == CANCELLED` |

### Family 3 — `TestUpgradeNoneProration`

**Scenario:** $600 → $2000 upgrade with `proration_behavior = none`. No backdate needed (zero credit expected regardless).

Subtests:

| Subtest name | Assertions |
|---|---|
| `execute/new_sub_active` | New sub `ACTIVE` on $2000 plan |
| `execute/opening_invoice_full_price` | Opening invoice total == $2000 exactly |
| `execute/no_wallet_credit` | No wallet OR balance == 0 |
| `execute/no_proration_applied` | `response.ProrationApplied == nil` |

### Family 4 — `TestUpgradeScheduledPeriodEnd`

**Scenario:** $600 → $2000, `change_at = period_end`. Change is scheduled, not executed. No backdate.

Subtests:

| Subtest name | Assertions |
|---|---|
| `scheduled/response_is_scheduled` | `response.IsScheduled == true`; `response.ScheduleID != nil` |
| `scheduled/old_sub_still_active` | Old sub still `ACTIVE` |
| `scheduled/no_new_sub_created` | Subscription list for customer has only one active subscription |

### Family 5 — `TestIsFirstSubscriptionOpenInvoiceReason`

**Pure unit test — no DB.** Tests `InvoiceBillingReason.IsFirstSubscriptionOpenInvoiceReason()` directly.

Table-driven using `require.True` / `require.False`:

| Reason | Expected |
|---|---|
| `SUBSCRIPTION_CREATE` | `true` |
| `SUBSCRIPTION_TRIAL_END` | `true` |
| `SUBSCRIPTION_UPDATE` | `true` ← newly added in PR |
| `SUBSCRIPTION_CYCLE` | `false` |
| `PRORATION` | `false` |
| `MANUAL` | `false` |

### Family 6 — `TestApplyFixedChargeAdjustmentToLineItems`

**Pure unit test — no DB.** Tests the private `applyFixedChargeAdjustmentToLineItems` billing helper directly.

Table-driven:

| Case | Credit | Input line items | Expected line items |
|---|---|---|---|
| credit < single item | $300 | [$2000] | [$1700] |
| credit spans two items | $300 | [$200, $200] | [$0, $100] |
| credit equals total | $400 | [$200, $200] | [$0, $0] |
| credit exceeds total (capped) | $500 | [$200, $200] | [$0, $0] (no negative) |
| zero credit | $0 | [$200] | [$200] (unchanged) |
| empty items | $300 | [] | [] |

Each case asserts: each line item amount matches expected, total sum is correct, no negative amounts.

---

## What Is Not Tested Here

- Entitlement proration (`handleSubscriptionChangeEntitlementProration`) — covered separately in entitlement tests.
- Coupon transfer (`transferLineItemCoupons`) — covered in coupon-specific tests.
- Multi-currency scenarios — out of scope for this PR.
- Payment gateway webhook triggering subscription activation via `IsFirstSubscriptionOpenInvoiceReason` — covered in payment processor tests.

---

## Files Changed

| File | Change |
|---|---|
| `internal/service/subscription_change_test.go` | Add helpers, 6 test families, uncomment suite runner |

No new files required.
