# One-Time Charges Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the never-persisted `billing_cadence` field from subscription line items, replace `IsOneTime()` with a derivation from `BillingPeriod == ""`, and add missing ONETIME+ADVANCE validations.

**Architecture:** The `billing_cadence` column was added to the ent schema but never migrated to the DB and never written by the repository. We remove it cleanly and instead derive ONETIME status from `BillingPeriod == ""` on the line item — guaranteed by a new normalization step in `ToSubscriptionLineItem()`. Two validation gaps (price creation + line item creation) are plugged simultaneously.

**Tech Stack:** Go 1.23, Ent ORM, Gin, go-playground/validator. Run `make generate-ent` after schema changes. Run `go test ./...` or `make test` to verify.

**Spec:** `docs/superpowers/specs/2026-03-25-onetime-charges-cleanup-design.md`

---

## File Map

| File | Change type | What changes |
|---|---|---|
| `ent/schema/subscription_line_item.go` | Modify | Remove `billing_cadence` field; remove `NotEmpty()` from `billing_period` |
| `internal/domain/subscription/line_item.go` | Modify | Remove `BillingCadence` field + `FromEnt` assignment; update `IsOneTime()` |
| `internal/api/dto/subscription_line_item.go` | Modify | `ToSubscriptionLineItem()`: remove billingCadence local var, fix line 554 reference, add ONETIME normalization; `Validate()`: add ONETIME+ADVANCE check; `SubscriptionPriceCreateRequest`: remove `validate:"required"` |
| `internal/api/dto/price.go` | Modify | Remove `validate:"required"` from `BillingPeriod`; add `BillingPeriodCount<1` guard into RECURRING case only; add `case BILLING_CADENCE_ONETIME` |
| `internal/service/billing_onetime_test.go` | Modify | Fix price fixtures; fix helpers + inline line item literals; remove 4 stale test functions |

> **Files confirmed NOT needing changes:** `billing_test.go`, `proration_test.go`, `invoice_void_recalculate_test.go`, `subscription_phase_test.go`, `subscription_schedule_test.go`. All `BillingCadence` references in those files are on `price.Price{}`, `subscription.Subscription{}`, or `PlanChangeConfiguration{}` structs — not on `SubscriptionLineItem{}`.

---

## Task 1: Remove `billing_cadence` from Ent Schema; Fix `billing_period` Constraint

**Files:**
- Modify: `ent/schema/subscription_line_item.go`

Context: `billing_cadence` was added to the schema but never migrated. `billing_period` has `NotEmpty()` (Go-level only, no DB constraint) which must be removed to allow `BillingPeriod = ""` for ONETIME line items.

- [ ] **Step 1.1: Remove `billing_cadence` field**

In `ent/schema/subscription_line_item.go`, find and delete the entire `billing_cadence` field block (starting around line 184):

```go
// DELETE all of this:
// billing_cadence mirrors the price's billing cadence (RECURRING or ONETIME).
// Stored here to avoid price lookups during invoice classification.
// Default is RECURRING for backwards compatibility.
field.String("billing_cadence").
    SchemaType(map[string]string{
        "postgres": "varchar(20)",
    }).
    Default(string(types.BILLING_CADENCE_RECURRING)).
    GoType(types.BillingCadence("")),
```

- [ ] **Step 1.2: Remove `NotEmpty()` from `billing_period`**

In the same file, find `billing_period` (around line 110) and remove only `.NotEmpty()`:

```go
// Before:
field.String("billing_period").
    SchemaType(map[string]string{
        "postgres": "varchar(50)",
    }).
    NotEmpty().
    GoType(types.BillingPeriod("")),

// After:
field.String("billing_period").
    SchemaType(map[string]string{
        "postgres": "varchar(50)",
    }).
    GoType(types.BillingPeriod("")),
```

- [ ] **Step 1.3: Regenerate Ent code**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice
make generate-ent
```

Expected: exits 0. The generated files in `ent/` will no longer reference `BillingCadence` on subscription line items.

- [ ] **Step 1.4: Verify build fails with expected errors only**

```bash
go build ./...
```

Expected: compile errors about `BillingCadence` field on `subscription.SubscriptionLineItem` in `internal/domain/` and `internal/service/billing_onetime_test.go`. **Do not fix these now — they are expected and will be resolved in Tasks 2 and 6.**

---

## Task 2: Remove `BillingCadence` from Domain Model; Update `IsOneTime()`

**Files:**
- Modify: `internal/domain/subscription/line_item.go`

- [ ] **Step 2.1: Remove `BillingCadence` field from struct**

Find and delete this field from the `SubscriptionLineItem` struct:

```go
// DELETE:
// BillingCadence mirrors the price's billing cadence (RECURRING or ONETIME).
// For ONETIME charges, StartDate is used as the charge date.
BillingCadence types.BillingCadence `db:"billing_cadence" json:"billing_cadence"`
```

- [ ] **Step 2.2: Remove `BillingCadence` assignment in `SubscriptionLineItemFromEnt()`**

Find and delete these lines (around line 162):

```go
// DELETE all of this:
billingCadence := types.BillingCadence(e.BillingCadence)
if billingCadence == "" {
    billingCadence = types.BILLING_CADENCE_RECURRING
}
```

Also delete the `BillingCadence: billingCadence` line in the returned struct.

- [ ] **Step 2.3: Update `IsOneTime()`**

```go
// Before:
func (li *SubscriptionLineItem) IsOneTime() bool {
    return li.BillingCadence == types.BILLING_CADENCE_ONETIME
}

