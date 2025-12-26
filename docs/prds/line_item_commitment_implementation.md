# Line-Item Commitment Implementation Design

## Overview
Extend commitment pricing from subscription-level to line-item-level, supporting both **non-window** and **window-based** commitment models.

---

## 1. Data Model Changes

### 1.1 SubscriptionLineItem Schema
Add to `internal/domain/subscription/line_item.go`:

```go
// Commitment fields
CommitmentAmount    *decimal.Decimal        `db:"commitment_amount" json:"commitment_amount,omitempty"`
CommitmentQuantity  *decimal.Decimal        `db:"commitment_quantity" json:"commitment_quantity,omitempty"`
CommitmentType      types.CommitmentType    `db:"commitment_type" json:"commitment_type,omitempty"`
OverageFactor       *decimal.Decimal        `db:"overage_factor" json:"overage_factor,omitempty"`
EnableTrueUp        bool                    `db:"enable_true_up" json:"enable_true_up"`
IsWindowCommitment  bool                    `db:"is_window_commitment" json:"is_window_commitment"`
```

### 1.2 New Type Definition
Add to `internal/types/commitment.go`:

```go
type CommitmentType string

const (
    COMMITMENT_TYPE_AMOUNT   CommitmentType = "amount"
    COMMITMENT_TYPE_QUANTITY CommitmentType = "quantity"
)
```

**Rationale**: 
- Mirrors existing subscription-level commitment pattern (lines 96-122 in `subscription/model.go`)
- Supports both amount and quantity-based commitments
- Store commitment as amount/quantity pair + type indicator

---

## 2. Validation Rules

### 2.1 Line Item Creation/Update Validation
Location: `internal/service/subscription.go` (extend existing validation)

**Rules**:
1. **Commitment Type Validation**:
   - Must specify exactly ONE of: `CommitmentAmount` OR `CommitmentQuantity` (not both)
   - If `CommitmentAmount` set → `CommitmentType = AMOUNT`
   - If `CommitmentQuantity` set → `CommitmentType = QUANTITY`

2. **Non-window commitment** (entire billing period):
   - Allowed for ANY meter (with or without bucket_size)
   - Set `IsWindowCommitment = false`

3. **Window-based commitment** (per-window):
   - **ONLY** allowed if meter has `bucket_size` configured
   - Set `IsWindowCommitment = true`
   - Validate: `meter.HasBucketSize() == true` OR reject

4. **Commitment requirements**:
   - If commitment is set (amount or quantity), then `OverageFactor > 1.0` (existing pattern from subscription.go:2042-2046)
   - Price must be `PRICE_TYPE_USAGE`
   - Cannot set both subscription-level and line-item-level commitment simultaneously

**Validation pseudocode**:
```go
// Commitment type validation
if lineItem.CommitmentAmount != nil && lineItem.CommitmentQuantity != nil {
    return error("cannot set both commitment_amount and commitment_quantity")
}

hasCommitment := (lineItem.CommitmentAmount != nil && lineItem.CommitmentAmount.GreaterThan(decimal.Zero)) ||
                 (lineItem.CommitmentQuantity != nil && lineItem.CommitmentQuantity.GreaterThan(decimal.Zero))

if hasCommitment {
    // Overage factor required
    if lineItem.OverageFactor == nil || lineItem.OverageFactor.LessThanOrEqual(decimal.NewFromInt(1)) {
        return error("overage_factor must be > 1.0 when commitment is set")
    }
    
    // Window commitment requires bucketed meter
    if lineItem.IsWindowCommitment && !meter.HasBucketSize() {
        return error("window commitment requires meter with bucket_size")
    }
}
```

---

## 3. Billing Calculation Flow

### 3.1 Commitment Normalization
Location: `internal/service/billing.go:CalculateUsageCharges` (before line 230)

**Convert quantity-based commitment to amount** (leverages existing `CalculateCost` from price.go:1100-1104):

```go
// Normalize commitment to amount for comparison
commitmentAmountForCalculation := decimal.Zero

if item.CommitmentType == types.COMMITMENT_TYPE_AMOUNT {
    commitmentAmountForCalculation = lo.FromPtr(item.CommitmentAmount)
    
} else if item.CommitmentType == types.COMMITMENT_TYPE_QUANTITY {
    // Convert quantity to amount using price configuration
    commitmentQuantity := lo.FromPtr(item.CommitmentQuantity)
    priceObj := matchingCharge.Price // Already fetched for usage calculation
    
    // Use existing CalculateCost method (price.go:1102)
    commitmentAmountForCalculation = priceService.CalculateCost(ctx, priceObj, commitmentQuantity)
}
```

