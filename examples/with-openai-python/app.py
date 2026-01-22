import os
import time
import datetime
from dotenv import load_dotenv

import flexprice
from flexprice.api import events_api
from flexprice.models.dto_ingest_event_request import DtoIngestEventRequest

from openai import OpenAI


def main() -> None:
    load_dotenv()

    # Env
    openai_key = os.getenv("OPENAI_API_KEY")
    api_key = os.getenv("FLEXPRICE_API_KEY")
    api_host = os.getenv("FLEXPRICE_API_HOST", "api.cloud.flexprice.io")
    customer_id = os.getenv("CUSTOMER_ID", "cust_demo_123")

    if not openai_key:
        raise RuntimeError("Missing env var: OPENAI_API_KEY")
    if not api_key:
        raise RuntimeError("Missing env var: FLEXPRICE_API_KEY")

    # 1) Call OpenAI
    client = OpenAI(api_key=openai_key)

    start = time.time()
    completion = client.chat.completions.create(
        model="gpt-4o-mini",
        messages=[
            {"role": "system", "content": "Be concise."},
            {"role": "user", "content": "Say hello and count to 3."},
        ],
    )
    latency_ms = int((time.time() - start) * 1000)
    usage = getattr(completion, "usage", None) or {}
    total_tokens = int(getattr(usage, "total_tokens", 0) or 0)

    # 2) Report usage to Flexprice via official Python SDK
    configuration = flexprice.Configuration(host=f"https://{api_host}/v1")
    configuration.api_key['x-api-key'] = api_key

    with flexprice.ApiClient(configuration) as api_client:
        events = events_api.EventsApi(api_client)

        event = DtoIngestEventRequest(
            event_name="openai.chat.completion",
            external_customer_id=customer_id,
            properties={
                "units": total_tokens,
                "provider": "openai",
                "model": getattr(completion, "model", "unknown"),
                "latency_ms": latency_ms,
                "source": "with-openai-python",
            },
            source="with-openai-python",
            timestamp=datetime.datetime.utcnow().isoformat() + "Z",
        )
        events.events_post(event=event)

        # 3) Simple summary (last hour)
        data = events.events_get(external_customer_id=customer_id)
        evs = getattr(data, "events", []) or []
        one_hour_ago = datetime.datetime.utcnow() - datetime.timedelta(hours=1)
        total_units = 0
        for e in evs:
            ts_str = getattr(e, "timestamp", "") or ""
            try:
                ts = datetime.datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
            except Exception:
                ts = None
            if ts and ts >= one_hour_ago:
                props = getattr(e, "properties", {}) or {}
                try:
                    total_units += float(props.get("units", 0))
                except Exception:
                    pass

        print("OpenAI response:", completion.choices[0].message.content)
        print("Flexprice total units (last hour):", int(total_units))


if __name__ == "__main__":
    main()
