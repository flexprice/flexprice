#!/usr/bin/env python3
"""
FlexPrice Python SDK example – sync client, create customer and ingest event.
Set FLEXPRICE_API_KEY (and optionally FLEXPRICE_API_HOST) in .env or environment.
"""

import os
import time
from dotenv import load_dotenv

load_dotenv()

from flexprice import Flexprice

def main():
    api_key = os.getenv("FLEXPRICE_API_KEY")
    api_host = os.getenv("FLEXPRICE_API_HOST", "us.api.flexprice.io")

    if not api_key:
        raise SystemExit("Set FLEXPRICE_API_KEY in .env or environment")

    server_url = api_host if api_host.startswith(("http://", "https://")) else f"https://{api_host}"
    customer_id = f"sample-customer-{int(time.time())}"

    print("Starting FlexPrice Python SDK example...")
    print(f"Using API key: {api_key[:4]}...{api_key[-4:]}")

    try:
        with Flexprice(
            server_url=server_url,
            api_key_auth=api_key,
        ) as flexprice:
            result = flexprice.events.ingest_event(
                request={
                    "event_name": "Sample Event",
                    "external_customer_id": customer_id,
                    "properties": {"source": "python_app", "environment": "test"},
                    "source": "python_app",
                }
            )
            print("Event ingested:", result)
        print("Example completed successfully!")
    except Exception as e:
        print(f"Error: {e}")
        raise

if __name__ == "__main__":
    main()
