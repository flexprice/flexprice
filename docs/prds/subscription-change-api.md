# Subscription Plan Change API

This document describes the API endpoints for changing subscription plans (upgrades, downgrades, and lateral changes) with proper proration handling.

## Overview

The subscription change API allows customers to modify their subscription plans while handling proration calculations, billing adjustments, and invoice generation automatically.

## API Endpoints

### Preview Subscription Change

Preview the impact of changing a subscription's plan before executing the change.

```
POST /v1/subscriptions/{id}/change/preview
```

**Path Parameters:**
- `id` (string, required): The subscription ID

**Request Body:**
```json
{
  "target_plan_id": "plan_abc123",
  "proration_behavior": "create_prorations",
  "effective_date": "2024-01-15T10:00:00Z",
  "billing_cycle_anchor": "unchanged",
  "trial_end": "2024-02-15T10:00:00Z",
  "cancel_at_period_end": false,
  "invoice_now": true,
  "metadata": {
    "change_reason": "customer_request"
  },
  "preview_date": "2024-01-15T10:00:00Z"
}
```

**Response:**
```json
{
  "subscription_id": "subs_xyz789",
  "current_plan": {
    "id": "plan_basic",
    "name": "Basic Plan",
    "lookup_key": "basic",
    "description": "Basic subscription plan"
  },
  "target_plan": {
    "id": "plan_abc123",
    "name": "Premium Plan",
    "lookup_key": "premium",
    "description": "Premium subscription plan"
  },
  "change_type": "upgrade",
  "proration_details": {
    "credit_amount": "5.00",
    "credit_description": "Credit for unused time on current plan",
    "charge_amount": "15.00",
    "charge_description": "Charge for new plan from 2024-01-15",
    "net_amount": "10.00",
    "proration_date": "2024-01-15T10:00:00Z",
    "current_period_start": "2024-01-01T00:00:00Z",
    "current_period_end": "2024-02-01T00:00:00Z",
    "days_used": 14,
    "days_remaining": 17,
    "currency": "usd"
  },
  "immediate_invoice_preview": {
    "subtotal": "10.00",
    "tax_amount": "0.00",
    "total": "10.00",
    "currency": "usd",
    "line_items": [
      {
        "description": "Credit for unused time on current plan",
        "amount": "-5.00",
        "quantity": "1",
        "unit_price": "-5.00",
        "period_start": "2024-01-01T00:00:00Z",
        "period_end": "2024-01-15T10:00:00Z",
        "is_proration": true
      },
      {
        "description": "Charge for new plan from 2024-01-15",
        "amount": "15.00",
        "quantity": "1",
        "unit_price": "15.00",
        "period_start": "2024-01-15T10:00:00Z",
        "period_end": "2024-02-01T00:00:00Z",
        "is_proration": true
      }
    ],
    "due_date": "2024-01-15T10:00:00Z"
  },
  "next_invoice_preview": {
    "subtotal": "20.00",
    "tax_amount": "0.00",
    "total": "20.00",
    "currency": "usd",
    "line_items": [
      {
        "description": "Premium Plan - Monthly subscription",
        "amount": "20.00",
        "quantity": "1",
        "unit_price": "20.00",
        "is_proration": false
      }
    ]
  },
  "effective_date": "2024-01-15T10:00:00Z",
  "new_billing_cycle": {
    "period_start": "2024-01-15T10:00:00Z",
    "period_end": "2024-02-15T10:00:00Z",
    "billing_anchor": "2024-01-01T00:00:00Z",
    "billing_cadence": "RECURRING",
    "billing_period": "MONTHLY",
    "billing_period_count": 1
  },
  "warnings": [
    "Proration charges or credits will be applied to your next invoice."
  ],
  "metadata": {
    "change_reason": "customer_request"
  }
}
```

### Execute Subscription Change

Execute the actual subscription plan change.

```
POST /v1/subscriptions/{id}/change/execute
```

**Path Parameters:**
- `id` (string, required): The subscription ID

**Request Body:**
```json
{
  "target_plan_id": "plan_abc123",
  "proration_behavior": "create_prorations",
  "effective_date": "2024-01-15T10:00:00Z",
  "billing_cycle_anchor": "unchanged",
  "trial_end": "2024-02-15T10:00:00Z",
  "cancel_at_period_end": false,
  "invoice_now": true,
  "metadata": {
    "change_reason": "customer_request"
  }
}
```

