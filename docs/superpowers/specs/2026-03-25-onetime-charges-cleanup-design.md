# One-Time Charges: Cleanup & Validation Design

**Date:** 2026-03-25
**Branch:** `feat/onetime-charges`
**Status:** Approved (v2 — post spec review)

---

## Context

The two most recent commits (`427640b6`, `d5265655`) implemented one-time charge support for subscriptions. An audit revealed:

- **Structural issue:** A `billing_cadence` field was added to the `subscription_line_items` ent schema but was **never applied to the database** (no migration run) and was **never written by the repository** (no `SetBillingCadence()` call in Create/Update). It must be removed cleanly and replaced with a derived discriminator.
- **Validation gap 1:** `CreatePriceRequest.Validate()` does not reject `BILLING_CADENCE_ONETIME` with a non-`ADVANCE` invoice cadence.
- **Validation gap 2:** `CreateSubscriptionLineItemRequest.Validate()` does not explicitly reject ONETIME + non-ADVANCE.

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

- No new DB columns or migrations (the `billing_cadence` column was never migrated; `billing_period` column already exists)
- No changes to existing recurring billing logic
- Backward-compatible — all existing subscriptions continue to work

---

## Design

### 1. Remove `billing_cadence` from Ent Schema

**File:** `ent/schema/subscription_line_item.go`

Remove the `billing_cadence` field definition entirely. Since this column was never migrated, no DB migration is needed. The generated Ent code referencing `BillingCadence` will be removed after `make generate-ent`.

While here, also remove `NotEmpty()` from the `billing_period` field:

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

`NotEmpty()` is a **Go-level validator only** — it adds no DB constraint. The underlying `billing_period` column is `varchar(50)` with no DB-level NOT NULL or CHECK constraint, so empty strings can be stored. Removing it requires no migration. This is necessary to allow ONETIME line items to be stored with `BillingPeriod = ""`.

After both changes, run `make generate-ent`.

---

### 2. Remove `BillingCadence` from Domain Model and Ent Conversion

**File:** `internal/domain/subscription/line_item.go`

- Remove the `BillingCadence types.BillingCadence` field from the `SubscriptionLineItem` struct.
- Remove the `BillingCadence` assignment in `SubscriptionLineItemFromEnt()` (the line that defaults to `RECURRING` when empty).
- Update `IsOneTime()` to derive ONETIME status from `BillingPeriod`:

```go
// IsOneTime returns true if this line item is a one-time charge.
// ONETIME charges are FIXED type with no billing period.
// RECURRING FIXED charges always have an explicit BillingPeriod (MONTHLY, ANNUAL, etc.).
func (li *SubscriptionLineItem) IsOneTime() bool {
    return li.PriceType == types.PRICE_TYPE_FIXED && li.BillingPeriod == ""
}
```

This works because:
- ONETIME prices always have `BillingPeriod = ""` (enforced at price creation — see §4)
- RECURRING FIXED prices always have a non-empty `BillingPeriod` (enforced by existing validation in `CreatePriceRequest.Validate()` — the `case BILLING_CADENCE_RECURRING` block requires `BillingPeriod != ""`)
- `BillingPeriod` is a persisted column on `subscription_line_items` and is already copied from price → line item during creation

`GetChargeDate()` is unchanged — still returns `li.StartDate`.

---

### 3. Remove `BillingCadence` from DTO Conversion

**File:** `internal/api/dto/subscription_line_item.go` — `ToSubscriptionLineItem()`

Remove the `billingCadence` local variable and all code that reads `params.Price.BillingCadence` to set it on the line item. The field no longer exists on the struct.

The ONETIME-specific `invoiceCadence` default-to-ADVANCE logic can also be removed here — it is now enforced upstream at price creation (§4 ensures ONETIME prices are always stored with `InvoiceCadence = ADVANCE`).

---

### 4. Fix `CreatePriceRequest` for ONETIME

**File:** `internal/api/dto/price.go`

Two changes:

**4a. Remove `validate:"required"` struct tag from `BillingPeriod`:**

```go
// Before
BillingPeriod types.BillingPeriod `json:"billing_period" validate:"required"`

// After
BillingPeriod types.BillingPeriod `json:"billing_period"`
```

The `validate:"required"` tag fires via go-playground/validator before the manual `Validate()` call, which would reject ONETIME price creation requests with no `billing_period`. It is safe to remove because the `case BILLING_CADENCE_RECURRING` block in `Validate()` already enforces `BillingPeriod != ""` for recurring prices.

