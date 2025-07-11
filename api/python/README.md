# FlexPrice Python SDK

This is the Python client library for the FlexPrice API.

## Installation

```bash
pip install flexprice
```

## Usage

```python
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
```

## Asynchronous Event Submission

The FlexPrice SDK provides asynchronous event submission functionality that allows you to:

- Submit events in a non-blocking manner with "fire-and-forget" capability
- Include optional callbacks to handle success/failure responses
- Automatically retry failed event submissions with exponential backoff
- Process events in background threads

### Basic Async Usage

```python
from flexprice import Configuration, ApiClient, EventsApi
from flexprice.models import DtoIngestEventRequest

# Configure the client
configuration = Configuration(api_key={'ApiKeyAuth': 'YOUR_API_KEY'})
configuration.host = "https://api.cloud.flexprice.io/v1"

# Create API client and event API instance
api_client = ApiClient(configuration)
events_api = EventsApi(api_client)

# Create an event
event = DtoIngestEventRequest(
    external_customer_id="customer123",
    event_name="api_call",
    properties={"region": "us-west", "method": "GET"},
    source="my_application"
)

# Submit asynchronously (fire-and-forget)
events_api.events_post_async(event)
```

### Using Callbacks

```python
# Define a callback function
def on_event_processed(result, error, success):
    if success:
        print(f"Event processed successfully: {result}")
    else:
        print(f"Event processing failed: {error}")

# Create and submit event with callback
event = DtoIngestEventRequest(
    external_customer_id="customer123",
    event_name="user_action",
    properties={"action": "login", "device": "mobile"},
    source="user_portal"
)

# Submit with callback
events_api.events_post_async(event, callback=on_event_processed)
```

### Complete Example

For a complete example of asynchronous event submission, see the `async_event_example.py` file in the examples directory.

## Running the Example

To run the provided example:

1. Clone the repository:
   ```bash
   git clone https://github.com/flexprice/python-sdk.git
   cd python-sdk/examples
   ```

2. Create a virtual environment and install dependencies:
   ```bash
   python -m venv venv
   source venv/bin/activate  # On Windows: venv\Scripts\activate
   pip install -r requirements.txt
   ```

3. Create a `.env` file with your API credentials:
   ```bash
   cp .env.sample .env
   # Edit .env with your API key
   ```

4. Run the example:
   ```bash
   python example.py
   ```

5. Run the async example:
   ```bash
   python async_event_example.py
   ```

## Features

- Complete API coverage
- Strong type hints
- Detailed documentation
- Error handling
- Asynchronous support for event submission

## Documentation

For detailed API documentation, refer to the code comments and the official FlexPrice API documentation.

## Advanced Usage

### Handling Errors

The SDK provides detailed error information through exceptions:

```python
try:
    # API call
    result = client.some_api_call()
except flexprice.ApiException as e:
    print(f"API exception: {e}")
    print(f"Status code: {e.status}")
    print(f"Response body: {e.body}")
except Exception as e:
    print(f"General exception: {e}")
```

### Asynchronous API Usage with asyncio

In addition to the built-in asynchronous event submission, the SDK can be used with libraries like `asyncio` for other operations:

```python
import asyncio
import flexprice
from flexprice.api import customers_api

async def get_customer(customer_id):
    configuration = flexprice.Configuration(
        host="https://api.cloud.flexprice.io/v1"
    )
    configuration.api_key['x-api-key'] = "your-api-key"
    
    async with flexprice.ApiClient(configuration) as api_client:
        api = customers_api.CustomersApi(api_client)
        return await api.customers_id_get(id=customer_id)

# Run with asyncio
customer = asyncio.run(get_customer("customer-123"))
print(customer)
``` 