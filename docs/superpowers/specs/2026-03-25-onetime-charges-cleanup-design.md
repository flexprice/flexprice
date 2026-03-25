# One-Time Charges: Cleanup & Validation Design

**Date:** 2026-03-25
**Branch:** `feat/onetime-charges`
**Status:** Approved (v3 — post second spec review)

---

## Context

The two most recent commits (`427640b6`, `d5265655`) implemented one-time charge support for subscriptions. An audit revealed:

- **Structural issue:** A `billing_cadence` field was added to the `subscription_line_items` ent schema but was **never applied to the database** (no migration run) and was **never written by the repository** (no `SetBillingCadence()` call in Create/Update). It must be removed cleanly and replaced with a derived discriminator.
- **Validation gap 1:** `CreatePriceRequest.Validate()` does not reject `BILLING_CADENCE_ONETIME` with a non-`ADVANCE` invoice cadence.
- **Validation gap 2:** `CreateSubscriptionLineItemRequest.Validate()` does not explicitly reject ONETIME + non-ADVANCE.

**Pre-condition confirmed:** `make migrate-ent` was never run after commit `427640b6`. The `billing_cadence` column does not exist in the database. No migration is needed to remove it.

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
| Phase plan-price loop: ONETIME defaults to `phase.StartDate` | `internal/service/subscription.go` | ✅ |
| `GetChargeDate()` returns `StartDate` | `internal/domain/subscription/line_item.go` | ✅ |

---

## Constraints

- No new DB columns or migrations (`billing_cadence` was never migrated; `billing_period` column exists and allows empty strings at the DB level)
- No changes to existing recurring billing logic
- Backward-compatible — all existing subscriptions continue to work

---

## Design

### 1. Remove `billing_cadence` from Ent Schema + Fix `billing_period` constraint

**File:** `ent/schema/subscription_line_item.go`

Two changes in this file:

**Remove the `billing_cadence` field entirely.** Since the column was never migrated, no DB migration is needed.

**Remove `NotEmpty()` from `billing_period`:**

```go
// Before
field.String("billing_period").
    SchemaType(map[string]string{"postgres": "varchar(50)"}).
    NotEmpty().
    GoType(types.BillingPeriod(""))

// After
field.String("billing_period").
    SchemaType(map[string]string{"postgres": "varchar(50)"}).
    GoType(types.BillingPeriod(""))
```

`NotEmpty()` is a **Go-level validator only** — the underlying `billing_period` DB column is `varchar(50)` with no DB-level NOT NULL or CHECK constraint, so empty strings can be stored. Removing `NotEmpty()` requires no migration.

After both changes, run `make generate-ent`.

---

### 2. Remove `BillingCadence` from Domain Model; Update `IsOneTime()`

**File:** `internal/domain/subscription/line_item.go`

- Remove the `BillingCadence types.BillingCadence` field from the `SubscriptionLineItem` struct.
- Remove the `BillingCadence` assignment in `SubscriptionLineItemFromEnt()`.
- Update `IsOneTime()`:

```go
// IsOneTime returns true if this line item is a one-time charge.
// ONETIME charges are FIXED type with no billing period.
// RECURRING FIXED charges always have an explicit BillingPeriod (MONTHLY, ANNUAL, etc.).
func (li *SubscriptionLineItem) IsOneTime() bool {
    return li.PriceType == types.PRICE_TYPE_FIXED && li.BillingPeriod == ""
}
```

**Safety invariant:** `BillingPeriod == ""` on a ONETIME line item is guaranteed by normalization in `ToSubscriptionLineItem()` (§3), which forces `BillingPeriod = ""` when the price's `BillingCadence` is ONETIME. This covers new prices and any existing prices that have a non-empty `BillingPeriod` from before this change.

`GetChargeDate()` is unchanged — still returns `li.StartDate`.

---

### 3. Update `ToSubscriptionLineItem()` — Normalize + Remove `BillingCadence`

**File:** `internal/api/dto/subscription_line_item.go`

Two changes in this method:

**3a. Remove `billingCadence` logic.** Remove the local `billingCadence` variable and all code that reads `params.Price.BillingCadence` to set it on the line item struct (the field no longer exists).

**3b. Normalize `BillingPeriod` for ONETIME prices.** After copying price fields to the line item, force `BillingPeriod = ""` and `BillingPeriodCount = 0` if the price is ONETIME:

```go
if params.Price != nil && params.Price.BillingCadence == types.BILLING_CADENCE_ONETIME {
    lineItem.BillingPeriod = ""
    lineItem.BillingPeriodCount = 0
}
```

This guarantees `IsOneTime()` returns true regardless of what `BillingPeriod` was stored on the price object.

The ONETIME-specific `invoiceCadence` default-to-ADVANCE logic can also be removed here — enforced upstream at price creation (§4).

---

### 4. Fix `CreatePriceRequest` for ONETIME

**File:** `internal/api/dto/price.go`

Three changes:

**4a. Remove `validate:"required"` struct tag from `BillingPeriod`:**

```go
// Before
BillingPeriod types.BillingPeriod `json:"billing_period" validate:"required"`

// After
BillingPeriod types.BillingPeriod `json:"billing_period"`
```

Safe because the `case BILLING_CADENCE_RECURRING` block already enforces `BillingPeriod != ""` for recurring prices.

**4b. Move the `BillingPeriodCount < 1` guard inside the `RECURRING` case.** Currently it fires before the switch — this would reject ONETIME requests where the caller omits `billing_period_count` (Go zero value is `0 < 1`):

```go
// Move this check out of its current pre-switch position
// and into case BILLING_CADENCE_RECURRING only:
case types.BILLING_CADENCE_RECURRING:
    if r.BillingPeriod == "" {
        return error...
    }
    if r.BillingPeriodCount < 1 {
        return ierr.NewError("billing period count must be greater than 0")...
    }
```