// After:
func (li *SubscriptionLineItem) IsOneTime() bool {
    // ONETIME charges are FIXED type with no billing period.
    // RECURRING FIXED charges always have an explicit BillingPeriod (MONTHLY, ANNUAL, etc.).
    return li.PriceType == types.PRICE_TYPE_FIXED && li.BillingPeriod == ""
}
```

- [ ] **Step 2.4: Verify domain package compiles**

```bash
go build ./internal/domain/...
```

Expected: compiles. Remaining errors are in `internal/api/dto/` and test files — fixed in later tasks.

---

## Task 3: Update `ToSubscriptionLineItem()` — Remove `billingCadence`, Add Normalization, Fix line 554

**Files:**
- Modify: `internal/api/dto/subscription_line_item.go`

Context: Three changes in `ToSubscriptionLineItem()`:
1. Remove the `billingCadence` local variable block (lines 459–471)
2. Fix the `billingCadence` reference at line 554 (charge_date override uses the now-deleted variable)
3. Add ONETIME normalization after struct init to guarantee `BillingPeriod = ""`

- [ ] **Step 3.1: Remove `billingCadence` block and simplify `invoiceCadence`**

Find the block starting around line 459:
```go
// Resolve BillingCadence and InvoiceCadence from price
billingCadence := types.BILLING_CADENCE_RECURRING
invoiceCadence := types.InvoiceCadenceAdvance
if params.Price != nil {
    if params.Price.BillingCadence != "" {
        billingCadence = params.Price.BillingCadence
    }
    invoiceCadence = params.Price.InvoiceCadence
    // ONETIME charges default to ADVANCE invoice cadence if not explicitly set
    if billingCadence == types.BILLING_CADENCE_ONETIME && invoiceCadence == "" {
        invoiceCadence = types.InvoiceCadenceAdvance
    }
}
```

Replace with (keep only `invoiceCadence`):
```go
// Resolve InvoiceCadence from price
invoiceCadence := types.InvoiceCadenceAdvance
if params.Price != nil {
    invoiceCadence = params.Price.InvoiceCadence
}
```

- [ ] **Step 3.2: Remove `BillingCadence` from `lineItem` struct literal**

In the `lineItem := &subscription.SubscriptionLineItem{...}` block (around line 473), delete the line:
```go
BillingCadence:      billingCadence,
```

- [ ] **Step 3.3: Fix `billingCadence` reference at line 554**

Find this block (the charge_date override for ONETIME):
```go
// charge_date overrides everything for ONETIME charges (it is the exact billing date)
if billingCadence == types.BILLING_CADENCE_ONETIME && r.ChargeDate != nil {
    startDate = r.ChargeDate.UTC()
}
```

Replace `billingCadence ==` with a price lookup:
```go
// charge_date overrides everything for ONETIME charges (it is the exact billing date)
if params.Price != nil && params.Price.BillingCadence == types.BILLING_CADENCE_ONETIME && r.ChargeDate != nil {
    startDate = r.ChargeDate.UTC()
}
```

- [ ] **Step 3.4: Add ONETIME normalization after struct initialization**

Immediately after the closing `}` of the `lineItem := &subscription.SubscriptionLineItem{...}` block, add:

```go
// Normalize ONETIME line items: clear billing period so IsOneTime() works correctly.
// RECURRING prices always have a non-empty BillingPeriod; ONETIME prices do not.
if params.Price != nil && params.Price.BillingCadence == types.BILLING_CADENCE_ONETIME {
    lineItem.BillingPeriod = ""
    lineItem.BillingPeriodCount = 0
}
```

- [ ] **Step 3.5: Verify `internal/api/dto` compiles**

```bash
go build ./internal/api/...
```

Expected: compiles cleanly.

---

## Task 4: Fix `CreatePriceRequest` Validation for ONETIME

**Files:**
- Modify: `internal/api/dto/price.go`

- [ ] **Step 4.1: Remove `validate:"required"` from `BillingPeriod` struct tag**

Find line 24 in `CreatePriceRequest`:
```go
// Before:
BillingPeriod      types.BillingPeriod      `json:"billing_period" validate:"required"`

