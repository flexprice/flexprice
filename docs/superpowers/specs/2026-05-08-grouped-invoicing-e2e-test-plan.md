# Grouped Invoicing E2E Test Plan

**Date:** 2026-05-08  
**Status:** Approved  
**Author:** omkar273

---

## Overview

End-to-end test coverage for the grouped invoicing subscription type feature. Covers HTTP API testing (live server) and service-layer unit tests.

---

## Fix Required: GetPreviewInvoice for Parent Subscriptions

`GetPreviewInvoice` and `GetInternalPreviewInvoice` currently call `PrepareSubscriptionInvoiceRequest` on a single subscription. For a `parent` subscription they must detect grouped children and use `PrepareGroupedInvoiceRequest` instead, returning a merged clubbed view.

**Change:** In both preview handlers, after fetching `sub`, if `sub.SubscriptionType == parent`:
1. Call `getGroupedInvoicingSubscriptions(ctx, sub.ID)` on the subscription service (or via repo filter)
2. If children found, call `billingService.PrepareGroupedInvoiceRequest(...)` instead of `PrepareSubscriptionInvoiceRequest`
3. Response line items reflect merged parent + all grouped children

---

## Test Data Setup (Phase 0)

| Entity | Details |
|---|---|
| Plan A | name=`gi-plan`, flat fee $10/month, USD, billing_cadence=monthly, billing_period=monthly, billing_period_count=1 |
| Customer P | external_id=`gi-parent-cust` — holds parent subscription |
| Customer C1 | external_id=`gi-child-1` — holds first child subscription |
| Customer C2 | external_id=`gi-child-2` — holds second child subscription |

---

## Phase 1: Create-Time Subscription Types

### TC 1.1 — Standalone (baseline)
- **Action:** POST `/v1/subscriptions` for Customer P, no `inheritance`
- **Assert:** `subscription_type=standalone`, `parent_subscription_id=null`

### TC 1.2 — Parent subscription
- **Action:** POST `/v1/subscriptions` for Customer P with `inheritance.invoicing_behavior=parent`
- **Assert:** `subscription_type=parent`

### TC 1.3 — Grouped child at creation time
- **Action:** POST `/v1/subscriptions` for Customer C1 with `inheritance.invoicing_behavior=grouped_invoicing`, `inheritance.parent_subscription_id=<parent_id>`
- **Assert:** `subscription_type=grouped_invoicing`, `parent_subscription_id=<parent_id>`

### TC 1.4 — Parent attaches existing standalones at creation
- **Pre-condition:** Create standalone subs for C1 and C2
- **Action:** POST `/v1/subscriptions` for Customer P with `inheritance.invoicing_behavior=parent`, `inheritance.sub_ids_for_grouped_invoicing=[sub_c1, sub_c2]`
- **Assert:** Both C1 and C2 subs now have `subscription_type=grouped_invoicing` and `parent_subscription_id` set

### TC 1.5 — Delegated subscription
- **Action:** POST `/v1/subscriptions` for Customer C1 with `inheritance.invoicing_behavior=delegated`, `inheritance.invoicing_customer_external_id=gi-parent-cust`
- **Assert:** `subscription_type=delegated`, `invoicing_customer_id=<Customer P internal ID>`

---

## Phase 2: Modify API (Post-Creation Membership Changes)

### TC 2.1 — Preview add (dry run)
- **Pre-condition:** Parent sub exists; standalone sub for C1 exists
- **Action:** POST `/v1/subscriptions/:parent_id/modify/preview` with `type=grouped_invoicing_add`, `grouped_invoicing_params.parent_subscription_id=<parent_id>`, `child_subscription_ids=[<c1_sub_id>]`
- **Assert:** Response contains changed subscription entry; C1 sub still has `subscription_type=standalone` (no writes)

### TC 2.2 — Execute add
- **Action:** POST `/v1/subscriptions/:parent_id/modify/execute` same body as TC 2.1
- **Assert:** C1 sub now has `subscription_type=grouped_invoicing`, `parent_subscription_id=<parent_id>`