**4c. Add `case BILLING_CADENCE_ONETIME`:**

```go
case types.BILLING_CADENCE_ONETIME:
    if r.InvoiceCadence != "" && r.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("invoice_cadence must be ADVANCE for ONETIME prices").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
    r.BillingPeriod = ""
    r.BillingPeriodCount = 0
```

---

### 5. Fix `SubscriptionPriceCreateRequest` for Inline ONETIME Prices

**File:** `internal/api/dto/subscription_line_item.go`

`SubscriptionPriceCreateRequest` (used for inline price creation within line item requests) also has `validate:"required"` on `BillingPeriod`. Remove it:

```go
// Before
BillingPeriod types.BillingPeriod `json:"billing_period" validate:"required"`

// After
BillingPeriod types.BillingPeriod `json:"billing_period"`
```

---

### 6. Add Line Item Validation for ONETIME + Non-ADVANCE

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

Note: `price.BillingCadence` here is the `Price` domain object's field — not the (now-removed) `SubscriptionLineItem.BillingCadence`.

---

### 7. Update All Files Referencing `BillingCadence` on `SubscriptionLineItem`

Removing `BillingCadence` from the struct will cause **compile errors** in the following files. All references must be removed:

| File | Lines (approx) | What to change |
|---|---|---|
| `internal/service/billing_onetime_test.go` | 138–152, 193–225, 374–391, 584–592 | Fix `setupSharedFixtures`: set `BillingPeriod: ""` on ONETIME price fixtures. Fix `makeOnetimeLineItem`: remove `BillingCadence`, remove `BillingPeriod` (zero value). Remove `TestLineItemFromEnt_DefaultsToRecurring`, `TestLineItemFromEnt_OnetimeCadence`, `TestOnetime_BillingCadenceStoredOnLineItem`, `TestRecurring_BillingCadenceDefault`. |
| `internal/service/billing_test.go` | ~204, 228, 245, 262, 806, 823, 992 | Remove `BillingCadence: types.BILLING_CADENCE_RECURRING` from line item literals |
| `internal/service/proration_test.go` | ~91, 105 | Remove `BillingCadence` field |
| `internal/service/invoice_void_recalculate_test.go` | ~139 | Remove `BillingCadence` field |
| `internal/service/subscription_phase_test.go` | ~65, 191 | Remove `BillingCadence` field |
| `internal/service/subscription_schedule_test.go` | ~99, 198 | Remove `BillingCadence` field |
| `internal/integration/stripe/subscription.go` | ~453, 515 | Remove `BillingCadence` field — **production code** |
| `internal/temporal/models/workflow.go` | ~729 | Remove `BillingCadence` field — **production code** |

For RECURRING line items: simply remove the `BillingCadence` field — their non-empty `BillingPeriod` ensures `IsOneTime()` returns false correctly.
For ONETIME line items: remove `BillingCadence` and ensure `BillingPeriod: ""` (zero value — omit the field).

---

### 8. Verify `CalculateFixedCharges` (No Change Expected)

**File:** `internal/service/billing.go`

`CalculateFixedCharges()` calls `IsOneTime()` on line items. After the change, `IsOneTime()` returns `li.PriceType == FIXED && li.BillingPeriod == ""`. The method also fetches the price and has `price.BillingCadence` available — verify the ONETIME branch still fires correctly. No code change expected.

---

## Change Summary

| # | File | Change |
|---|---|---|
| 1 | `ent/schema/subscription_line_item.go` | Remove `billing_cadence` field; remove `NotEmpty()` from `billing_period` |
| 2 | `internal/domain/subscription/line_item.go` | Remove `BillingCadence` field + `FromEnt` assignment; update `IsOneTime()` |
| 3 | `internal/api/dto/subscription_line_item.go` | Normalize `BillingPeriod=""` for ONETIME in `ToSubscriptionLineItem()`; remove `BillingCadence` logic; add ONETIME+ADVANCE check in `Validate()`; remove `validate:"required"` from `SubscriptionPriceCreateRequest.BillingPeriod` |
| 4 | `internal/api/dto/price.go` | Remove `validate:"required"` from `BillingPeriod`; move `BillingPeriodCount<1` guard into RECURRING case; add `case BILLING_CADENCE_ONETIME` |
| 5 | `internal/service/billing_onetime_test.go` | Fix fixtures; fix `makeOnetimeLineItem`; remove 4 `BillingCadence`-specific test cases |
| 6 | `internal/service/billing_test.go` | Remove `BillingCadence` from ~7 line item literals |
| 7 | `internal/service/proration_test.go` | Remove `BillingCadence` from ~2 literals |
| 8 | `internal/service/invoice_void_recalculate_test.go` | Remove `BillingCadence` from ~1 literal |
| 9 | `internal/service/subscription_phase_test.go` | Remove `BillingCadence` from ~2 literals |
| 10 | `internal/service/subscription_schedule_test.go` | Remove `BillingCadence` from ~2 literals |
| 11 | `internal/integration/stripe/subscription.go` | Remove `BillingCadence` from ~2 line item structs |
| 12 | `internal/temporal/models/workflow.go` | Remove `BillingCadence` from ~1 line item struct |
| 13 | Run `make generate-ent` | Regenerate Ent code after schema change |
| 14 | Run `make test` | Verify no regressions |

---

## Non-Goals

- No new DB migration
- No changes to `CalculateFixedCharges`, `ClassifyLineItems`, or any other billing pipeline logic
- No changes to invoice generation, proration, or coupon logic
- Out of scope: `UpdateSubscriptionLineItemRequest.ToSubscriptionLineItem()` pre-existing bug (does not copy all fields on update) — unrelated to this feature
