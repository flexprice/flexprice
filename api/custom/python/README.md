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

## Environment

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `FLEXPRICE_API_KEY` | Yes | API key |
| `FLEXPRICE_API_HOST` | Optional | Full base URL including `https://` and `/v1` (default: `https://us.api.flexprice.io/v1`). No trailing slash. |

**Integration tests** in [api/tests/python/test_sdk.py](../tests/python/test_sdk.py) use a different env shape; see [api/tests/README.md](../tests/README.md).

## Quick start

Initialize the client, create a customer, ingest an event:

```python
import os
from flexprice import Flexprice

api_key = os.getenv("FLEXPRICE_API_KEY", "YOUR_API_KEY")
server_url = os.getenv(
    "FLEXPRICE_API_HOST", "https://us.api.flexprice.io/v1"
)

with Flexprice(server_url=server_url, api_key_auth=api_key) as client:
    external_id = "customer-123"
    client.customers.create_customer(
        external_id=external_id,
        email="user@example.com",
        name="Example Customer",
    )
    result = client.events.ingest_event(
        request={
            "event_name": "Sample Event",
            "external_customer_id": external_id,
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
import os
from flexprice import Flexprice

async def main():
    server_url = os.getenv(
        "FLEXPRICE_API_HOST", "https://us.api.flexprice.io/v1"
    )
    async with Flexprice(
        server_url=server_url,
        api_key_auth=os.getenv("FLEXPRICE_API_KEY", "YOUR_API_KEY"),
    ) as client:
        result = await client.events.ingest_event_async(
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
- Set `FLEXPRICE_API_HOST` to a full URL (see [Environment](#environment)) or use the default `https://us.api.flexprice.io/v1`.
- Prefer environment variables; get keys from your [FlexPrice dashboard](https://app.flexprice.io) or docs.

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
- **Wrong server URL:** Use a full URL such as `https://us.api.flexprice.io/v1` (include `/v1`; no trailing slash).
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
- [SDK integration tests](../tests/README.md) — different `FLEXPRICE_API_HOST` shape for automated tests
