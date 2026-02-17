import json
import requests
import time
from datetime import datetime, timezone, timedelta
from concurrent.futures import ThreadPoolExecutor
from uuid import uuid4

import logging

# Set up logging
logger = logging.getLogger(__name__)
logger.setLevel(logging.INFO)

# File handler to save logs to a file
file_handler = logging.FileHandler("ingestion.log" if __name__ == "__main__" else "ingestion.log")
file_handler.setLevel(logging.INFO)

# Formatter for log messages
formatter = logging.Formatter("%(asctime)s - %(levelname)s - %(message)s")
file_handler.setFormatter(formatter)

# Stream handler to print logs to the console (optional)
stream_handler = logging.StreamHandler()
stream_handler.setLevel(logging.INFO)
stream_handler.setFormatter(formatter)

# Add handlers to the logger
logger.addHandler(file_handler)
logger.addHandler(stream_handler)


# Constants for ingestion
NUM_EVENTS = 1000000
BATCH_SIZE = 150
REQUESTS_PER_SEC = 150
API_ENDPOINT = "http://localhost:8080/v1/events/ingest"
MAX_RETRIES = 1
INITIAL_BACKOFF = 0.1
BEARER_TOKEN = ""


def generate_event(index):
    """
    Generate a mock event with ISO 8601 timestamp.
    """
    sources = ["web", "mobile"]
    event_types = ["gpu_time"]
    # event_timestamp = datetime.now(timezone.utc)

    return {
        "event_id": str(uuid4()),
        "event_name": event_types[index % len(event_types)],
        "external_customer_id": f"cus_loadtest_5",
        "source": sources[index % len(sources)],
        "properties": {
            "bytes_transferred": 100 + (index % 1000),
            "duration_ms": 50 + (index % 200),
            "status_code": 200 + ((index % 3) * 100),
            "test_group": f"group_{index % 10}",
        },
    }


def post_event(event):
    """
    Post an event to the API endpoint with retries and validation.
    """
    retries = 0
    backoff = INITIAL_BACKOFF
    headers = {
        "Authorization": f"Bearer {BEARER_TOKEN}",
        "Content-Type": "application/json",
    }

    while retries <= MAX_RETRIES:
        try:
            payload = json.dumps(event)
            response = requests.post(API_ENDPOINT, data=payload, headers=headers, timeout=5)

            if response.status_code == 202:
                logger.info(f"Event ingested successfully: {event['event_id']}")
                return True
            elif response.status_code in [429, 500]:
                retries += 1
                time.sleep(backoff)
                backoff *= 2
                continue
            else:
                logger.error(f"Unexpected status code: {response.status_code} - {response.text}")
                return False
        except Exception as e:
            logger.error(f"Failed to post event: {e}")
            retries += 1
            time.sleep(backoff)
            backoff *= 2
    return False


def seed_events_clickhouse(config):
    """
    Seed multiple events to the API.
    """
    logger.info("Starting event ingestion...")
    with ThreadPoolExecutor(max_workers=REQUESTS_PER_SEC) as executor:
        for i in range(0, NUM_EVENTS, BATCH_SIZE):
            batch = [generate_event(j) for j in range(i, min(i + BATCH_SIZE, NUM_EVENTS))]
            executor.map(post_event, batch)
            time.sleep(1)
    logger.info("Event ingestion completed")
