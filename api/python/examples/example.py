#!/usr/bin/env python3
"""
FlexPrice Python SDK example – sync client, create customer and ingest event.

Default base URL: https://us.api.flexprice.io/v1
Override with FLEXPRICE_API_HOST (full URL). Set FLEXPRICE_API_KEY in .env or environment.
"""

import os
import time
from dotenv import load_dotenv

load_dotenv()

from flexprice import Flexprice


def main():
    api_key = os.getenv("FLEXPRICE_API_KEY")
    server_url = os.getenv(
        "FLEXPRICE_API_HOST", "https://us.api.flexprice.io/v1"
    )

    if not api_key:
        raise SystemExit("Set FLEXPRICE_API_KEY in .env or environment")

    customer_id = f"sample-customer-{int(time.time())}"

    print("Starting FlexPrice Python SDK example...")
    print(f"Using API key: {api_key[:4]}...{api_key[-4:]}")

    try:
        with Flexprice(
            server_url=server_url,
            api_key_auth=api_key,
        ) as client:
            client.customers.create_customer(
                external_id=customer_id,
                email=f"sample-{customer_id}@example.com",
                name="Example Customer",
            )
            print("Customer created:", customer_id)

            result = client.events.ingest_event(
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
