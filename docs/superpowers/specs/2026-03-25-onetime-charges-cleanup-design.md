# One-Time Charges: Cleanup & Validation Design

**Date:** 2026-03-25
**Branch:** `feat/onetime-charges`
**Status:** Approved

---

## Context

The two most recent commits (`427640b6`, `d5265655`) implemented one-time charge support for subscriptions. An audit revealed that the core billing logic is correct and complete, with one structural issue and two validation gaps:

- **Structural:** A `billing_cadence` column was added to the `subscription_line_items` ent schema but was never applied to the database. It must be removed cleanly.
- **Validation gap 1:** `CreatePriceRequest.Validate()` does not reject `BILLING_CADENCE_ONETIME` with a non-`ADVANCE` invoice cadence.
- **Validation gap 2:** `CreateSubscriptionLineItemRequest.Validate()` does not explicitly reject ONETIME + non-ADVANCE combinations (it silently defaults, which is unsafe).

---

## What Is Already Working (Do Not Touch)

| Area | File | Status |
|---|---|---|
| `CalculateFixedCharges` — full amount, `period_start = period_end = charge_date` | `internal/service/billing.go` | ✅ |
| `ClassifyLineItems` — ONETIME never repeats in NextPeriod | `internal/service/billing.go` | ✅ |
| `checkIfChargeInvoiced` / idempotency | `internal/service/billing.go` | ✅ |
| Coupon application includes ONETIME items | `internal/service/billing.go` | ✅ |
| Charge date validation against subscription bounds | `internal/api/dto/subscription_line_item.go` | ✅ |
| Charge date validation against phase bounds | `internal/service/subscription.go` | ✅ |
| `BillingCadence` propagation from plan price → line item | `internal/api/dto/subscription_line_item.go` | ✅ |
| Phase plan-price loop: ONETIME defaults to `phase.StartDate` | `internal/service/subscription.go` | ✅ |
| `GetChargeDate()` returns `StartDate` | `internal/domain/subscription/line_item.go` | ✅ |

---

## Constraints

- No DB schema changes (the `billing_cadence` column was never migrated)
- No changes to existing recurring billing logic
- Backward-compatible — all existing subscriptions continue to work

---

## Design

### 1. Remove `billing_cadence` from Ent Schema

**File:** `ent/schema/subscription_line_item.go`

Remove the `billing_cadence` field definition entirely. This field was never applied to the DB, so no migration is needed.

After removal, run `make generate-ent` to regenerate all Ent-generated code.

---

### 2. Remove `BillingCadence` from Domain Model and Ent Conversion

**File:** `internal/domain/subscription/line_item.go`

- Remove the `BillingCadence types.BillingCadence` field from the `SubscriptionLineItem` struct.
- Remove the `BillingCadence` assignment in the `FromEnt()` conversion (which defaults to `RECURRING` when empty).
- Update `IsOneTime()` to derive ONETIME status from existing persisted fields:

```go
// IsOneTime returns true if this line item is a one-time charge.
// ONETIME charges are FIXED type with no billing period.
func (li *SubscriptionLineItem) IsOneTime() bool {
    return li.PriceType == types.PRICE_TYPE_FIXED && li.BillingPeriod == ""
}
```

This works because:
- ONETIME prices always have `BillingPeriod = ""` (enforced at price creation — see §4)
- RECURRING FIXED prices always have a `BillingPeriod` (already enforced by existing validation)
- `BillingPeriod` is already a persisted column on `subscription_line_items`

`GetChargeDate()` is unchanged — still returns `li.StartDate`.

---

### 3. Remove `BillingCadence` from DTO Conversion

**File:** `internal/api/dto/subscription_line_item.go` — `ToSubscriptionLineItem()`

Remove the `billingCadence` variable and all code that reads `params.Price.BillingCadence` to set it on the line item. The field no longer exists on the struct.

The `invoiceCadence` default-to-ADVANCE logic for ONETIME can be removed here too — it is now enforced upstream at price creation (§4).

---

### 4. Add Price Creation Validation for ONETIME

**File:** `internal/api/dto/price.go` — `CreatePriceRequest.Validate()`

In the existing `switch r.BillingCadence` block, add:

```go
case types.BILLING_CADENCE_ONETIME:
    if r.InvoiceCadence != "" && r.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("invoice_cadence must be ADVANCE for ONETIME prices").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
    // ONETIME prices have no billing period
    r.BillingPeriod = ""
    r.BillingPeriodCount = 0
```

This ensures:
- The DB never stores an ONETIME price with a non-ADVANCE cadence
- `BillingPeriod` and `BillingPeriodCount` are always cleared, so line items derived from this price will have `BillingPeriod == ""`, making `IsOneTime()` work correctly

---

### 5. Add Line Item Validation for ONETIME + Non-ADVANCE

**File:** `internal/api/dto/subscription_line_item.go` — `Validate()`

After the existing charge_date bounds check, add:

```go
// ONETIME charges must use ADVANCE invoice cadence
if price != nil && price.BillingCadence == types.BILLING_CADENCE_ONETIME {
    if price.InvoiceCadence != "" && price.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("ONETIME charges must have invoice_cadence ADVANCE").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
}
```

This acts as a defense-in-depth check at line item creation, catching any ONETIME price that somehow has non-ADVANCE cadence.

---

### 6. Update Tests

**File:** `internal/service/billing_onetime_test.go`

Update all test fixtures and helpers that set `BillingCadence` on line items:
- Remove `BillingCadence: types.BILLING_CADENCE_ONETIME` from line item construction
- Instead ensure `BillingPeriod: ""` (or omit it — zero value is `""`) on ONETIME line items
- Ensure `BillingPeriod` is set on RECURRING line items in fixtures

---

## Change Summary

| # | File | Change |
|---|---|---|
| 1 | `ent/schema/subscription_line_item.go` | Remove `billing_cadence` field |
| 2 | `internal/domain/subscription/line_item.go` | Remove `BillingCadence` field; update `IsOneTime()` |
| 3 | `internal/api/dto/subscription_line_item.go` | Remove `BillingCadence` from `ToSubscriptionLineItem()`; add ONETIME+ADVANCE validation in `Validate()` |
| 4 | `internal/api/dto/price.go` | Add `case BILLING_CADENCE_ONETIME` to price validation |
| 5 | `internal/service/billing_onetime_test.go` | Update fixtures to use `BillingPeriod == ""` instead of `BillingCadence` |
| 6 | Run `make generate-ent` | Regenerate Ent code after schema change |
| 7 | Run `make test` | Verify no regressions |

---

## Non-Goals

- No changes to `CalculateFixedCharges`, `ClassifyLineItems`, or any other billing pipeline logic
- No new DB migration
- No changes to invoice generation, proration, or coupon logic
