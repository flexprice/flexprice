#!/usr/bin/env python3
"""
FlexPrice Python SDK Example

This example demonstrates how to use the FlexPrice Python SDK
to interact with the FlexPrice API.
"""

import os
import time
import datetime
from pprint import pprint

# Import the FlexPrice SDK
import flexprice
from flexprice.api import customers_api, events_api
from flexprice.models.dto_create_customer_request import DtoCreateCustomerRequest
from flexprice.models.dto_ingest_event_request import DtoIngestEventRequest

# Optional: Load environment variables from .env file
from dotenv import load_dotenv
load_dotenv()


def run_example():
    """Main example function demonstrating FlexPrice SDK usage."""
    print("Starting FlexPrice Python SDK example...")

    try:
        # Configure the API client
        api_key = os.getenv("FLEXPRICE_API_KEY")
        api_host = os.getenv("FLEXPRICE_API_HOST", "api.cloud.flexprice.io")

        if not api_key:
            raise ValueError("FLEXPRICE_API_KEY environment variable is required")
            
        print("Using API Key:", api_key[:4] + "..." + api_key[-4:])  # Show just the start and end for security

        # Configure API key authorization
        configuration = flexprice.Configuration(
            host=f"https://{api_host}/v1"
        )
        configuration.api_key['x-api-key'] = api_key
       
        # Create API client
        with flexprice.ApiClient(configuration) as api_client:
            # Set the API key header
            api_client.default_headers['x-api-key'] = api_key
            # Add User-Agent header
            configuration.user_agent = "FlexPricePythonSDK/1.0.0 Example"
            # Print actual headers for debugging
            
            # Create API instances
            events_api_instance = events_api.EventsApi(api_client)

            # Generate a unique customer ID for this example
            customer_id = f"sample-customer-{int(time.time())}"
            
            print(f"Creating customer with ID: {customer_id}...")

            # Step 1: Create an event
            print("Creating event...")
            
            event_request = DtoIngestEventRequest(
                event_name="Sample Event",
                external_customer_id=customer_id,
                properties={
                    "source": "python_sample_app",
                    "environment": "test",
                    "timestamp": datetime.datetime.now().isoformat()
                },
                source="python_sample_app"
            )
            
            event_result = events_api_instance.events_post(event=event_request)
            print(f"Event created successfully! ID: {event_result.event_id if hasattr(event_result, 'event_id') else 'unknown'}")
            
            # Sleep for 1 second to allow the event to be processed
            time.sleep(1)

            # Step 2: Retrieve events for this customer
            print(f"Retrieving events for customer {customer_id}...")
            
            events_response = events_api_instance.events_get(external_customer_id=customer_id)
            
            # Check if events are available in the response
            if hasattr(events_response, 'events') and events_response.events:
                print(f"Found {len(events_response.events)} events:")
                
                for i, event in enumerate(events_response.events):
                    print(f"Event {i+1}: {event.id if hasattr(event, 'id') else 'unknown'} - {event.event_name if hasattr(event, 'event_name') else 'unknown'}")
                    print(f"Properties: {event.properties if hasattr(event, 'properties') else {}}")
            else:
                print("No events found or events not available in response.")
            
            print("Example completed successfully!")

    except flexprice.ApiException as e:
        print(f"\n=== API Exception ===")
        print(f"Status code: {e.status}")
        print(f"Reason: {e.reason}")
        print(f"HTTP response headers: {e.headers}")
        print(f"HTTP response body: {e.body}")    
    except ValueError as e:
        print(f"Value error: {e}")
    except Exception as e:
        print(f"Unexpected error: {e}")


if __name__ == "__main__":
    run_example() 