### TC 2.3 — Preview remove (dry run)
- **Pre-condition:** C1 sub is grouped_invoicing
- **Action:** POST `/v1/subscriptions/:parent_id/modify/preview` with `type=grouped_invoicing_remove`, `child_subscription_ids=[<c1_sub_id>]`
- **Assert:** Response contains changed subscription entry; C1 sub still has `subscription_type=grouped_invoicing` (no writes)

### TC 2.4 — Execute remove
- **Action:** POST `/v1/subscriptions/:parent_id/modify/execute` same body as TC 2.3
- **Assert:** C1 sub now has `subscription_type=standalone`, `parent_subscription_id=null`

---

## Phase 3: Preview Invoice

### TC 3.1 — Preview parent-only (no children)
- **Pre-condition:** Parent sub with no grouped children
- **Action:** POST `/v1/invoices/preview` with `subscription_id=<parent_id>`
- **Assert:** Line items contain only parent's own charges

### TC 3.2 — Preview clubbed invoice (parent + 2 children)
- **Pre-condition:** Parent sub with C1 and C2 as grouped_invoicing children
- **Action:** POST `/v1/invoices/preview` with `subscription_id=<parent_id>`
- **Assert:** Line items contain parent's charges + C1's charges + C2's charges; subtotal = sum of all three

### TC 3.3 — Preview child's own invoice
- **Pre-condition:** C1 is grouped_invoicing under parent
- **Action:** POST `/v1/invoices/preview` with `subscription_id=<c1_sub_id>`
- **Assert:** Returns C1's own line items only (child is independent, not skipped for preview)

---

## Phase 4: Validation Errors

| TC | Request | Expected error |
|---|---|---|
| 4.1 | `invoicing_behavior=grouped_invoicing` without `parent_subscription_id` | 400 — parent_subscription_id required |
| 4.2 | `invoicing_behavior=delegated` without `invoicing_customer_external_id` | 400 — invoicing_customer_external_id required |
| 4.3 | `invoicing_behavior=parent` with `parent_subscription_id` set | 400 — parent must not have parent_subscription_id |
| 4.4 | Add child with mismatched billing period to parent | 400 — billing period mismatch |
| 4.5 | Add already-grouped_invoicing child | 400 — child must be standalone |
| 4.6 | Remove a standalone sub (not grouped) | 400 — not of type grouped_invoicing |
| 4.7 | `invoicing_behavior=standalone` with `sub_ids_for_grouped_invoicing` set | 400 — standalone must not have sub_ids_for_grouped_invoicing |

---

## Phase 5: Service-Layer Test Cases (Code)

Added to `internal/service/subscription_test.go` and `internal/service/subscription_modification_grouped_test.go`.

| TC | Test name | What |
|---|---|---|
| 5.1 | `TestCreateSubscription_GroupedInvoicingChild` | `InvoicingBehavior=grouped_invoicing` + valid parent → sub has correct type and parent link |
| 5.2 | `TestCreateSubscription_ParentAttachesExistingStandalones` | `SubIDsForGroupedInvoicing` converts subs post-parent-create |
| 5.3 | `TestCreateSubscription_DelegatedSetsInvoicingCustomerID` | `delegated` resolves external ID to internal and stores it |
| 5.4 | `TestModifySubscription_GroupedInvoicingAddPreview` | Preview returns changed subs without writing |
| 5.5 | `TestModifySubscription_GroupedInvoicingAddExecute` | Execute flips child type and sets parent link |
| 5.6 | `TestModifySubscription_GroupedInvoicingRemoveExecute` | Remove resets child to standalone and clears parent link |
| 5.7 | `TestPrepareGroupedInvoiceRequest_MergesLineItems` | Billing service merges parent + children line items; subtotal = sum |

---

## Execution Order

1. Fix `GetPreviewInvoice` / `GetInternalPreviewInvoice` for parent subs
2. Build + start server
3. Run Phase 0 setup via curl
4. Run Phases 1–4 via curl, log pass/fail
5. Run Phase 5 service tests via `go test`
6. Add any failing cases as new service tests
