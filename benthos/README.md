# Bento Flexprice Collector

Custom Bento distribution for streaming usage events to Flexprice from any data source (Kafka, databases, APIs, etc.).

> **Note**: This project uses [Bento](https://github.com/warpstreamlabs/bento), an open-source stream processor (MIT license), ensuring complete freedom without vendor restrictions.

## Features

- ✅ **Flexprice Output Plugin** - Uses official Flexprice Go SDK
- ✅ **Kafka Consumer** - Stream from Kafka (with SASL/TLS support)
- ✅ **Event Generator** - Create synthetic events for testing
- ✅ **Bloblang Transforms** - Transform events on-the-fly
- ✅ **Docker Ready** - Production-ready container

## Quick Start

### 1. Build the Binary

```bash
cd bento
go build -o bento-flexprice main.go
```

### 2. Set Environment Variables

```bash
export FLEXPRICE_API_HOST=api.cloud.flexprice.io
export FLEXPRICE_API_KEY=your_api_key_here
```

### 3. Run an Example

```bash
# Generate random events (for testing)
./bento-flexprice -c examples/generate.yaml

# Consume from Kafka
./bento-flexprice -c examples/kafka-consumer.yaml
```

## Event Format

Events must be JSON with these fields:

```json
{
  "event_name": "feature 1",               // Required: meter name
  "external_customer_id": "cust_123",      // Required: customer ID
  "properties": {                          // Optional: event data
    "feature 1": 100,                      // Use numbers for aggregation
    "metadata": "value"
  },
  "timestamp": "2025-12-01T10:30:00Z",     // Optional: defaults to now
  "source": "kafka-stream",                // Optional: event source
  "event_id": "evt_123"                    // Optional: unique ID
}
```

**⚠️ SDK Note:** Due to SDK limitations, numeric property values must be converted to strings in your Bloblang transform using `"%v".format(this.value)` - the API will convert them back to numbers for aggregation.

## Configuration

### Flexprice Output

```yaml
output:
  flexprice:
    api_host: ${FLEXPRICE_API_HOST}     # api.cloud.flexprice.io
    api_key: ${FLEXPRICE_API_KEY}       # Your API key
    scheme: https                        # http or https
```

### Kafka Input (Local)

```yaml
input:
  kafka:
    addresses: 
      - localhost:29092
    topics:
      - events
    consumer_group: bento-flexprice
    start_from_oldest: true
```

### Kafka Input (Confluent Cloud)

```yaml
input:
  kafka:
    addresses: 
      - ${KAFKA_BROKERS}
    topics:
      - your-topic
    consumer_group: bento-flexprice
    start_from_oldest: false
    
    # Enable TLS
    tls:
      enabled: true
    
    # SASL authentication
    sasl:
      mechanism: PLAIN
      user: ${KAFKA_SASL_USER}
      password: ${KAFKA_SASL_PASSWORD}
```

## Real-World Example: Staging Kafka

Here's how to stream from a production Kafka to Flexprice:

**1. Create `.env.staging` file:**

```bash
# Flexprice
export FLEXPRICE_API_HOST=api.cloud.flexprice.io
export FLEXPRICE_API_KEY=fp_xxx

# Kafka (Confluent Cloud)
export FLEXPRICE_KAFKA_BROKERS=pkc-xxx.us-east-1.aws.confluent.cloud:9092
export FLEXPRICE_KAFKA_SASL_USER=your_user
export FLEXPRICE_KAFKA_SASL_PASSWORD=your_password
export FLEXPRICE_KAFKA_CONSUMER_GROUP=bento-flexprice-prod
```

**2. Load and run:**

```bash
source .env.staging
./bento-flexprice -c examples/kafka-staging.yaml
```

**3. Send test events:**

```bash
# Build the event sender
go build -o send-events send-events.go

# Send 10 events to Kafka
./send-events 10
```

**Expected logs:**

```
INFO[...] Flexprice output connected and ready
INFO[...] Input type kafka is now active
INFO[...] [KAFKA→FLEXPRICE] Processing event:
- Event Name: feature 1
- Customer ID: cust_01KB01JF360SNFB2EX7KRFHX0N
INFO[...] 📤 Sending event: feature 1 for customer: cust_...
INFO[...] ✅ Event accepted successfully, ID: real-test-1764619426582
```

## Error Handling

| Status Code | Behavior |
|-------------|----------|
| **202 Accepted** | ✅ Success - event accepted |
| **400 Bad Request** | ❌ Dropped - validation error |
| **401/403 Unauthorized** | 🛑 Stop - auth failed |
| **429 Rate Limited** | 🔄 Retry with backoff |
| **5xx Server Error** | 🔄 Retry with backoff |
| **Network Error** | 🔄 Retry with backoff |

## Bloblang Transformations

Transform events before sending to Flexprice:

```yaml
pipeline:
  processors:
    - mapping: |
        # Map your format to Flexprice format
        root.event_name = this.eventType
        root.external_customer_id = this.userId
        
        # Convert ALL property values to strings for SDK compatibility
        root.properties = this.data.map_each(kv -> {
          kv.key: "%v".format(kv.value)
        })
        
        # Add source tracking
        root.source = "my-service"
        
        # Use current timestamp if missing
        root.timestamp = this.timestamp.or(now().format_timestamp("2006-01-02T15:04:05Z07:00"))
```

## Testing

### Test 1: Generate Random Events

```bash
./bento-flexprice -c examples/generate.yaml
```

Generates 100 random events and sends to Flexprice.

### Test 2: Kafka → Flexprice

**Start local Kafka (from repo root):**

```bash
docker-compose up -d kafka
```

**Send test event:**

```bash
docker exec -it flexprice-kafka-1 kafka-console-producer \
  --broker-list localhost:9092 \
  --topic events

# Paste this (then Ctrl+D):
{"event_name":"api.request","external_customer_id":"cust_123","properties":{"endpoint":"/api/users","count":5}}
```

**Run Bento:**

```bash
export KAFKA_BROKER=localhost:29092
export KAFKA_TOPIC=events
./bento-flexprice -c examples/kafka-consumer.yaml
```

### Test 3: Docker

```bash
docker build -t bento-flexprice .

docker run --rm \
  -e FLEXPRICE_API_HOST=api.cloud.flexprice.io \
  -e FLEXPRICE_API_KEY=your_key \
  bento-flexprice:latest
```

## Monitoring

Bento exposes metrics on port **4195**:

- **Metrics**: `http://localhost:4195/metrics` (Prometheus)
- **Health**: `http://localhost:4195/ping`
- **Stats**: `http://localhost:4195/stats`

## Troubleshooting

### Events not showing in Flexprice UI

**Check 1: Event name matches meter**

```bash
# In Bento logs, look for:
INFO[...] 📤 Sending event: feature 1 for customer: cust_...
```

The `event_name` must match a meter's `event_name` in Flexprice.

**Check 2: Properties are numbers (for aggregation)**

If your meter does `SUM` of a property, send it as a number:

```json
{
  "event_name": "feature 1",
  "properties": {
    "feature 1": 100    // ✅ Number (not "100")
  }
}
```

**Check 3: Customer exists**

The `external_customer_id` must match an existing customer in Flexprice.

**Check 4: API response**

Look for errors in logs:

```bash
# Success:
INFO[...] ✅ Event accepted successfully, ID: evt_xxx

# Failure:
ERROR[...] Failed to send event: 400 Bad Request
```

### Kafka not connecting

**Local Kafka:**
- Use `localhost:29092` (from host) or `kafka:9092` (from Docker)
- Check topic exists: `kafka-topics --list --bootstrap-server localhost:29092`

**Confluent Cloud:**
- Verify `FLEXPRICE_KAFKA_BROKERS` has the full hostname with `:9092`
- Check SASL credentials are correct
- Ensure TLS is enabled in config

### Build errors

```bash
# Clean and rebuild
go mod tidy
go build -o bento-flexprice main.go
```

## Project Structure

```
bento/
├── main.go                    # Entry point (imports custom plugin)
├── output/
│   └── flexprice.go          # Custom Flexprice output plugin
├── examples/
│   ├── generate.yaml         # Random event generator
│   ├── kafka-consumer.yaml   # Local Kafka example
│   └── kafka-staging.yaml    # Production Kafka (SASL+TLS)
├── send-events.go            # Kafka event sender (for testing)
├── Dockerfile                # Production container
├── env.example               # Environment template
└── README.md                 # This file
```

## How It Works

```
┌───────────┐
│   Kafka   │  Your event source
│  (or any  │  (database, API, file, etc.)
│   input)  │
└─────┬─────┘
      │
      v
┌───────────┐
│ Bloblang  │  Transform to Flexprice format
│ Transform │  (optional)
└─────┬─────┘
      │
      v
┌───────────┐
│ Flexprice │  Custom output plugin
│  Output   │  (uses Go SDK)
└─────┬─────┘
      │
      v
┌───────────┐
│ Flexprice │  Usage data appears in UI
│    API    │
└───────────┘
```

## Environment Variables Reference

| Variable | Description | Example |
|----------|-------------|---------|
| `FLEXPRICE_API_HOST` | Flexprice API host | `api.cloud.flexprice.io` |
| `FLEXPRICE_API_KEY` | API key | `fp_xxx` |
| `KAFKA_BROKER` | Kafka broker (local) | `localhost:29092` |
| `KAFKA_TOPIC` | Topic name | `events` |
| `FLEXPRICE_KAFKA_BROKERS` | Kafka brokers (cloud) | `pkc-xxx.aws.confluent.cloud:9092` |
| `FLEXPRICE_KAFKA_SASL_USER` | SASL username | From Confluent Cloud |
| `FLEXPRICE_KAFKA_SASL_PASSWORD` | SASL password | From Confluent Cloud |

## Support

- **Documentation**: [docs.flexprice.io](https://docs.flexprice.io)
- **Issues**: [GitHub Issues](https://github.com/flexprice/flexprice/issues)
- **Bento Docs**: [warpstreamlabs.github.io/bento](https://warpstreamlabs.github.io/bento/)

## Related

- [Bento](https://github.com/warpstreamlabs/bento) - Open source stream processor (MIT license)
- [OpenMeter Bento Plugin](https://github.com/openmeterio/openmeter) - Inspiration
- [Flexprice](https://flexprice.io) - Usage-based billing
