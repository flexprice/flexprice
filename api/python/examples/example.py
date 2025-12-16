import os
from datetime import datetime
from flexprice import Flexprice
from flexprice.models import components

api_key = os.getenv("FLEXPRICE_API_KEY")
api_host = os.getenv("FLEXPRICE_API_HOST", "https://api.cloud.flexprice.io")

if not api_key:
    print("Error: FLEXPRICE_API_KEY required")
    exit(1)

print("FlexPrice Python SDK Example")
print("=" * 50)

sdk = Flexprice(api_key_auth=api_key, server_url=api_host)

# Send event
print("\n1. Sending event...")
try:
    event = components.DtoIngestEventRequest(
        external_customer_id="customer_123",
        event_name="api_call",
        properties={"method": "GET"},
        timestamp=datetime.now().isoformat()
    )
    result = sdk.events.ingest(request=event)
    print(f"✓ Event sent: {result.object}")
except Exception as e:
    print(f"Error: {e}")

print("\nExample completed!")
