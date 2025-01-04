### Product Requirements Document: Invoice Line items and period

# Invoice Line Items and Period Tracking

## Overview
This PRD outlines the enhancement of the invoice system to include detailed line items and period tracking capabilities. This addition will provide better granularity and temporal context for invoices, enabling more detailed billing information and better tracking of subscription periods.

## Problem Statement
Currently, our invoice system lacks:
1. Ability to track the time period for which an invoice is generated
2. Detailed breakdown of individual line items within an invoice
3. Association of line items with specific subscriptions, prices, and meters

## Requirements

### 1. Invoice Period Tracking
- Add optional `period_start` field to track when the billing period begins
- Add optional `period_end` field to track when the billing period ends
- These fields will help in organizing and querying invoices based on time periods

### 2. Invoice Line Items
Create a new entity `InvoiceLineItems` with the following attributes:
- `id`: Unique identifier for the line item
- `invoice_id`: Reference to the parent invoice
- `customer_id`: Reference to the customer
- `subscription_id`: Reference to the subscription (if applicable)
- `price_id`: Reference to the price configuration
- `meter_id`: Reference to the meter (if usage-based)
- `amount`: The monetary amount for this line item
- `quantity`: The quantity of items/units
- `currency`: The currency code (must match invoice currency)
- `period_start`: Start of the period for this specific line item
- `period_end`: End of the period for this specific line item
- `metadata`: JSON field for additional contextual data

### 3. Data Model Changes
- Modify existing invoice schema to include period fields
- Create new schema for invoice line items
- Establish proper foreign key relationships and constraints

### 4. API Changes
- Update CreateInvoiceRequest to accept period information
- Update CreateInvoice and CreateSubscriptionInvoice methods to handle line items
- Add proper validation for:
  - Period dates (end must be after start)
  - Currency matching between invoice and line items
  - Required fields based on invoice type
  - Amount and quantity validations

## Technical Considerations
- Use proper indexing for period_start and period_end for efficient querying
- Implement proper transaction handling for invoice and line items creation
- Ensure backward compatibility for existing invoice creation flows
- Add proper logging for debugging and tracking

## Success Metrics
- Successful creation and retrieval of invoices with line items
- Proper period tracking for subscription-based invoices
- Accurate aggregation of line item amounts matching total invoice amount
- Improved debugging capability with detailed line item information
