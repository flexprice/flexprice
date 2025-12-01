# Benthos Flexprice Integration

Simple Benthos configs to test event ingestion into Flexprice.

## Quick Start

```bash
cd benthos

# Set your API credentials
export FLEXPRICE_BASE_URL=http://localhost:8080
export FLEXPRICE_API_KEY=your_api_key

# Run single event test
benthos -c config.yaml

# Or run bulk test
benthos -c config-bulk.yaml
```

## Files

**Test Configs:**
- `config.yaml` - Send 100 random events individually
- `config-bulk.yaml` - Send 1000 random events in batches
- `config-real-customer.yaml` - Send 50 events for real customer (334230687423)
- `config-real-bulk.yaml` - Send 500 batched events for real customer
- `test.sh` - Helper script

## Bloblang Mapping

Bloblang is used in the `mapping:` section to transform data. Example:

```yaml
mapping: |
  root.event_name = "feature 1"
  root.external_customer_id = "334230687423"
  root.properties = {
    "feature 1": random_int(min: 1, max: 10)
  }
```

This transforms generated data into Flexprice event format.

## Test Real Customer

```bash
export FLEXPRICE_BASE_URL=http://localhost:8080
export FLEXPRICE_API_KEY=your_api_key

# Send 50 events
benthos -c config-real-customer.yaml

# Or send 500 events in bulk
benthos -c config-real-bulk.yaml
```