// After:
BillingPeriod      types.BillingPeriod      `json:"billing_period"`
```

- [ ] **Step 4.2: Move `BillingPeriodCount < 1` guard into the RECURRING case**

Delete the pre-switch guard at lines ~204–211:
```go
// DELETE:
if r.BillingPeriodCount < 1 {
    return ierr.NewError("billing period count must be greater than 0").
        WithHint("Billing period count must be greater than 0").
        WithReportableDetails(map[string]interface{}{
            "billing_period_count": r.BillingPeriodCount,
        }).
        Mark(ierr.ErrValidation)
}
```

Then find the **existing** `case types.BILLING_CADENCE_RECURRING:` block (around line 359 — it already contains a `BillingPeriod == ""` check). Add the `BillingPeriodCount` guard **inside** it:

```go
// The case already exists — only ADD the BillingPeriodCount check:
case types.BILLING_CADENCE_RECURRING:
    if r.BillingPeriod == "" {
        return ierr.NewError("billing_period is required when billing_cadence is RECURRING").
            WithHint("Please select a billing period to set up recurring pricing").
            Mark(ierr.ErrValidation)
    }
    // ADD THIS:
    if r.BillingPeriodCount < 1 {
        return ierr.NewError("billing period count must be greater than 0").
            WithHint("Billing period count must be greater than 0").
            WithReportableDetails(map[string]interface{}{
                "billing_period_count": r.BillingPeriodCount,
            }).
            Mark(ierr.ErrValidation)
    }
```

- [ ] **Step 4.3: Add `case BILLING_CADENCE_ONETIME` to the switch**

In the same switch block, after the `RECURRING` case, add a new case:
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

- [ ] **Step 4.4: Verify build**

```bash
go build ./internal/api/...
```

Expected: compiles.

---

## Task 5: Fix `SubscriptionPriceCreateRequest` + Add ONETIME Validation in `Validate()`

**Files:**
- Modify: `internal/api/dto/subscription_line_item.go`

- [ ] **Step 5.1: Remove `validate:"required"` from `SubscriptionPriceCreateRequest.BillingPeriod`**

Find line 19:
```go
// Before:
BillingPeriod      types.BillingPeriod      `json:"billing_period" validate:"required"`

// After:
BillingPeriod      types.BillingPeriod      `json:"billing_period"`
```

- [ ] **Step 5.2: Add ONETIME + non-ADVANCE rejection to `Validate()`**

Find the end of the charge_date bounds check (around line 235 — the closing `}` of `if price != nil && price.BillingCadence == types.BILLING_CADENCE_ONETIME && r.ChargeDate != nil`). Immediately after it, add:

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

Note: `price.BillingCadence` here is the `Price` domain object's field — not the removed `SubscriptionLineItem.BillingCadence`.

- [ ] **Step 5.3: Verify build**

```bash
go build ./internal/api/...
```

Expected: compiles cleanly.

---

## Task 6: Fix `billing_onetime_test.go`

**Files:**
- Modify: `internal/service/billing_onetime_test.go`

Context: This is the only test file with `BillingCadence` on `SubscriptionLineItem` struct literals. The changes fall into three groups:
- Price fixtures in `setupSharedFixtures` — `BillingCadence` stays on `price.Price` but `BillingPeriod` must be cleared
- Helper functions and inline literals — remove `BillingCadence` from `SubscriptionLineItem` literals
- 4 stale test functions testing the deleted field — delete them entirely

**Summary of all `BillingCadence` lines in this file:**
- Lines 131, 148, 165: on `price.Price{}` structs — `BillingCadence` stays, only `BillingPeriod`/`BillingPeriodCount` change on lines 148 and 165
- Lines 200, 217: `makeOnetimeLineItem()` and `makeRecurringLineItem()` helpers — `BillingCadence` removed
- Lines 371–391: stale test functions — delete entire functions
- Lines 410, 437, 468, 494, 523, 566: inline `SubscriptionLineItem` literals — remove `BillingCadence`
- Lines 584–591: stale test functions — delete

- [ ] **Step 6.1: Fix `setupSharedFixtures` — clear `BillingPeriod` on ONETIME prices**

Find `onetimeFixed` price (~line 138). Change `BillingPeriod` and `BillingPeriodCount` (keep `BillingCadence` — it's on `price.Price`, not `SubscriptionLineItem`):
```go
// onetimeFixed: change these two lines
BillingPeriod:      "",   // was types.BILLING_PERIOD_MONTHLY
BillingPeriodCount: 0,    // was 1
```

Apply the same change to `onetimeArrear` (~line 155):
```go
BillingPeriod:      "",   // was types.BILLING_PERIOD_MONTHLY
BillingPeriodCount: 0,    // was 1
```

- [ ] **Step 6.2: Fix `makeOnetimeLineItem()` helper**

Remove `BillingCadence` and `BillingPeriod` fields (both are on the `SubscriptionLineItem` literal):
```go
// Before:
return &subscription.SubscriptionLineItem{
    ...
    BillingCadence: types.BILLING_CADENCE_ONETIME,
    InvoiceCadence: cadence,
    BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
    ...
}

