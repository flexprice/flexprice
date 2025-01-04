### Product Requirements Document: Invoice Entity for Flexprice

#### Overview

The Invoice entity is a core component to enable detailed billing for Flexprice. It will handle generating invoices for various use cases, including subscriptions and one-off charges. This document outlines the technical requirements and functionalities of the Invoice entity to ensure alignment with Flexprice's goals and existing architecture.

---

### Objectives

1. Provide a structured invoice entity to track charges and payments.
2. Enable both manual and automated invoice creation workflows.
3. Support multiple states for invoice lifecycle management.
4. Integrate seamlessly with existing subscription workflows.
5. Support payment status updates and void operations.
6. Provide API endpoints for creating, retrieving, updating, and managing invoices.

---

### Functional Requirements

#### Invoice Types

1. **Subscription:** Automatically generated for subscription-based billing.
2. **One-off:** Manual invoices for one-time charges.
3. **Credit:** Credit notes for refunds or adjustments.

#### Invoice States

1. **Invoice Status:**
   - **Draft:** Initial state, can be modified
   - **Finalized:** Ready for payment processing
   - **Voided:** Canceled or invalid

2. **Payment Status:**
   - **Pending:** Awaiting payment
   - **Succeeded:** Payment completed
   - **Failed:** Payment attempt failed

#### Supported Actions

1. **Create Invoice:** Create new invoices with specified type and details.
2. **Update Payment Status:** Track payment lifecycle with proper validation.
3. **Void Invoice:** Cancel an invoice when needed.
4. **Fetch Invoice Details:** Retrieve detailed invoice information.
5. **List Invoices:** Filter invoices by various criteria.

---

### Data Model

The Invoice entity includes the following fields:

| Field             | Type         | Description                                               |
| ----------------- | ------------ | --------------------------------------------------------- |
| `id`              | `string`     | Unique identifier                                         |
| `tenant_id`       | `string`     | Tenant identifier                                         |
| `customer_id`     | `string`     | Customer identifier                                       |
| `subscription_id` | `string`     | (Optional) Related subscription                           |
| `invoice_type`    | `enum`       | Subscription, One-off, Credit                             |
| `invoice_status`  | `enum`       | Draft, Finalized, Voided                                  |
| `payment_status`  | `enum`       | Pending, Succeeded, Failed                                |
| `currency`        | `string`     | Currency code (e.g., USD)                                 |
| `amount_due`      | `decimal`    | Total amount due                                          |
| `amount_paid`     | `decimal`    | Amount paid so far                                        |
| `amount_remaining`| `decimal`    | Amount still to be paid                                   |
| `description`     | `string`     | Invoice description                                       |
| `due_date`        | `datetime`   | Payment due date                                          |
| `paid_at`         | `datetime`   | When payment was completed                                |
| `voided_at`       | `datetime`   | When invoice was voided                                   |
| `finalized_at`    | `datetime`   | When invoice was finalized                                |
| `invoice_pdf_url` | `string`     | URL to invoice PDF                                        |
| `billing_reason`  | `string`     | Reason for invoice generation                             |
| `metadata`        | `json`       | Additional metadata                                        |
| `version`         | `int`        | Optimistic locking version                                |
| `status`          | `enum`       | Record status (Published, Deleted)                        |

---

### Subscription Integration

The invoice system is tightly integrated with the subscription workflow to automate billing:

1. **Billing Period Updates:**
   - Every 15 minutes, the system checks for subscriptions due for billing period updates
   - For each subscription, in a transaction:
     1. Creates a draft invoice for the current period
     2. Calculates usage charges for the period
     3. Finalizes the invoice
     4. Updates subscription billing period
   - If any step fails, the entire transaction is rolled back

2. **Usage Calculation:**
   - System aggregates usage events for the billing period
   - Applies pricing tiers and rules based on subscription plan
   - Includes both recurring charges and usage-based charges

3. **Invoice Generation:**
   - Automatically creates draft invoice with:
     - Subscription details
     - Usage breakdown
     - Due date (7 days from period end)
   - Finalizes invoice immediately to start payment collection

4. **Error Handling:**
   - Transactional processing ensures data consistency
   - Failed subscriptions are logged and can be retried
   - Monitoring alerts for processing failures

---

### API Endpoints

#### Create Invoice

**POST** `/v1/invoices`

```json
{
  "customer_id": "cus_123",
  "subscription_id": "sub_123",
  "invoice_type": "subscription",
  "currency": "USD",
  "amount_due": "100.00",
  "description": "Monthly subscription charge",
  "due_date": "2024-01-31T00:00:00Z"
}
```

#### Update Payment Status

**PUT** `/v1/invoices/{id}/payment`

```json
{
  "payment_status": "succeeded",
  "amount": "100.00"
}
```

#### List Invoices

**GET** `/v1/invoices`

Query Parameters:
- `customer_id`
- `subscription_id`
- `invoice_type`
- `invoice_status`
- `payment_status`
- `start_time`
- `end_time`

---

### State Transitions

#### Payment Status Transitions

```
Pending -> Succeeded
Pending -> Failed
Failed -> Pending
```

Each transition updates relevant fields:
- **Succeeded:** Updates amount_paid, amount_remaining, paid_at
- **Failed:** Resets amount_paid, updates amount_remaining
- **Pending:** Initial state for new invoices

---

### Integration Points

1. **Subscription System:**
   - Auto-generate invoices for subscription events
   - Track subscription-related metadata
   - Handle billing period transitions

2. **Payment Processing:**
   - Handle payment status updates
   - Maintain payment audit trail

3. **Reporting System:**
   - Track invoice metrics
   - Generate financial reports

---

### Monitoring and Alerts

1. **Key Metrics:**
   - Invoice generation success rate
   - Payment success rate
   - Processing time for billing updates
   - Number of failed transactions

2. **Alerts:**
   - Failed subscription processing
   - Payment processing errors
   - Unusual payment patterns
   - System performance degradation
