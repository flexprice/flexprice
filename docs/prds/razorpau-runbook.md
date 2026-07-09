# Razorpay API Runbook — Two Checkout Scenarios

Status: **Implementation reference, validated against live test-mode calls.**
Companion to: `docs/prds/razorpay-upi-autopay-prd.md`.

Two scenarios, driven by the checkout request's `collection_method` (per the PRD §7.2).

---

## Scenario 1: `collection_method = send_invoice` (manual payment, unchanged baseline)

Two calls, no mandate involved.

**1. Create customer**

```bash
curl -u <KEY_ID>:<KEY_SECRET> \
  -X POST https://api.razorpay.com/v1/customers \
  -H "Content-Type: application/json" \
  -d '{
    "name": "<CUSTOMER_NAME>",
    "contact": "<CUSTOMER_CONTACT>",
    "email": "<CUSTOMER_EMAIL>",
    "fail_existing": "0",
    "notes": { "flexprice_customer_id": "<FLEXPRICE_CUSTOMER_ID>" }
  }'
```

`fail_existing: "0"` makes this idempotent — a duplicate `contact`/`email` returns the existing customer instead of erroring.

**2. Create Payment Link**

```bash
curl -u <KEY_ID>:<KEY_SECRET> \
  -X POST https://api.razorpay.com/v1/payment_links \
  -H "Content-Type: application/json" \
  -d '{
    "amount": <INVOICE_AMOUNT_PAISE>,
    "currency": "INR",
    "description": "<INVOICE_DESCRIPTION>",
    "reference_id": "<FLEXPRICE_INVOICE_ID>",
    "customer": {
      "name": "<CUSTOMER_NAME>",
      "contact": "<CUSTOMER_CONTACT>",
      "email": "<CUSTOMER_EMAIL>"
    },
    "notify": { "sms": true, "email": true },
    "reminder_enable": true
  }'
```

Returns `short_url` — send to customer, done. No further action on our side until the `payment_link.paid` webhook arrives.

---

## Scenario 2: `collection_method = charge_automatically` (UPI Autopay)

### Step 1 — Register mandate + pay first invoice, in one call

```bash
curl -u <KEY_ID>:<KEY_SECRET> \
  -X POST https://api.razorpay.com/v1/subscription_registration/auth_links \
  -H "Content-Type: application/json" \
  -d '{
    "customer": {
      "name": "<CUSTOMER_NAME>",
      "contact": "<CUSTOMER_CONTACT>",
      "email": "<CUSTOMER_EMAIL>"
    },
    "type": "link",
    "amount": <FIRST_INVOICE_AMOUNT_PAISE>,
    "currency": "INR",
    "description": "<INVOICE_DESCRIPTION>",
    "subscription_registration": {
      "method": "upi",
      "max_amount": <MANDATE_CEILING_PAISE>,
      "expire_at": <MANDATE_EXPIRY_UNIX_TS>
    },
    "receipt": "<FLEXPRICE_INVOICE_ID>",
    "email_notify": true,
    "sms_notify": true,
    "notes": { "flexprice_customer_id": "<FLEXPRICE_CUSTOMER_ID>" }
  }'
```

**Confirmed via testing**: this one call handles customer creation itself — if a customer with matching `contact`/`email` already exists, it reuses that same customer rather than creating a duplicate. Idempotent, same spirit as `fail_existing: "0"` on the plain customer endpoint.

Returns `short_url` — customer opens it, authorizes the mandate, and pays the first invoice together, in one action.

### Step 2 — Every subsequent invoice (renewal, or any future auto-charge)

**2a. Get valid tokens for the customer**

```bash
curl -u <KEY_ID>:<KEY_SECRET> \
  -X GET https://api.razorpay.com/v1/customers/<CUSTOMER_ID>/tokens
```

- **No confirmed token found** → fall back to Step 1 (send a fresh registration link — mandate was never set up, expired, or was cancelled).
- **Confirmed token found** → continue to 2b.

**2b. Create an order for this invoice**

```bash
curl -u <KEY_ID>:<KEY_SECRET> \
  -X POST https://api.razorpay.com/v1/orders \
  -H "Content-Type: application/json" \
  -d '{
    "amount": <INVOICE_AMOUNT_PAISE>,
    "currency": "INR",
    "payment_capture": true
  }'
```

**2c. Charge the token against that order**

```bash
curl -u <KEY_ID>:<KEY_SECRET> \
  -X POST https://api.razorpay.com/v1/payments/create/recurring \
  -H "Content-Type: application/json" \
  -d '{
    "email": "<CUSTOMER_EMAIL>",
    "contact": "<CUSTOMER_CONTACT>",
    "amount": <INVOICE_AMOUNT_PAISE>,
    "currency": "INR",
    "order_id": "<ORDER_ID_FROM_2b>",
    "customer_id": "<CUSTOMER_ID>",
    "token": "<TOKEN_ID_FROM_2a>",
    "recurring": true,
    "description": "<INVOICE_DESCRIPTION>"
  }'
```

---

## Idempotency notes

- **Customer creation**: idempotent — both the plain `/v1/customers` endpoint (`fail_existing: "0"`) and `subscription_registration/auth_links` dedupe on `contact`/`email` rather than creating a duplicate.
- **Auto-charge is asynchronous**: `create/recurring` (2c) returns immediately with `status: "created"` — that's a submission acknowledgment, not a final result. Actual capture confirmation (success or failure) arrives later, on your side via webhook (`payment.captured`/`payment.failed`) or by checking the dashboard/`GET /v1/payments/{id}`. Don't treat the 2c response itself as proof of a completed charge.
- **Order-level protection against double-submission**: once a specific `order_id` has been paid, Razorpay won't let you charge that _same order_ again — a retried 2c call against an already-paid order fails safely rather than double-charging. Good, but worth being precise about what this does and doesn't cover: it protects against _retrying the exact same order_, not against accidentally creating a _second, brand-new_ order for an invoice that was already charged via a different order. That second case is what our own system's idempotency claim (the PRD's `InvoiceCharge`/`TokenCycleCharge` mechanism, §9–§10) exists to prevent — always reuse Razorpay's own per-order protection as a nice extra backstop, not a replacement for our own claim before step 2b even runs.
