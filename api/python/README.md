# FlexPrice Python SDK

Type-safe Python client for the FlexPrice API: billing, metering, and subscription management for SaaS and usage-based products.

## Requirements

- **Python 3.10+**

## Installation

```bash
pip install flexprice
```

With uv or poetry:

```bash
uv add flexprice
# or
poetry add flexprice
```

Runnable samples are in the `examples/` directory.

## Quick start

Initialize the client with your server URL and API key, then ingest an event:

```python
from flexprice import Flexprice

# Always include /v1 in the base URL; no trailing space or slash.
with Flexprice(
    server_url="https://us.api.flexprice.io/v1",
    api_key_auth="YOUR_API_KEY",
) as flexprice:
    # Ingest an event
    result = flexprice.events.ingest_event(
        request={
            "event_name": "Sample Event",
            "external_customer_id": "customer-123",
            "properties": {"source": "python_app", "environment": "test"},
            "source": "python_app",
        }
    )
    print(result)
```

## Async usage

The same client supports async when used as an async context manager:

```python
import asyncio
from flexprice import Flexprice

async def main():
    async with Flexprice(
        server_url="https://us.api.flexprice.io/v1",
        api_key_auth="YOUR_API_KEY",
    ) as flexprice:
        result = await flexprice.events.ingest_event_async(
            request={
                "event_name": "Sample Event",
                "external_customer_id": "customer-123",
                "properties": {"source": "python_async", "environment": "test"},
                "source": "python_async",
            }
        )
        print(result)

asyncio.run(main())
```

## Authentication

- Pass your API key as `api_key_auth` when creating the client. The SDK sends it in the `x-api-key` header.
- Prefer environment variables (e.g. `FLEXPRICE_API_KEY`) and load them in code; get keys from your [FlexPrice dashboard](https://app.flexprice.io) or docs.

## Error handling

API errors are raised as exceptions. Catch them and inspect the response as needed:

```python
try:
    with Flexprice(server_url="...", api_key_auth="...") as flexprice:
        result = flexprice.events.ingest_event(request={...})
except Exception as e:
    print(f"Error: {e}")
    # Inspect status code and body if available on the exception
```

See the [API docs](https://docs.flexprice.io) for error formats and status codes.

## Features

- Full API coverage (customers, plans, events, invoices, payments, entitlements, etc.)
- Sync and async support
- Type-safe request/response models (Pydantic)
- Built-in retries and error handling

For a full list of operations, see the [API reference](https://docs.flexprice.io) and the [examples](examples/) in this repo.

## Troubleshooting

- **Missing or invalid API key:** Ensure `api_key_auth` is set (or set `FLEXPRICE_API_KEY` and pass it in). Keys are for server-side use only.
- **Wrong server URL:** Use `https://us.api.flexprice.io/v1`. Always include `/v1`; no trailing space or slash.
- **4xx/5xx on ingest:** Event ingest returns 202 Accepted; for errors, check request fields (`event_name`, `external_customer_id`, `properties`, `source`) against the [API docs](https://docs.flexprice.io).

## Handling Webhooks

Flexprice sends webhook events to your server for async updates on payments, invoices, subscriptions, wallets, and more.

**Flow:**
1. Register your endpoint URL in the Flexprice dashboard
2. Receive `POST` with raw JSON body
3. Read `event_type` to route
4. Parse payload into typed model
5. Handle business logic idempotently
6. Return `200` quickly

```python
import json
from flexprice.models import (
    WebhookDtoPaymentWebhookPayload,
    WebhookDtoSubscriptionWebhookPayload,
    WebhookDtoInvoiceWebhookPayload,
)

def handle_webhook(raw_body: str) -> None:
    event = json.loads(raw_body)

    match event.get("event_type"):
        case "payment.success" | "payment.failed" | "payment.updated":
            payload = WebhookDtoPaymentWebhookPayload.model_validate(event)
            if payload.payment:
                print(f"payment {payload.payment.id}")
                # TODO: update payment record

        case "subscription.activated" | "subscription.cancelled" | "subscription.updated":
            payload = WebhookDtoSubscriptionWebhookPayload.model_validate(event)
            if payload.subscription:
                print(f"subscription {payload.subscription.id}")

        case "invoice.update.finalized" | "invoice.payment.overdue":
            payload = WebhookDtoInvoiceWebhookPayload.model_validate(event)
            if payload.invoice:
                print(f"invoice {payload.invoice.id}")

        case _:
            print(f"unhandled event: {event.get('event_type')}")
```

### Event types

| Category | Events |
|---|---|
| **Payment** | `payment.created` · `payment.updated` · `payment.success` · `payment.failed` · `payment.pending` |
| **Invoice** | `invoice.create.drafted` · `invoice.update` · `invoice.update.finalized` · `invoice.update.payment` · `invoice.update.voided` · `invoice.payment.overdue` · `invoice.communication.triggered` |
| **Subscription** | `subscription.created` · `subscription.draft.created` · `subscription.activated` · `subscription.updated` · `subscription.paused` · `subscription.resumed` · `subscription.cancelled` · `subscription.renewal.due` |
| **Subscription Phase** | `subscription.phase.created` · `subscription.phase.updated` · `subscription.phase.deleted` |
| **Customer** | `customer.created` · `customer.updated` · `customer.deleted` |
| **Wallet** | `wallet.created` · `wallet.updated` · `wallet.terminated` · `wallet.transaction.created` · `wallet.credit_balance.dropped` · `wallet.credit_balance.recovered` · `wallet.ongoing_balance.dropped` · `wallet.ongoing_balance.recovered` |
| **Feature / Entitlement** | `feature.created` · `feature.updated` · `feature.deleted` · `feature.wallet_balance.alert` · `entitlement.created` · `entitlement.updated` · `entitlement.deleted` |
| **Credit Note** | `credit_note.created` · `credit_note.updated` |

**Production rules:**
- Keep handlers idempotent — Flexprice retries on non-`2xx`
- Return `200` for unknown event types — prevents unnecessary retries
- Do heavy processing async — respond fast, queue the work

## Documentation

- [FlexPrice API documentation](https://docs.flexprice.io)
- [Python SDK examples](examples/) in this repo
