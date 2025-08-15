# Price Unit Implementation Guide

## Overview

The Price Unit feature allows you to create prices using custom price units (like BTC, GBP, etc.) while automatically converting them to the base currency for storage and billing calculations. This guide covers the complete workflow from price unit CRUD operations to price creation and line item processing.

## Table of Contents

1. [Price Unit CRUD Operations](#price-unit-crud-operations)
2. [Price Creation with Price Units](#price-creation-with-price-units)
3. [Billing Models Support](#billing-models-support)
4. [Line Items Processing](#line-items-processing)
5. [API Endpoints](#api-endpoints)
6. [Error Handling](#error-handling)

## Price Unit CRUD Operations

### 1. Create Price Unit

**Endpoint:** `POST /prices/units`

**Request:**
```bash
curl -X POST http://localhost:8080/api/v1/prices/units \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "name": "Bitcoin",
    "code": "btc",
    "symbol": "₿",
    "precision": 8,
    "conversion_rate": "50000.00",
    "status": "published"
  }'
```

**Response:**
```json
{
  "id": "price_unit_01K0WASRE5JPA64XSFGN0QMM70",
  "name": "Bitcoin",
  "code": "btc",
  "symbol": "₿",
  "precision": 8,
  "conversion_rate": "50000.00",
  "status": "published",
  "created_at": "2025-07-29T06:00:00.000Z",
  "updated_at": "2025-07-29T06:00:00.000Z"
}
```

### 2. Get Price Unit by ID

**Endpoint:** `GET /prices/units/{id}`

```bash
curl -X GET http://localhost:8080/api/v1/prices/units/price_unit_01K0WASRE5JPA64XSFGN0QMM70 \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### 3. Get Price Unit by Code

**Endpoint:** `GET /prices/units/code/{code}`

```bash
curl -X GET http://localhost:8080/api/v1/prices/units/code/btc \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### 4. List Price Units

**Endpoint:** `GET /prices/units`

```bash
curl -X GET "http://localhost:8080/api/v1/prices/units?limit=10&offset=0" \
  -H "Authorization: Bearer YOUR_API_KEY"
```

### 5. Update Price Unit

**Endpoint:** `PUT /prices/units/{id}`

```bash
curl -X PUT http://localhost:8080/api/v1/prices/units/price_unit_01K0WASRE5JPA64XSFGN0QMM70 \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "conversion_rate": "52000.00"
  }'
```

### 6. Delete Price Unit

**Endpoint:** `DELETE /prices/units/{id}`

```bash
curl -X DELETE http://localhost:8080/api/v1/prices/units/price_unit_01K0WASRE5JPA64XSFGN0QMM70 \
  -H "Authorization: Bearer YOUR_API_KEY"
```

## Price Creation with Price Units

### Endpoint
**POST** `/prices/config` - Create price with price unit configuration

### Request Structure

```json
{
  "amount": "0",  // Can be empty when using price_unit_config
  "currency": "usd",
  "billing_model": "FLAT_FEE|PACKAGE|TIERED",
  "billing_period": "MONTHLY",
  "billing_period_count": 1,
  "billing_cadence": "RECURRING",
  "invoice_cadence": "ARREAR",
  "type": "FIXED|USAGE",
  "plan_id": "plan_01K196D5RKHJG6P9GVT4XFGAZ4",
  "meter_id": "meter_01K0JWYB1VERXK4PKGREF8XP7T",  // Required for USAGE type
  "price_unit_config": {
    "amount": "2.00",
    "price_unit": "gbp",
    "tiers": [  // Optional, for TIERED billing
      {
        "up_to": 1000,
        "unit_amount": "0.001",
        "flat_amount": "0.01"
      },
      {
        "unit_amount": "0.002"
      }
    ]
  }
}
```

## Billing Models Support

### 1. FLAT_FEE with Price Unit

**Use Case:** Fixed monthly fee in custom currency

**Request:**
```bash
curl -X POST http://localhost:8080/api/v1/prices/config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "billing_model": "FLAT_FEE",
    "billing_period": "MONTHLY",
    "billing_period_count": 1,
    "billing_cadence": "RECURRING",
    "invoice_cadence": "ARREAR",
    "currency": "usd",
    "type": "FIXED",
    "plan_id": "plan_01K196D5RKHJG6P9GVT4XFGAZ4",
    "price_unit_config": {
      "amount": "10.00",
      "price_unit": "gbp"
    }
  }'
```

**Response:**
```json
{
  "id": "price_01K1ACEE615RNKBHSB6YT8WCPQ",
  "amount": "12.70",
  "display_amount": "$12.70",
  "currency": "usd",
  "price_unit_id": "price_unit_01K0WASRE5JPA64XSFGN0QMM70",
  "price_unit_amount": "10",
  "display_price_unit_amount": "£10.00",
  "price_unit": "gbp",
  "conversion_rate": "1.27",
  "billing_model": "FLAT_FEE"
}
```

**Calculation:** `10.00 GBP × 1.27 = 12.70 USD`

### 2. PACKAGE with Price Unit

**Use Case:** Buy X units for Y amount in custom currency

**Request:**
```bash
curl -X POST http://localhost:8080/api/v1/prices/config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "billing_model": "PACKAGE",
    "billing_period": "MONTHLY",
    "billing_period_count": 1,
    "billing_cadence": "RECURRING",
    "invoice_cadence": "ARREAR",
    "currency": "usd",
    "type": "USAGE",
    "meter_id": "meter_01K0JWYB1VERXK4PKGREF8XP7T",
    "plan_id": "plan_01K196D5RKHJG6P9GVT4XFGAZ4",
    "transform_quantity": {
      "divide_by": 100,
      "round": "up"
    },
    "price_unit_config": {
      "amount": "50.00",
      "price_unit": "gbp"
    }
  }'
```

**Response:**
```json
{
  "id": "price_01K1ACEE615RNKBHSB6YT8WCPQ",
  "amount": "63.50",
  "display_amount": "$63.50",
  "currency": "usd",
  "price_unit_id": "price_unit_01K0WASRE5JPA64XSFGN0QMM70",
  "price_unit_amount": "50",
  "display_price_unit_amount": "£50.00",
  "price_unit": "gbp",
  "conversion_rate": "1.27",
  "billing_model": "PACKAGE",
  "transform_quantity": {
    "divide_by": 100,
    "round": "up"
  }
}
```

**Calculation:** `50.00 GBP × 1.27 = 63.50 USD` for every 100 units

### 3. TIERED with Price Unit

**Use Case:** Volume-based pricing with custom currency

**Request:**
```bash
curl -X POST http://localhost:8080/api/v1/prices/config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -d '{
    "billing_model": "TIERED",
    "tier_mode": "VOLUME",
    "billing_period": "MONTHLY",
    "billing_period_count": 1,
    "billing_cadence": "RECURRING",
    "invoice_cadence": "ARREAR",
    "currency": "usd",
    "type": "USAGE",
    "meter_id": "meter_01K0JWYB1VERXK4PKGREF8XP7T",
    "plan_id": "plan_01K196D5RKHJG6P9GVT4XFGAZ4",
    "price_unit_config": {
      "price_unit": "gbp",
      "tiers": [
        {
          "up_to": 1000,
          "unit_amount": "0.001",
          "flat_amount": "0.01"
        },
        {
          "unit_amount": "0.002"
        }
      ]
    }
    // Note: amount is not required when using TIERED billing model
  }'
```

**Response:**
```json
{
  "id": "price_01K1ACXTGFV95DVJNJ5CV96062",
  "amount": "19.05",
  "display_amount": "$19.05",
  "currency": "usd",
  "price_unit_id": "price_unit_01K0WASRE5JPA64XSFGN0QMM70",
  "price_unit_amount": "15",
  "display_price_unit_amount": "£15.00",
  "price_unit": "gbp",
  "conversion_rate": "1.27",
  "billing_model": "TIERED",
  "tier_mode": "VOLUME",
  "tiers": [
    {
      "up_to": 1000,
      "unit_amount": "0.00127",
      "flat_amount": "0.0127"
    },
    {
      "up_to": null,
      "unit_amount": "0.00254"
    }
  ]
}
```

**Tier Conversions:**
- Tier 1: `0.001 GBP × 1.27 = 0.00127 USD`
- Tier 1 Flat: `0.01 GBP × 1.27 = 0.0127 USD`
- Tier 2: `0.002 GBP × 1.27 = 0.00254 USD`

## Line Items Processing

### Subscription Line Items

When a price with price unit is used in a subscription, the line items automatically include price unit information:

**Subscription Line Item Response:**
```json
{
  "id": "subs_line_01K0YGFB9QY321P0GKSF52P3MG",
  "subscription_id": "subs_01K0YGFB9QY321P0GKSDCV6T9J",
  "price_id": "price_01K1ACEE615RNKBHSB6YT8WCPQ",
  "price_unit_id": "price_unit_01K0WASRE5JPA64XSFGN0QMM70",
  "price_unit": "gbp",
  "amount": "12.70",
  "currency": "usd",
  "billing_model": "FLAT_FEE"
}
```

### Invoice Line Items

During billing calculation, invoice line items include both base currency and price unit amounts:

**Invoice Line Item Response:**
```json
{
  "id": "inv_line_01K1ACEE615RNKBHSB6YT8WCPQ",
  "invoice_id": "inv_01K1ACEE615RNKBHSB6YT8WCPQ",
  "price_id": "price_01K1ACEE615RNKBHSB6YT8WCPQ",
  "price_unit_id": "price_unit_01K0WASRE5JPA64XSFGN0QMM70",
  "price_unit": "gbp",
  "price_unit_amount": "10.00",
  "amount": "12.70",
  "currency": "usd"
}
```

## API Endpoints

### Price Unit Management
- `POST /prices/units` - Create price unit
- `GET /prices/units/{id}` - Get price unit by ID
- `GET /prices/units/code/{code}` - Get price unit by code
- `GET /prices/units` - List price units
- `PUT /prices/units/{id}` - Update price unit
- `DELETE /prices/units/{id}` - Delete price unit

### Price Creation
- `POST /prices` - Create regular price
- `POST /prices/config` - Create price with price unit config

### Billing
- `POST /subscriptions` - Create subscription (supports price units)
- `POST /invoices` - Create invoice (supports price units)

## Error Handling

### Common Validation Errors

**1. Invalid Price Unit:**
```json
{
  "success": false,
  "error": {
    "message": "invalid or unpublished price unit",
    "internal_error": "Price unit must exist and be published"
  }
}
```

**2. Missing Required Fields:**
```json
{
  "success": false,
  "error": {
    "message": "price_unit is required when price_unit_config is provided",
    "internal_error": "Please provide a valid price unit"
  }
}
```

**3. Invalid Amount Format:**
```json
{
  "success": false,
  "error": {
    "message": "invalid price unit amount format",
    "internal_error": "Price unit amount must be a valid decimal number"
  }
}
```

**4. Conflicting Tiers:**
```json
{
  "success": false,
  "error": {
    "message": "cannot provide both regular tiers and price unit tiers",
    "internal_error": "Use either regular tiers or price unit tiers, not both"
  }
}
```

## Implementation Details

### Conversion Process

1. **Fetch Price Unit**: Get price unit by code, tenant, and environment
2. **Validate**: Ensure price unit exists and is published
3. **Convert Amount**: Apply conversion rate to price unit amount
4. **Convert Tiers**: If present, convert all tier amounts to base currency
5. **Set Fields**: Populate price unit fields in the price object

### Database Storage

The price is stored with both:
- **Base Currency Amount**: For billing calculations
- **Price Unit Amount**: For display and reference
- **Conversion Rate**: For future conversions
- **Price Unit ID**: For relationship tracking

### Validation Rules

1. **Price Unit Format**: Must be exactly 3 characters
2. **Amount Validation**: Must be positive decimal number
3. **Tier Validation**: All tier amounts must be valid decimals
4. **Conflicting Configs**: Cannot use both regular and price unit tiers
5. **Required Fields**: 
   - Price unit is always required when config is provided
   - Amount is required when config is provided, except for TIERED billing model

## Benefits

1. **Unified Workflow**: No separate API endpoints needed
2. **Automatic Conversion**: Handles currency conversion seamlessly
3. **Backward Compatible**: Existing price creation still works
4. **Flexible**: Supports all billing models (FLAT_FEE, PACKAGE, TIERED)
5. **Consistent**: Uses existing price unit repository and validation
6. **Complete Integration**: Works with subscriptions, invoices, and line items

This ensures accurate billing while preserving the original price unit context and providing a seamless experience for multi-currency pricing scenarios. 