**4b. Add `case BILLING_CADENCE_ONETIME` to the switch block in `Validate()`:**

```go
case types.BILLING_CADENCE_ONETIME:
    if r.InvoiceCadence != "" && r.InvoiceCadence != types.InvoiceCadenceAdvance {
        return ierr.NewError("invoice_cadence must be ADVANCE for ONETIME prices").
            WithHint("One-time charges are always billed in advance").
            Mark(ierr.ErrValidation)
    }
    // Clear period fields — ONETIME charges have no billing period
    r.BillingPeriod = ""
    r.BillingPeriodCount = 0
```

Note: `r.BillingPeriodCount = 0` is set after the `BillingPeriodCount < 1` validation (which runs earlier), so it only affects what gets persisted. The persisted `0` is safe — `SubscriptionLineItemFromEnt()` has a default-to-1 for `billing_period_count` which only applies to line items, not prices. Price consumers check `BillingPeriod` first; if empty they treat the price as ONETIME and don't use `BillingPeriodCount`.

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

This acts as defense-in-depth. Note: `price.BillingCadence` here refers to the `Price` domain object passed into `Validate()` — not the line item's `BillingCadence` field (which is being removed).

---

### 6. Update Tests

**File:** `internal/service/billing_onetime_test.go`

Specific changes required:

- **`makeOnetimeLineItem()` helper** — remove `BillingCadence: types.BILLING_CADENCE_ONETIME` and remove `BillingPeriod: types.BILLING_PERIOD_MONTHLY`. ONETIME line items must have `BillingPeriod: ""` (zero value) for `IsOneTime()` to return true.

- **`makeRecurringLineItem()` helper** — ensure `BillingPeriod: types.BILLING_PERIOD_MONTHLY` is set (it likely already is, but verify).

- **`TestLineItemFromEnt_DefaultsToRecurring`** and **`TestLineItemFromEnt_OnetimeCadence`** — these directly assert `item.BillingCadence`. Remove these test cases entirely or rewrite them to test `IsOneTime()` using `BillingPeriod`.

- **`TestOnetime_BillingCadenceStoredOnLineItem`** and **`TestRecurring_BillingCadenceDefault`** (around lines 584–592) — remove; they test the deleted field.

- **All other ONETIME test cases** (Groups 1–5, ~12 callsites of `makeOnetimeLineItem`) — these will automatically pass once `makeOnetimeLineItem` is fixed, since `IsOneTime()` will return true when `BillingPeriod == ""`.

---

### 7. Verify `CalculateFixedCharges` (No Change Expected)

**File:** `internal/service/billing.go`

`CalculateFixedCharges()` calls `IsOneTime()` on line items. After the change, `IsOneTime()` returns `li.PriceType == FIXED && li.BillingPeriod == ""`. The method also independently fetches the price object and has access to `price.BillingCadence` — verify that the ONETIME branch (`if item.IsOneTime()`) still fires correctly. No code change expected.

---

## Change Summary

| # | File | Change |
|---|---|---|
| 1 | `ent/schema/subscription_line_item.go` | Remove `billing_cadence` field; remove `NotEmpty()` from `billing_period` |
| 2 | `internal/domain/subscription/line_item.go` | Remove `BillingCadence` field + `FromEnt` assignment; update `IsOneTime()` |
| 3 | `internal/api/dto/subscription_line_item.go` | Remove `BillingCadence` from `ToSubscriptionLineItem()`; add ONETIME+ADVANCE validation in `Validate()` |
| 4 | `internal/api/dto/price.go` | Remove `validate:"required"` from `BillingPeriod`; add `case BILLING_CADENCE_ONETIME` to `Validate()` switch |
| 5 | `internal/service/billing_onetime_test.go` | Fix `makeOnetimeLineItem` helper; remove `BillingCadence`-specific test cases |
| 6 | Run `make generate-ent` | Regenerate Ent code after schema change |
| 7 | Run `make test` | Verify no regressions |

---

## Non-Goals

- No changes to `CalculateFixedCharges`, `ClassifyLineItems`, or any other billing pipeline logic
- No new DB migration (removing `NotEmpty()` from ent schema is Go-only, no DB change)
- No changes to invoice generation, proration, or coupon logic
- Out of scope: `UpdateSubscriptionLineItemRequest.ToSubscriptionLineItem()` pre-existing bug (BillingCadence not copied on update) — unrelated to this feature, tracked separately
