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

## Documentation

- [FlexPrice API documentation](https://docs.flexprice.io)
- [Python SDK examples](examples/) in this repo
