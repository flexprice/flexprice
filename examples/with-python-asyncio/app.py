import os
import asyncio
import datetime
from dotenv import load_dotenv

import flexprice
from flexprice.api import events_api
from flexprice.models.dto_ingest_event_request import DtoIngestEventRequest


async def main() -> None:
    load_dotenv()
    api_key = os.getenv("FLEXPRICE_API_KEY")
    api_host = os.getenv("FLEXPRICE_API_HOST", "api.cloud.flexprice.io")
    customer_id = os.getenv("CUSTOMER_ID")

    if not api_key or not customer_id:
        raise RuntimeError("Missing env vars: FLEXPRICE_API_KEY, CUSTOMER_ID")

    # Configure client
    configuration = flexprice.Configuration(host=f"https://{api_host}/v1")
    configuration.api_key['x-api-key'] = api_key

    async def post_event_async(api: events_api.EventsApi, req: DtoIngestEventRequest):
        """Wrap the SDK's async submission into an asyncio Future."""
        loop = asyncio.get_running_loop()
        fut: asyncio.Future = loop.create_future()

        def _cb(error, data, response):
            if error:
                loop.call_soon_threadsafe(fut.set_exception, Exception(str(error)))
            else:
                loop.call_soon_threadsafe(fut.set_result, data)

        api.events_post_async(event=req, callback=_cb)
        return await fut

    with flexprice.ApiClient(configuration) as api_client:
        api = events_api.EventsApi(api_client)

        # Simulate bursty async tracking using official SDK
        e1 = DtoIngestEventRequest(
            event_name="api.request",
            external_customer_id=customer_id,
            properties={"endpoint": "/v1/chat.completions", "units": 5, "source": "with-python-asyncio"},
            source="with-python-asyncio",
        )
        e2 = DtoIngestEventRequest(
            event_name="generation.tokens",
            external_customer_id=customer_id,
            properties={"model": "claude-3.5-sonnet", "units": 2048, "source": "with-python-asyncio"},
            source="with-python-asyncio",
        )

        await asyncio.gather(post_event_async(api, e1), post_event_async(api, e2))

        # Summary (last 24h) via client-side aggregation
        data = await api.events_get_async(external_customer_id=customer_id)
        events = getattr(data, 'events', []) or []
        since = datetime.datetime.utcnow() - datetime.timedelta(hours=24)
        total_units = 0.0
        for ev in events:
            ts = getattr(ev, 'timestamp', None) or ""
            try:
                when = datetime.datetime.fromisoformat(ts.replace("Z", "+00:00"))
            except Exception:
                when = None
            if when and when >= since:
                props = getattr(ev, 'properties', {}) or {}
                try:
                    total_units += float(props.get('units', 0))
                except Exception:
                    pass

        print("Summary:", {"total_units": total_units})


if __name__ == "__main__":
    asyncio.run(main())