**Rationale**: 
- Reuses existing pricing logic (flat_fee, tiered, package models) from price.go:1064-1098
- Single comparison path: always compare amounts (not mixing quantity vs amount)
- Pricing complexity handled by existing `CalculateCost` infrastructure

### 3.2 Non-Window Commitment
Location: `internal/service/billing.go:CalculateUsageCharges` (extend lines 172-629)

**Logic** (similar to existing subscription-level at lines 583-626):

```
For each usage line item:
  1. Calculate total_usage_cost for billing period
  2. Normalize commitment to amount (if quantity-based)
  3. If total_usage_cost >= commitment_amount:
     - Charge: commitment_amount + (total_usage_cost - commitment_amount) * overage_factor
  4. Else:
     - Charge: commitment_amount (if enable_true_up = true)
     - Charge: total_usage_cost (if enable_true_up = false)
```

**Implementation approach**:
- After calculating `lineItemAmount` (line 508), check if line item has commitment
- Normalize commitment to amount using step 3.1
- Apply same logic as subscription-level commitment (lines 583-626) but **per line item**
- Add metadata: `"line_item_commitment": "true"`, `"commitment_type"`, `"commitment_amount"`, `"commitment_quantity"`, `"commitment_utilized"`

### 3.3 Window-Based Commitment
Location: Same as above, new branch for `IsWindowCommitment = true`

**Logic** (leverages existing bucketed meter handling at lines 421-491):

```
For each window in billing period:
  1. Fetch bucketed usage values (already exists for IsBucketedMaxMeter/IsBucketedSumMeter)
  2. For each bucket:
     a. Normalize commitment to amount for this window
        - If AMOUNT: use commitment_amount directly
        - If QUANTITY: Convert commitment_quantity to amount using price for this bucket
     b. Calculate cost_per_window using CalculateCost (single bucket value)
     c. Apply commitment:
        - If cost_per_window >= commitment_amount:
          * Window charge = commitment_amount + (cost_per_window - commitment_amount) * overage_factor
        - Else:
          * Window charge = commitment_amount (true-up per window)
  3. Total line item charge = sum(all window charges)
```

**Implementation details**:
- Reuse `usageRequest` with `WindowSize = meter.Aggregation.BucketSize` (line 465)
- Iterate `bucketedValues` (line 476-479)
- For each bucket value:
  - Normalize commitment (quantity → amount conversion per window)
  - Use `CalculateCost(ctx, price, bucketValue)` for single bucket cost (price.go:1102)
  - Apply commitment logic **per bucket** instead of aggregating first
- Create invoice metadata with window breakdown summary

**Example calculation** (from task doc):

**Quantity-based window commitment**: 100 units/minute commitment
| Window | Usage Qty | Usage Cost | Commitment Cost | Charged |
|--------|-----------|------------|-----------------|---------|
| 1 | 100 units | $10 | $10 (100*$0.1) | $10 |
| 2 | 50 units | $5 | $10 (100*$0.1) | $10 (true-up) |
| 3 | 150 units | $15 | $10 (100*$0.1) | $10 + ($15-$10)*overage |

**Amount-based window commitment**: $10/minute commitment
| Window | Usage | Usage Cost | Charged |
|--------|-------|------------|---------|
| 1 | any | $10 | $10 |
| 2 | any | $5 | $10 (true-up) |
| 3 | any | $15 | $10 + ($15-$10)*overage |

---

## 4. Precedence & Compatibility

### 4.1 Priority Order
1. **Line-item commitment** takes precedence over subscription-level commitment
2. If line item has commitment → ignore subscription commitment for that line item
3. Other line items without commitment → fall back to subscription-level commitment (if exists)

### 4.2 Backward Compatibility
- Existing subscriptions: No changes (new fields default to `nil`/`false`)
- Existing commitment logic at subscription level: Unchanged
- Add null checks before accessing line item commitment fields

---

## 5. Edge Cases

