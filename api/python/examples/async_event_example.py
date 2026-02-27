#!/usr/bin/env python3
"""
FlexPrice Python SDK example – async client, ingest events.
Set FLEXPRICE_API_KEY (and optionally FLEXPRICE_API_HOST) in .env or environment.
"""

import asyncio
import logging
import os
from dotenv import load_dotenv

load_dotenv()

from flexprice import Flexprice

logging.basicConfig(level=logging.INFO, format="%(asctime)s - %(levelname)s - %(message)s")
logger = logging.getLogger(__name__)


async def main():
    api_key = os.getenv("FLEXPRICE_API_KEY")
    api_host = os.getenv("FLEXPRICE_API_HOST", "us.api.flexprice.io/v1")

    if not api_key:
        raise SystemExit("Set FLEXPRICE_API_KEY in .env or environment")

    server_url = api_host if api_host.startswith(("http://", "https://")) else f"https://{api_host}"

    logger.info("Using async FlexPrice client")

    async with Flexprice(
        server_url=server_url,
        api_key_auth=api_key,
    ) as flexprice:
        result = await flexprice.events.ingest_event_async(
            request={
                "event_name": "Sample Event",
                "external_customer_id": "customer-async-123",
                "properties": {"source": "python_async", "environment": "test"},
                "source": "python_async",
            }
        )
        logger.info("Event ingested: %s", result)

    logger.info("Async example complete!")


if __name__ == "__main__":
    asyncio.run(main())