// After:
return &subscription.SubscriptionLineItem{
    ...
    // BillingPeriod intentionally omitted (zero value "") — IsOneTime() uses PriceType==FIXED && BillingPeriod==""
    InvoiceCadence: cadence,
    ...
}
```

- [ ] **Step 6.3: Fix `makeRecurringLineItem()` helper**

Remove only `BillingCadence` (line 217). Keep `BillingPeriod: types.BILLING_PERIOD_MONTHLY` — the non-empty period is what makes `IsOneTime()` return false for recurring items:
```go
// Remove this line from makeRecurringLineItem():
BillingCadence: types.BILLING_CADENCE_RECURRING,
```

- [ ] **Step 6.4: Remove `BillingCadence` from all inline `SubscriptionLineItem` literals**

The following lines all set `BillingCadence` on inline `&subscription.SubscriptionLineItem{}` literals. Delete the `BillingCadence` field from each. For ONETIME items (lines 410, 437, 468, 494, 566) also ensure `BillingPeriod` is absent/`""`. For the RECURRING item (line 523) keep the non-empty `BillingPeriod`.

Lines to update: **410, 437, 468, 494, 523, 566**

- [ ] **Step 6.5: Delete 4 stale test functions**

Delete the following functions in their entirety:
- `TestLineItemFromEnt_DefaultsToRecurring` (~line 374, tests the deleted field default)
- `TestLineItemFromEnt_OnetimeCadence` (~line 386, tests the deleted field)
- `TestOnetime_BillingCadenceStoredOnLineItem` (~line 584)
- `TestRecurring_BillingCadenceDefault` (~line 589)

Also delete the `// Group 3: BillingCadence stored on domain model` comment (~line 371).

- [ ] **Step 6.6: Full build passes**

```bash
go build ./...
```

Expected: **zero compile errors** across all packages.

---

## Task 7: Run Tests and Commit

- [ ] **Step 7.1: Run the full test suite**

```bash
cd /Users/omkar/Developer/source-code/flexprice/flexprice
make test
```

Expected: all tests pass. Pay attention to:
- `internal/service` — all `billing_onetime` classification tests should pass (they rely on `IsOneTime()` via `makeOnetimeLineItem` which now has `BillingPeriod: ""`)
- `internal/api/dto` — no DTO tests broken
- `internal/service/billing_test.go` — recurring billing unchanged

If a test fails with `IsOneTime()` returning false for an ONETIME item, check that the `SubscriptionLineItem` has `PriceType: types.PRICE_TYPE_FIXED` and `BillingPeriod: ""` — both are required.

- [ ] **Step 7.2: Commit**

```bash
git add \
  ent/schema/subscription_line_item.go \
  internal/domain/subscription/line_item.go \
  internal/api/dto/subscription_line_item.go \
  internal/api/dto/price.go \
  internal/service/billing_onetime_test.go
# Include all regenerated ent/ files:
git add ent/

git commit -m "$(cat <<'EOF'
refactor(billing): remove billing_cadence from subscription line items

- Remove billing_cadence ent schema field (was never migrated to DB)
- Remove NotEmpty() from billing_period to allow ONETIME line items
- Update IsOneTime() to derive from PriceType==FIXED && BillingPeriod==""
- Normalize BillingPeriod="" in ToSubscriptionLineItem() for ONETIME prices
- Fix billingCadence variable reference at charge_date override (line 554)
- Add ONETIME+ADVANCE validation in price creation and line item creation
- Move BillingPeriodCount guard into RECURRING case only
- Fix ONETIME price fixtures and helpers in billing_onetime_test.go

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Quick Reference: `IsOneTime()` After This Change

```
IsOneTime() returns true when:
  li.PriceType == PRICE_TYPE_FIXED   (flat charge, not usage-based)
  && li.BillingPeriod == ""          (no recurring interval)

Guaranteed by:
  ToSubscriptionLineItem() sets BillingPeriod="" for any price with BillingCadence==ONETIME
  CreatePriceRequest.Validate() clears BillingPeriod for ONETIME prices at creation time
```