**Response:**
```json
{
  "old_subscription": {
    "id": "subs_xyz789",
    "status": "cancelled",
    "plan_id": "plan_basic",
    "current_period_start": "2024-01-01T00:00:00Z",
    "current_period_end": "2024-02-01T00:00:00Z",
    "billing_anchor": "2024-01-01T00:00:00Z",
    "created_at": "2024-01-01T00:00:00Z",
    "archived_at": "2024-01-15T10:00:00Z"
  },
  "new_subscription": {
    "id": "subs_new456",
    "status": "active",
    "plan_id": "plan_abc123",
    "current_period_start": "2024-01-15T10:00:00Z",
    "current_period_end": "2024-02-15T10:00:00Z",
    "billing_anchor": "2024-01-01T00:00:00Z",
    "created_at": "2024-01-15T10:00:00Z"
  },
  "change_type": "upgrade",
  "invoice": {
    "id": "inv_change123",
    "amount_due": "10.00",
    "currency": "usd",
    "status": "finalized",
    "created_at": "2024-01-15T10:00:00Z"
  },
  "proration_applied": {
    "credit_amount": "5.00",
    "credit_description": "Credit for unused time on current plan",
    "charge_amount": "15.00",
    "charge_description": "Charge for new plan from 2024-01-15",
    "net_amount": "10.00",
    "proration_date": "2024-01-15T10:00:00Z",
    "current_period_start": "2024-01-01T00:00:00Z",
    "current_period_end": "2024-02-01T00:00:00Z",
    "days_used": 14,
    "days_remaining": 17,
    "currency": "usd"
  },
  "effective_date": "2024-01-15T10:00:00Z",
  "metadata": {
    "change_reason": "customer_request"
  }
}
```

## Request Parameters

### SubscriptionChangeRequest Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `target_plan_id` | string | Yes | ID of the new plan to change to |
| `proration_behavior` | string | Yes | How to handle proration: `create_prorations`, `always_invoice`, `none` |
| `effective_date` | timestamp | No | When the change should take effect (defaults to now) |
| `billing_cycle_anchor` | string | No | How to handle billing cycle: `unchanged`, `reset`, `immediate` |
| `trial_end` | timestamp | No | New trial end date |
| `cancel_at_period_end` | boolean | No | Schedule cancellation at period end |
| `invoice_now` | boolean | No | Generate invoice immediately (defaults to true) |
| `metadata` | object | No | Additional key-value pairs |

### Proration Behavior Options

- `create_prorations`: Calculate and apply prorations (default)
- `always_invoice`: Always create invoice items regardless of proration
- `none`: Calculate but don't apply proration (useful for previews)

### Billing Cycle Anchor Options

- `unchanged`: Keep current billing anchor (default)
- `reset`: Reset billing anchor to effective date
- `immediate`: Bill immediately and reset anchor

### Change Types

The system automatically determines the change type:

- `upgrade`: Target plan has higher value than current plan
- `downgrade`: Target plan has lower value than current plan
- `lateral`: Target plan has same value as current plan

## Error Responses

### Validation Errors (400)

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid request parameters",
    "details": {
      "target_plan_id": "Plan ID is required"
    }
  }
}
```

### Subscription Not Found (404)

```json
{
  "error": {
    "code": "NOT_FOUND",
    "message": "Subscription not found",
    "details": {
      "subscription_id": "subs_xyz789"
    }
  }
}
```

### Invalid Subscription State (400)

```json
{
  "error": {
    "code": "INVALID_OPERATION",
    "message": "Cannot change cancelled subscription",
    "details": {
      "current_status": "cancelled"
    }
  }
}
```

## Implementation Details

### Workflow

1. **Validation**: Verify subscription exists and is in valid state for changes
2. **Plan Comparison**: Determine change type (upgrade/downgrade/lateral)
3. **Proration Calculation**: Calculate credits and charges based on usage
4. **Archive Old Subscription**: Mark current subscription as cancelled
5. **Create New Subscription**: Generate new subscription with target plan
6. **Invoice Generation**: Create invoice for immediate charges (if applicable)
7. **Line Item Creation**: Add subscription line items for new plan

### Proration Logic

The system uses existing proration services to:
- Calculate unused time credits from current subscription
- Calculate prorated charges for new subscription
- Apply proration coefficients based on billing periods
- Handle different billing cycles and customer timezones

### Transaction Safety

All subscription changes are executed within database transactions to ensure:
- Atomicity of subscription archival and creation
- Consistency of billing data
- Rollback capability on failures

### Warnings and Validations

The system provides warnings for:
- Downgrades that may remove features
- Trial period changes
- Proration impacts

Validations prevent:
- Changes to cancelled/paused subscriptions
- Invalid plan transitions
- Past effective dates

## Examples

### Basic Upgrade

```bash
curl -X POST "https://api.flexprice.com/v1/subscriptions/subs_123/change/preview" \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-api-key" \
  -d '{
    "target_plan_id": "plan_premium",
    "proration_behavior": "create_prorations"
  }'
```

### Immediate Change Without Proration

```bash
curl -X POST "https://api.flexprice.com/v1/subscriptions/subs_123/change/execute" \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-api-key" \
  -d '{
    "target_plan_id": "plan_basic",
    "proration_behavior": "none",
    "billing_cycle_anchor": "reset",
    "invoice_now": false
  }'
```

### Scheduled Change

```bash
curl -X POST "https://api.flexprice.com/v1/subscriptions/subs_123/change/execute" \
  -H "Content-Type: application/json" \
  -H "x-api-key: your-api-key" \
  -d '{
    "target_plan_id": "plan_enterprise",
    "proration_behavior": "create_prorations",
    "effective_date": "2024-02-01T00:00:00Z",
    "metadata": {
      "scheduled_change": "true",
      "reason": "annual_upgrade"
    }
  }'
```
