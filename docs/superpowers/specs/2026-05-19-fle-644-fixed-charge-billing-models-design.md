# FLE-644: Fixed Charge Billing Models — Integration Test Design

**Date:** 2026-05-19
**Branch:** FLE-644
**Assignee:** omkar
**Priority:** Urgent
**Due:** 2026-05-20
**Customer:** Aruba (new, revenue not yet known)

---

## Goal

Verify that all fixed-charge billing models (`FLAT_FEE`, `PACKAGE`, `TIERED`) work correctly end-to-end:
- DTO validation at the API layer (both rejection and acceptance)
- Service logic correctness
- Invoice line item math for first invoice

This is done via scenario-based integration tests that flow: **create plan → attach fixed-charge prices → create subscription → assert first invoice**.

---

## Scope

- `PRICE_TYPE_FIXED` only (not usage charges)
- All three billing models: `FLAT_FEE`, `PACKAGE`, `TIERED` (both `VOLUME` and `SLAB` tier modes)
- Both invoice cadences: `ADVANCE` and `ARREAR`
- Multiple billing periods: `MONTHLY`, `ANNUAL`
- Mixed plan scenario (multiple fixed prices on one plan)
- DTO validation: rejection of invalid inputs, acceptance of valid inputs

Out of scope: usage-based charge codepaths, new API endpoints, refactoring existing billing functions.

---

## Test Scenarios

| # | Billing Model | Tier Mode | Cadence | Period | Description |
|---|---|---|---|---|---|
| 1 | `FLAT_FEE` | — | `ADVANCE` | Monthly | Bill at period start |
| 2 | `FLAT_FEE` | — | `ARREAR` | Monthly | Bill at period end |
| 3 | `PACKAGE` | — | `ADVANCE` | Monthly | Package rounding math |
| 4 | `PACKAGE` | — | `ARREAR` | Monthly | Package math, end-of-cycle invoice |
| 5 | `TIERED` | `SLAB` | `ADVANCE` | Monthly | Progressive tier math |
| 6 | `TIERED` | `VOLUME` | `ADVANCE` | Monthly | All units at final tier |
| 7 | `FLAT_FEE` | — | `ADVANCE` | Annual | Full annual period amount |
| 8 | Mixed (`FLAT_FEE` + `TIERED`) | `SLAB` | `ADVANCE` | Monthly | Two line items on one invoice |

---

## Test Data & Expected Math

### FLAT_FEE
- `amount = $100`, `quantity = 3`
- Expected: `line_item.amount = $300`

### PACKAGE
- `amount = $50/package`, `transform_quantity.divide_by = 10`, `round = up`, `quantity = 25`
- Expected: `ceil(25/10) = 3 packages → $150`

### TIERED SLAB
- Tiers: `[{up_to: 10, unit_amount: $5}, {up_to: 50, unit_amount: $3}]`
- `quantity = 20`
- Expected: `(10 × $5) + (10 × $3) = $80`

### TIERED VOLUME
- Same tiers, `quantity = 20`
- Expected: all 20 units at tier-2 rate → `20 × $3 = $60`

### Annual FLAT_FEE
- `amount = $1200`, `billing_period = ANNUAL`, `quantity = 1`
- Expected: `$1200` for full annual period

### Mixed Plan
- Price 1: FLAT_FEE `$100 × 1`
- Price 2: TIERED SLAB, quantity=20 → `$80`
- Expected: 2 line items, total `$180`

### Cadence Periods
- **ADVANCE**: `period_start = subscription.start_date`, `period_end = end_of_first_cycle`; invoice generated at subscription start
- **ARREAR**: `period_start = subscription.start_date`, `period_end = end_of_first_cycle`; invoice generated at cycle end

---

## DTO Validation Cases

### Rejection (400 errors)
| Input | Expected Error |
|---|---|
| `billing_model = ""` | validation error — required field |
| `billing_model = TIERED` + no `tiers` | validation error — tiers required for TIERED model |
| `billing_model = FLAT_FEE` + `amount = 0` | validation error — amount must be > 0 |

### Acceptance (pass-through to service)
| Input | Expected |
|---|---|
| `billing_model = TIERED` + valid `tiers` array + `amount = 0` | valid — per-tier pricing, top-level amount not required |
| `billing_model = PACKAGE` + valid `transform_quantity` | valid — passes to service |
| `billing_model = FLAT_FEE` + `amount > 0` | valid |

---

## File Structure

### New Integration Test File
```
internal/service/fixed_charge_billing_test.go
```
Uses `testutil.SetupTestDB()`. Each test is self-contained (seeds its own plan, prices, subscription).

### DTO Validation Tests
```
internal/api/v1/price_test.go
```
Tests `CreatePriceRequest` binding and validation logic.

### Test Helpers (within fixed_charge_billing_test.go)

```go
createPlanWithFixedPrice(t, svc, billingModel, cadence, period, tiers, transformQty) (planID, priceID string)
createTestCustomer(t, svc, name string) customerID string
createSubscription(t, svc, customerID, planID string, startTime time.Time) subscriptionID string
generateFirstInvoice(t, svc, subscriptionID string) *invoice.Invoice
assertLineItem(t, invoice, expectedAmount, expectedQty float64, expectedPeriodStart, expectedPeriodEnd time.Time)
```

Each test seeds its own data, no shared state between scenarios.

---

## Bug Fix Protocol

During verification, if a codepath produces wrong output:
1. Document the failing scenario (model + cadence + input)
2. Trace through `CalculateFixedCharges()` → `PriceService.CalculateCost()` → model branch
3. Fix the bug in place
4. The integration test becomes the regression guard

---

## Acceptance Criteria

- All 8 scenarios pass with correct line item amounts
- Both DTO rejection and acceptance cases assert correct behavior
- No existing billing tests regress
- `go vet ./...` passes
