# Commitment-Based Pricing with Overage Factor

## Overview
This document outlines the implementation of commitment-based pricing with overage factor for subscriptions. This pricing model allows customers to commit to a minimum spend (commitment amount) for a billing period, with any usage beyond this threshold charged at a higher rate (determined by the overage factor).

## Terminology
- **Commitment Amount**: The minimum amount a customer commits to paying for a billing period (e.g., $1000/month)
- **Overage Factor**: A multiplier applied to usage beyond the commitment amount (e.g., 1.5x)
- **Normal Usage**: Usage that falls within the commitment amount
- **Overage Usage**: Usage that exceeds the commitment amount and is subject to the overage factor

## Requirements

### 1. Data Model Changes
- Add `commitment_amount` (decimal) field to the Subscription schema
- Add `overage_factor` (decimal) field to the Subscription schema
- Default `overage_factor` to 1.0 (no overage)

### 2. Subscription Creation
- Update the `CreateSubscription` endpoint to accept the new fields:
  - `commitment_amount` (optional)
  - `overage_factor` (optional, defaults to 1.0)
- Validate that `overage_factor` ≥ 1.0
- Store these values in the subscription record

### 3. Usage Calculation Logic
The key challenge is calculating and displaying usage correctly, particularly in the breakdown of normal vs. overage usage at the feature level.

#### 3.1 Calculating Total Usage
1. Calculate the total usage cost at normal rates for all features
2. If total usage cost ≤ commitment amount:
   - All usage is charged at normal rates
3. If total usage cost > commitment amount:
   - Calculate the overage portion (total usage cost - commitment amount)
   - Apply the overage factor to the overage portion
   - Final cost = commitment amount + (overage portion × overage factor)

#### 3.2 Feature-Level Breakdown
When displaying feature-level breakdown in invoices:

1. Sort features by timestamp of usage (first-used first) or by a predefined priority
2. Allocate the commitment amount to features in order until exhausted
3. For each feature:
   - Track how much of its usage falls under normal pricing
   - Track how much falls under overage pricing
4. Present each feature's usage as potentially two line items:
   - One for usage at normal price
   - One for usage at overage price (if applicable)

### 4. API Changes

#### 4.1 Subscription Service
- Update `CreateSubscription` method to handle new fields
- Update `GetUsageBySubscription` to calculate and return both normal and overage usage

#### 4.2 Billing Service
- Modify invoice calculation to incorporate commitment and overage logic
- Update preview invoices to show the breakdown of normal vs. overage pricing

### 5. Frontend Display
- Enhance invoice display to show commitment amount and usage breakdown
- For each feature, display:
  - Normal usage quantity and cost
  - Overage usage quantity and cost (if applicable)
- Show summary with total normal usage, total overage usage, and final total

## Implementation Strategy

### Phase 1: Data Model and Basic Subscription Creation
1. Update the Subscription schema
2. Update the CreateSubscription endpoint
3. Update unit tests

### Phase 2: Usage Calculation
1. Implement the overage calculation algorithm
2. Update the GetUsageBySubscription endpoint
3. Add unit tests for boundary cases

### Phase 3: Invoice Generation
1. Update invoice generation logic
2. Update preview invoice endpoint
3. Add comprehensive tests

## Algorithm for Feature-Level Breakdown

```
function calculateFeatureBreakdown(features, commitmentAmount, overageFactor):
    # Sort features by usage time or priority
    sortedFeatures = sortFeatures(features)
    
    remainingCommitment = commitmentAmount
    result = []
    
    for feature in sortedFeatures:
        featureNormalCost = min(feature.totalCost, remainingCommitment)
        featureOverageCost = 0
        
        if feature.totalCost > remainingCommitment:
            # Calculate how much of this feature's usage is at normal price
            normalPriceRatio = remainingCommitment / feature.totalCost
            normalQuantity = feature.quantity * normalPriceRatio
            
            # Calculate how much is at overage price
            overageQuantity = feature.quantity - normalQuantity
            overageCost = (feature.totalCost - remainingCommitment) * overageFactor
            
            result.push({
                featureId: feature.id,
                normalPriceQuantity: normalQuantity,
                normalPriceTotal: remainingCommitment,
                overagePriceQuantity: overageQuantity,
                overagePriceTotal: overageCost
            })
            
            remainingCommitment = 0
        else:
            # All of this feature's usage is at normal price
            result.push({
                featureId: feature.id,
                normalPriceQuantity: feature.quantity,
                normalPriceTotal: feature.totalCost,
                overagePriceQuantity: 0,
                overagePriceTotal: 0
            })
            
            remainingCommitment -= feature.totalCost
    
    return result
```

## Edge Cases and Considerations

1. **Proration**: How commitment amounts are prorated for partial billing periods
2. **Subscription Changes**: How changes to commitment amount mid-period are handled
3. **Refunds/Credits**: How refunds and credits interact with commitment and overage
4. **Usage Ordering**: The order in which usage counts against the commitment can affect the final cost
5. **Multiple Currencies**: Ensure proper handling of currencies in calculations

## Example

### Input
- Commitment Amount: $1000
- Overage Factor: 1.5
- Features:
  - F1: $1/unit, 5000 units = $5000
  - F2: $2/unit, 2500 units = $5000

### Output
- F1: normal price $1/unit, 1000 quantity, $1000 total (uses up all commitment)
- F1: overage price $1.5/unit, 4000 quantity, $6000 total
- F2: overage price $3/unit, 2500 quantity, $7500 total
- Total: $14,500 ($1000 at normal rate + $13,500 at overage rate) 