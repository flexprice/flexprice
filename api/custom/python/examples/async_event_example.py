#!/usr/bin/env python3
"""
Example script demonstrating how to use the async event posting functionality.
"""

import time
import logging
import os
import sys
from flexprice import Configuration, ApiClient, EventsApi
from flexprice.models import DtoIngestEventRequest

# Set up logging
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

def main():
    # Configure API key authorization
    # Configure the API client
    api_key = os.getenv("FLEXPRICE_API_KEY", "test_api_key")  # Fallback to test key if env var not set
    api_host = os.getenv("FLEXPRICE_API_HOST", "api.cloud.flexprice.io")  # Default host
    
    logger.info(f"Using API host: {api_host} (use FLEXPRICE_API_HOST env var to change)")
    logger.info("Using " + ("actual API key" if os.getenv("FLEXPRICE_API_KEY") else "test API key") + 
                " (set FLEXPRICE_API_KEY env var to use your key)")
    
    configuration = Configuration(api_key={'ApiKeyAuth': api_key})
    
    # Optionally set the server URL if not using the default
    # Remove http:// prefix for local testing
    if api_host.startswith(('http://', 'https://')):
        configuration.host = f"{api_host}/v1"
    else:
        configuration.host = f"https://{api_host}/v1"
    
    # Create API client
    api_client = ApiClient(configuration)
    events_api = EventsApi(api_client)
    
    # Track successes and failures
    success_count = 0
    failure_count = 0
    
    # Define a callback function to handle the async result
    def on_event_processed(result, error, success):
        nonlocal success_count, failure_count
        if success:
            success_count += 1
            logger.info(f"Event sent successfully: {result}")
        else:
            failure_count += 1
            logger.error(f"Event failed: {error.__class__.__name__} - {error}")
    
    # Create and send events
    for i in range(5):
        # Create event with unique data
        event = DtoIngestEventRequest(
            external_customer_id=f"customer{i}",
            event_name="api_call",
            source="test_script",
            properties={
                "region": "us-west",
                "method": "GET",
                "request_id": f"req-{i}",
                "cpu_time_ms": f"{i * 100}"
            }
        )
        
        # Send event asynchronously with callback
        logger.info(f"Submitting event {i}...")
        events_api.events_post_async(event, callback=on_event_processed)
    
    # Demonstrate fire-and-forget usage (no callback)
    forget_event = DtoIngestEventRequest(
        external_customer_id="customer-forget",
        event_name="background_task",
        source="test_script",
        properties={"task_type": "data_process", "bytes_processed": "1024"},
    )
    
    logger.info("Submitting fire-and-forget event...")
    events_api.events_post_async(forget_event)
    
    # Sleep to allow background processing to complete
    # In a real app, your process would continue doing other work
    logger.info("Waiting for events to be processed...")
    
    # Sleep in smaller increments with status updates
    for i in range(5):
        time.sleep(1)
        logger.info(f"Processed: {success_count} successful, {failure_count} failed events")
    
    logger.info("Example complete!")
    
    # Return success if at least one event was processed successfully
    return success_count > 0

if __name__ == "__main__":
    success = main()
    # Exit with appropriate status code
    sys.exit(0 if success else 1) 