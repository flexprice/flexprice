# Invoicing Customer ID - Implementation & Impact Analysis

## Executive Summary

This document describes the implementation of the invoicing customer ID feature, which allows subscriptions to bill a different customer than the one using the service. The feature provides backward compatibility by falling back to the subscription's `customer_id` when no `invoicing_customer_id` is set.

**Key Design Principle:** Whoever gets billed, pays. If an invoice is created for the invoicing customer, they are responsible for payment using their own wallets and payment methods.

## Table of Contents

1. [Implementation Details](#implementation-details)
2. [Impact Analysis](#impact-analysis)
3. [Payment Behavior](#payment-behavior)
4. [Testing Scenarios](#testing-scenarios)
5. [Important Considerations](#important-considerations)
6. [Migration Notes](#migration-notes)

---

## Implementation Details

### 1. Domain Model Changes

#### Subscription Model (`internal/domain/subscription/model.go`)

Added `GetInvoicingCustomerID()` method to the `Subscription` struct:

```go
// GetInvoicingCustomerID returns the invoicing customer ID if available, 
// otherwise falls back to the subscription customer ID.
// This provides backward compatibility for subscriptions that don't have 
// an invoicing customer ID set.
func (s *Subscription) GetInvoicingCustomerID() string {
    if s.InvoicingCustomerID != nil && *s.InvoicingCustomerID != "" {
        return *s.InvoicingCustomerID
    }
    return s.CustomerID
}
```

**Rationale:** Centralizes the fallback logic in the domain model, making it reusable across all services.

### 2. Invoice Creation (`internal/service/billing.go`)

Updated `CreateInvoiceRequestForCharges()` to use invoicing customer ID:

- Invoice `CustomerID` field uses `sub.GetInvoicingCustomerID()`
- Tax preparation request `CustomerID` field uses `sub.GetInvoicingCustomerID()`

**Code Location:** `internal/service/billing.go:1672, 1664`

### 3. Invoice Recalculation (`internal/service/invoice.go`)

#### `RecalculateInvoice()`

- Fetches subscription to get current invoicing customer ID
- Updates invoice's `CustomerID` to match subscription's current invoicing customer ID
- Ensures consistency when recalculating invoice amounts

#### `RecalculateTaxesOnInvoice()`

- Uses invoice's existing `CustomerID` directly (no need to fetch subscription)
- Invoice customer ID is immutable after creation, so it's already correct

**Code Location:** `internal/service/invoice.go:2259, 2368`

### 4. Payment Processing Updates

#### Subscription Payment Processor (`internal/service/subscription_payment_processor.go`)

All payment operations now use invoicing customer ID:

- **Wallet Operations:**
  - `calculateWalletPayableAmount()` - uses `sub.GetInvoicingCustomerID()` (line 393)
  - `checkAvailableCredits()` - uses `sub.GetInvoicingCustomerID()` (line 916)

- **Stripe Operations:**
  - `processPaymentMethodCharge()` - uses invoicing customer ID for Stripe mapping (line 736)
  - `getPaymentMethodID()` - uses invoicing customer ID for payment method retrieval (line 830)

- **Payment Metadata:**
  - Uses invoicing customer ID as primary `customer_id` (line 765)
  - Includes subscription customer ID for reference

#### Wallet Payment Service (`internal/service/wallet_payment.go`)

- Uses `inv.CustomerID` directly for wallet lookups (line 83)
- Invoice customer ID is already set to invoicing customer during creation

#### Payment Service (`internal/service/payment.go`)

- Uses `invoice.CustomerID` directly for wallet lookups (line 175)
- Invoice customer ID is already set to invoicing customer during creation

---

## Impact Analysis

### What is Impacted

#### 1. Invoice Creation

- `CreateSubscriptionInvoice()` → Uses invoicing customer ID
- `PrepareSubscriptionInvoiceRequest()` → Uses invoicing customer ID
- Invoice line items → Use invoicing customer ID (inherited from invoice)

#### 2. Invoice Recalculation

- `RecalculateInvoice()` → Updates to use invoicing customer ID
- `RecalculateTaxesOnInvoice()` → Uses invoicing customer ID

#### 3. Tax Calculation

- Tax preparation uses invoicing customer ID
- Tax associations are resolved based on invoicing customer ID

#### 4. Payment Processing

- Payment processing uses invoice customer ID (which now uses invoicing customer ID)
- Wallet operations use invoice customer ID (invoicing customer)
- Payment gateway syncs use invoice customer ID
- Stripe payment methods use invoicing customer ID

#### 5. Auto-Cancellation

- Auto-cancellation looks at invoices and their customer IDs
- Since invoices use invoicing customer ID, cancellation is based on invoicing customer's payment status
- Works correctly: if invoicing customer's invoices are overdue, subscription is cancelled

### What is NOT Impacted

#### 1. Usage Tracking

- Usage calculation **still uses** `sub.CustomerID` (not invoicing customer ID)
- This is correct because:
  - Usage is tracked by the subscription customer (the entity that actually uses the service)
  - The invoicing customer ID is only for billing/invoicing purposes (who receives the invoice)
- **Locations:** `CalculateUsageCharges()` lines 261, 699

#### 2. Subscription Operations

- Subscription creation/updates unchanged
- Subscription status management unchanged
- Subscription entitlements unchanged
- **Note:** `invoicing_customer_id` cannot be updated after subscription creation (not in `UpdateSubscriptionRequest`)

#### 3. Credit Grants

- Credit grants use `subscription.CustomerID` (line 395 in `creditgrant.go`)
- This is correct: credits belong to the user (subscription customer), not the biller
- Credit grants are separate from invoicing - they're given to the subscription customer

---

## Payment Behavior

### Design Decision

**Wallets and Payment Methods belong to the INVOICING customer, not the subscription customer.**

**Rationale:**
- If an invoice is created for the invoicing customer, they are responsible for payment
- The invoicing customer should use their own wallets and payment methods
- This ensures proper financial accountability: whoever gets billed, pays
- Subscription customer is the entity using the service, but invoicing customer pays for it

### Implementation Details

#### Wallet Operations

All wallet operations use invoicing customer ID:

- `GetWalletsByCustomerID(ctx, invoicingCustomerID)`
- `GetWalletsForPayment(ctx, invoicingCustomerID, ...)`
- `calculateWalletPayableAmount(ctx, invoicingCustomerID, ...)`

**Code Locations:**
- `wallet_payment.go:83` - Uses `inv.CustomerID` (invoicing customer)
- `payment.go:175` - Uses `invoice.CustomerID` (invoicing customer)
- `subscription_payment_processor.go` - Uses `sub.GetInvoicingCustomerID()` for all wallet operations

#### Stripe Payment Methods

Stripe operations use invoicing customer ID:

- `HasCustomerStripeMapping(ctx, invoicingCustomerID, ...)`
- `GetDefaultPaymentMethod(ctx, invoicingCustomerID, ...)`

**Code Locations:**
- `subscription_payment_processor.go:736` - Stripe mapping check
- `subscription_payment_processor.go:830` - Payment method retrieval

#### Payment Metadata

Payment records include both customer IDs for audit trail:

```go
Metadata: types.Metadata{
    "customer_id":            sub.GetInvoicingCustomerID(), // Invoicing customer (who pays)
    "subscription_customer_id": sub.CustomerID,            // Subscription customer (for reference)
    "subscription_id":        sub.ID,
    "payment_source":         "subscription_auto_payment",
}
```

**Code Location:** `subscription_payment_processor.go:764-769`

---

## Testing Scenarios

### Test Case 1: Subscription WITHOUT Invoicing Customer ID (Backward Compatibility)

**Setup:**
- Create subscription with `customer_id = "cust_123"`
- Do NOT set `invoicing_customer_id`

**Expected Behavior:**
- Invoice should be created with `customer_id = "cust_123"`
- Payment should use `cust_123`'s wallets and payment methods
- All invoice operations should work as before
- No breaking changes

### Test Case 2: Subscription WITH Invoicing Customer ID

**Setup:**
- Create subscription with `customer_id = "cust_123"`
- Set `invoicing_customer_id = "cust_456"`

**Expected Behavior:**
- Invoice should be created with `customer_id = "cust_456"`
- Invoice line items should have `customer_id = "cust_456"`
- Tax calculations should use `cust_456`
- Payment should use `cust_456`'s wallets and payment methods
- Usage calculation should still use `cust_123` (subscription customer)

### Test Case 3: Invoice Recalculation

**Setup:**
- Subscription with `invoicing_customer_id = "cust_456"`
- Existing invoice with `customer_id = "cust_123"` (created before invoicing customer ID was set)

**Expected Behavior:**
- After recalculation, invoice `customer_id` should be updated to `"cust_456"`
- Line items should be updated to use `"cust_456"`
- Tax recalculation should use `"cust_456"`

**Note:** This scenario is theoretical since `invoicing_customer_id` cannot be changed after subscription creation. However, the code handles it correctly.

### Test Case 4: Usage Tracking (Should NOT Change)

**Setup:**
- Subscription with `customer_id = "cust_123"` and `invoicing_customer_id = "cust_456"`
- Track usage events with `external_customer_id` of `cust_123`

**Expected Behavior:**
- Usage should be tracked correctly using `cust_123` external ID
- Invoice should be created for `cust_456` but usage charges should reflect usage from `cust_123`

### Test Case 5: Payment Processing with Invoicing Customer ID

**Setup:**
- Subscription: `customer_id = "cust_child"`, `invoicing_customer_id = "cust_parent"`
- Invoice: `customer_id = "cust_parent"` (invoicing customer)
- Wallet: `customer_id = "cust_parent"` (invoicing customer) with balance $100
- Stripe mapping: `cust_parent` has payment method

**Expected Behavior:**
- Payment should use `cust_parent`'s wallet (invoicing customer)
- Payment should use `cust_parent`'s Stripe payment method (invoicing customer)
- Invoice should be paid
- Payment metadata should use invoicing customer ID

### Test Case 6: Auto-Cancellation with Invoicing Customer ID

**Setup:**
- Subscription: `customer_id = "cust_child"`, `invoicing_customer_id = "cust_parent"`
- Invoice: `customer_id = "cust_parent"`, overdue by 10 days

**Expected Behavior:**
- Auto-cancellation should find the invoice
- Should cancel the subscription
- Works correctly ✅

---

## Important Considerations

### ⚠️ Critical Requirements

1. **Invoicing Customer Must Have Wallets**
   - If invoicing customer has no wallets, wallet payments will fail
   - Ensure invoicing customer has wallets set up before creating invoices

2. **Invoicing Customer Must Have Payment Methods**
   - If invoicing customer has no Stripe payment methods, card payments will fail
   - Ensure invoicing customer has payment methods set up before creating invoices

3. **Invoicing Customer ID is Immutable**
   - `invoicing_customer_id` can only be set during subscription creation
   - It cannot be updated via `UpdateSubscriptionRequest`
   - This ensures consistency: invoice customer ID matches subscription invoicing customer ID

4. **Backward Compatibility**
   - Subscriptions without `invoicing_customer_id` continue to work
   - Falls back to subscription customer ID for all operations
   - No breaking changes for existing subscriptions

### Design Rationale

- **Whoever gets billed, pays** - Invoicing customer receives invoice and pays with their own resources
- **Financial accountability** - Clear separation: subscription customer uses service, invoicing customer pays
- **Business logic** - Parent company bills child company, parent company pays with their own payment methods/wallets
- **Usage tracking** - Usage remains tied to subscription customer (the entity using the service)

---

## Migration Notes

- **No migration required** - existing subscriptions without `invoicing_customer_id` will continue to work
- **Backward compatible** - all existing functionality preserved
- **Opt-in feature** - only subscriptions with `invoicing_customer_id` set will use it
- **No database migration needed** - field already exists in schema

---

## Files Modified

1. **`internal/domain/subscription/model.go`**
   - Added `GetInvoicingCustomerID()` method

2. **`internal/service/billing.go`**
   - Updated `CreateInvoiceRequestForCharges()` to use `sub.GetInvoicingCustomerID()`

3. **`internal/service/invoice.go`**
   - Updated `RecalculateInvoice()` to use invoicing customer ID
   - Updated `RecalculateTaxesOnInvoice()` to use invoice's existing customer ID

4. **`internal/service/subscription_payment_processor.go`**
   - Updated all wallet operations to use `sub.GetInvoicingCustomerID()`
   - Updated Stripe operations to use invoicing customer ID
   - Updated payment metadata to include both customer IDs

5. **`internal/service/wallet_payment.go`**
   - Uses `inv.CustomerID` directly (already set to invoicing customer)

6. **`internal/service/payment.go`**
   - Uses `invoice.CustomerID` directly (already set to invoicing customer)

---

## Conclusion

**Overall Impact: MODERATE** ⚠️

The invoicing customer ID feature has been successfully implemented with full backward compatibility:

### ✅ Key Achievements

1. **Payment Operations** - Now use invoicing customer's resources
   - Wallets belong to invoicing customer
   - Payment methods belong to invoicing customer
   - Payment metadata includes both customer IDs

2. **Invoice Operations** - Correctly use invoicing customer ID
   - Invoice creation uses invoicing customer ID
   - Invoice recalculation maintains consistency
   - Tax calculations use invoicing customer ID

3. **Auto-Cancellation** - Works correctly with invoicing customer ID

4. **Backward Compatibility** - Fully maintained
   - Subscriptions without invoicing customer ID work as before
   - No breaking changes

### ⚠️ Important Notes

- **Wallets must belong to invoicing customer** - If invoicing customer has no wallets, payment will fail
- **Payment methods must belong to invoicing customer** - If invoicing customer has no payment methods, card payment will fail
- **Usage tracking unchanged** - Still uses subscription customer ID (correct behavior)
- **Invoicing customer ID is immutable** - Set only during subscription creation

### Design Principles

- **Separation of concerns** - Subscription customer uses service, invoicing customer pays
- **Financial accountability** - Clear ownership of payment resources
- **Backward compatibility** - Existing subscriptions continue to work without changes

