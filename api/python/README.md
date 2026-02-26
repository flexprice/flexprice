# FlexPrice Python SDK

Type-safe Python client for the FlexPrice API: billing, metering, and subscription management for SaaS and usage-based products.

## Requirements

- **Python 3.10+**

## Installation

```bash
pip install flexprice-py
```

With uv or poetry:

```bash
uv add flexprice-py
# or
poetry add flexprice-py
```

## Quick start

Initialize the client with your server URL and API key, then ingest an event:

```python
from flexprice_py import Flexprice

with Flexprice(
    server_url="https://api.cloud.flexprice.io",
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
from flexprice_py import Flexprice

async def main():
    async with Flexprice(
        server_url="https://api.cloud.flexprice.io",
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

## Features

- Full API coverage (customers, plans, events, invoices, payments, entitlements, etc.)
- Sync and async support
- Type-safe request/response models (Pydantic)
- Built-in retries and error handling

For a full list of operations, see the [API reference](https://docs.flexprice.io) or the generated `docs/sdks/` in this repo.

## Troubleshooting

- **Missing or invalid API key:** Ensure `api_key_auth` is set (or set `FLEXPRICE_API_KEY` and pass it in). Keys are for server-side use only.
- **Wrong server URL:** Use `https://api.cloud.flexprice.io` (no trailing slash). The SDK appends paths as needed.
- **4xx/5xx on ingest:** Event ingest returns 202 Accepted; for errors, check request fields (`event_name`, `external_customer_id`, `properties`, `source`) against the [API docs](https://docs.flexprice.io).

## Documentation

- [FlexPrice API documentation](https://docs.flexprice.io)
- [Python SDK examples](examples/) in this repo