### 5.1 Entitlements + Line-Item Commitment
**Current behavior**: Entitlements reduce billable quantity (lines 267-406 in billing.go)
**New behavior**: Apply entitlement adjustments **before** commitment logic
```
1. Calculate raw usage
2. Apply entitlement deductions (existing)
3. Calculate adjusted cost
4. Apply line-item commitment (new)
```

### 5.2 Bucketed Meters without Window Commitment
- If meter has `bucket_size` but `IsWindowCommitment = false`:
  - Aggregate all buckets first (existing behavior at line 486-491)
  - Then apply commitment to **total** (non-window mode)

### 5.3 Line Item Proration
- If line item is prorated (lines 122-133 in billing.go):
  - For **non-window**: Prorate commitment amount by same factor
  - For **window**: Only charge commitment for windows within prorated period

### 5.4 Multiple Line Items with Commitment
- Each line item commitment is independent
- No aggregation across line items
- True-up calculated per line item

---

## 6. Implementation Changes Summary

### 6.1 Database Schema
- Add 6 fields to `subscription_line_items` table (see section 1.1)
- Migration: Add nullable columns with defaults

### 6.2 Code Changes

**File: `internal/types/commitment.go`** (new file)
- Add `CommitmentType` enum (AMOUNT, QUANTITY)

**File: `internal/domain/subscription/line_item.go`**
- Add commitment fields to struct (6 new fields)

**File: `internal/service/subscription.go`**
- Extend `createLineItemFromPrice` (line 3645): Add commitment field copying
- Add validation in line item creation/update (see section 2.1)

**File: `internal/service/billing.go`**
- Modify `CalculateUsageCharges` (line 172):
  - Add commitment normalization helper (converts quantity → amount using CalculateCost)
  - Add check for line item commitment after line 230
  - Branch: if `IsWindowCommitment` → window-based logic (iterate buckets, apply per-window)
  - Branch: else → non-window commitment logic (aggregate then apply, similar to lines 583-626)
  - Move existing subscription-level commitment check to **after** line-item processing

**File: `internal/api/dto/subscription.go`**
- Add commitment fields to request/response DTOs

### 6.3 Testing Requirements

**Quantity-based commitment**:
- [ ] Non-window quantity commitment: usage < commitment (true-up)
- [ ] Non-window quantity commitment: usage > commitment (overage)
- [ ] Window quantity commitment: mixed usage across windows
- [ ] Quantity commitment with tiered pricing (quantity → amount conversion)
- [ ] Quantity commitment with package pricing

**Amount-based commitment**:
- [ ] Non-window amount commitment: usage < commitment (true-up)
- [ ] Non-window amount commitment: usage > commitment (overage)
- [ ] Window amount commitment: mixed usage across windows

**Validation**:
- [ ] Cannot set both commitment_amount and commitment_quantity
- [ ] Meter without bucket_size → reject window commitment
- [ ] Overage factor validation (must be > 1.0)

**Integration**:
- [ ] Entitlements + line-item commitment interaction
- [ ] Multiple line items with different commitment configs (amount vs quantity)
- [ ] Multiple line items with different commitment modes (window vs non-window)
- [ ] Backward compatibility: existing subscriptions unchanged

---

## 7. Migration Path

1. **Phase 1**: Schema changes (nullable fields)
2. **Phase 2**: Validation logic (block invalid configs)
3. **Phase 3**: Billing calculation (enable commitment charging)
4. **Phase 4**: API exposure (allow setting via API)

No breaking changes to existing subscriptions.

---

## Key Design Decisions

1. **Unified calculation via amount**: Convert quantity-based commitments to amounts using existing `CalculateCost` (price.go:1102) - single comparison path, reuses all pricing models (flat, tiered, package)

2. **Reuse existing patterns**: Mirrors subscription-level commitment structure (lines 96-100 in subscription model, 583-626 in billing)

3. **Leverage bucketed meter infrastructure**: Window commitment uses existing `IsBucketedSumMeter()` and `IsBucketedMaxMeter()` logic (lines 421-491)

4. **Per-line-item independence**: Each line item's commitment is isolated (no cross-line-item aggregation)

5. **Validation at meter level**: Window commitment requires meter configuration, not just line item flag

6. **Entitlement-first**: Existing entitlement logic (lines 267-406) runs before commitment, maintaining current behavior for existing customers

7. **Quantity vs Amount flexibility**: Customers can choose natural unit (e.g., "100 API calls/minute" vs "$10/minute") - system normalizes to amount internally

