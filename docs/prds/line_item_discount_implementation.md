# Line Item Level Discount Implementation

## Overview
Successfully implemented line item level discount functionality that allows applying coupons to specific invoice line items in addition to the existing invoice-level coupons.

## Key Changes Made

### 1. Enhanced DTO Models (`internal/api/dto/invoice.go`)
- **Added `InvoiceLineItemCoupon` struct**: New type for coupons that apply to specific line items
- **Enhanced `CreateInvoiceRequest`**: Added `LineItemCoupons` field to support line item coupons
- **Added validation methods**: Full validation for line item coupons including coupon type validation

### 2. Extended Service Layer

#### Coupon Application Service (`internal/service/coupon_application.go`)
- **New method `ApplyCouponsOnInvoiceWithLineItems`**: Handles both invoice-level and line item-level coupons
- **Sophisticated discount calculation logic**: 
  - Applies line item coupons first to individual line items
  - Then applies invoice-level coupons to the remaining total
  - Prevents negative pricing through proper validation

#### Invoice Service (`internal/service/invoice.go`)
- **Enhanced `applyCouponsToInvoiceWithLineItems`**: New method that utilizes the enhanced coupon application service
- **Backward compatibility**: Existing `applyCouponsToInvoice` method remains unchanged

#### Billing Service (`internal/service/billing.go`)
- **Automatic line item coupon collection**: New `collectLineItemCoupons` method automatically finds and includes line item coupons during subscription invoice generation
- **Smart coupon association mapping**: Maps subscription line item coupons to invoice line items based on price_id

#### Coupon Association Service (`internal/service/coupon_association.go`)
- **New method `GetCouponAssociationsBySubscriptionLineItem`**: Retrieves coupons associated with specific subscription line items

### 3. Enhanced Repository Interface
- **Extended `CouponAssociation` repository interface**: Added `GetBySubscriptionLineItem` method to support line item coupon queries

## Architecture & Design

### Discount Application Flow
1. **Line Item Coupons Applied First**: Ensures line item discounts are calculated on original amounts
2. **Invoice Coupons Applied Second**: Applied to the total after line item discounts
3. **Atomic Transaction**: All coupon applications happen in a single database transaction
4. **Comprehensive Logging**: Detailed logging for debugging and audit trails

### Database Schema
The existing schema already supported line item discounts:
- `coupon_application` table has `invoice_line_item_id` field
- `coupon_association` table has `subscription_line_item_id` field  
- `invoice_line_item` entity has edges to `coupon_applications`

### Validation
- **Request Level**: Validates coupon types, amounts, and required fields
- **Business Logic Level**: Validates coupon eligibility and prevents over-discounting
- **Database Level**: Ensures data consistency through proper foreign key relationships

## Usage Example

```json
{
  "customer_id": "cust_123",
  "currency": "usd", 
  "amount_due": 100.00,
  "line_items": [
    {
      "price_id": "price_123",
      "amount": 50.00,
      "quantity": 1
    },
    {
      "price_id": "price_456", 
      "amount": 50.00,
      "quantity": 1
    }
  ],
  "invoice_coupons": [
    {
      "coupon_id": "coupon_invoice_10_percent",
      "type": "percentage",
      "percentage_off": 10
    }
  ],
  "line_item_coupons": [
    {
      "line_item_id": "price_123",
      "coupon_id": "coupon_line_item_5_off", 
      "type": "fixed",
      "amount_off": 5.00
    }
  ]
}
```

In this example:
1. Line item with `price_123` gets $5 off (original $50 → $45)
2. Invoice total becomes $95 ($45 + $50)  
3. Invoice-level 10% coupon applied to $95 → $9.50 off
4. Final total: $85.50

## Backwards Compatibility
- All existing invoice-level coupon functionality remains unchanged
- Existing API endpoints continue to work as before
- New line item coupon functionality is opt-in via the new `line_item_coupons` field

## Subscription Integration
- **Automatic Detection**: When creating subscription invoices, the system automatically detects line item level coupon associations
- **Seamless Application**: Line item coupons are automatically included without manual intervention
- **Proper Mapping**: Uses price_id to map subscription line item coupons to invoice line items

## Benefits
1. **Granular Control**: Merchants can apply different discounts to different products/services
2. **Complex Pricing**: Supports sophisticated pricing strategies with mixed discount types
3. **Audit Trail**: Complete tracking of which coupons were applied to which line items
4. **Performance**: Optimized batch processing for multiple coupon applications
5. **Extensibility**: Foundation for future enhancements like bundle discounts, tiered pricing, etc.

## Implementation Notes
- **Price ID Mapping**: Uses price_id as the bridge between subscription line items and invoice line items
- **Transaction Safety**: All coupon applications happen atomically
- **Error Handling**: Graceful handling of invalid coupons with detailed logging
- **Memory Efficiency**: Processes coupons in batches to minimize memory usage