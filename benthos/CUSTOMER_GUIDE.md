# Benthos-Flexprice Customer Guide

## Overview

`benthos-flexprice` is a pre-built stream processor that forwards usage events from **your data sources** (Kafka, databases, APIs) to **Flexprice** for usage-based billing. No code changes required in your application.

## Use Case Example: Vapi (Voice Agent Company)

**Scenario:** Vapi tracks voice call usage in their Kafka topic and wants to send it to Flexprice for billing.

### Vapi's Current Events (Kafka)
```json
{
  "call_id": "call_123",
  "customer_id": "vapi_cust_456",
  "duration_seconds": 120,
  "model": "gpt-4",
  "timestamp": "2025-12-01T10:30:00Z"
}
```

### Flexprice Event Format (Required)
```json
{
  "event_name": "voice_call",
  "external_customer_id": "vapi_cust_456",
  "properties": {
    "duration_seconds": 120,
    "model": "gpt-4"
  },
  "source": "vapi-production",
  "timestamp": "2025-12-01T10:30:00Z"
}
```

---

## Setup Steps (5 Minutes)

### 1. Clone & Build

```bash
git clone https://github.com/flexprice/benthos-flexprice.git
cd benthos-flexprice
go build -o benthos-flexprice main.go
```

### 2. Create Config File

**`vapi-config.yaml`:**

```yaml
input:
  kafka:
    addresses: 
      - ${VAPI_KAFKA_BROKERS}
    topics:
      - vapi-usage-events
    consumer_group: vapi-to-flexprice
    start_from_oldest: false
    
    # Authentication (if needed)
    tls:
      enabled: true
    sasl:
      mechanism: PLAIN
      user: ${VAPI_KAFKA_USER}
      password: ${VAPI_KAFKA_PASSWORD}

pipeline:
  processors:
    - mapping: |
        # Transform Vapi events to Flexprice format
        root.event_name = "voice_call"
        root.external_customer_id = this.customer_id
        root.properties = {
          "duration_seconds": this.duration_seconds,
          "model": this.model,
          "call_id": this.call_id
        }
        root.source = "vapi-production"
        root.timestamp = this.timestamp

output:
  flexprice:
    api_host: ${FLEXPRICE_API_HOST}
    api_key: ${FLEXPRICE_API_KEY}
    scheme: https
```

### 3. Set Environment Variables

**`.env.production`:**

```bash
# Flexprice credentials (from Flexprice dashboard)
export FLEXPRICE_API_HOST=api.cloud.flexprice.io
export FLEXPRICE_API_KEY=fp_your_key_here

# Your Kafka cluster
export VAPI_KAFKA_BROKERS=kafka.vapi.ai:9092
export VAPI_KAFKA_USER=service_account
export VAPI_KAFKA_PASSWORD=xxx
```

### 4. Run the Collector

```bash
source .env.production
./benthos-flexprice -c vapi-config.yaml
```

**Expected Output:**
```
INFO Flexprice output connected and ready
INFO Input type kafka is now active
INFO 📤 Sending event: voice_call for customer: vapi_cust_456
INFO ✅ Event accepted successfully, ID: evt_xxx
```

---

## Production Deployment

### Docker

**Dockerfile:**
```dockerfile
FROM golang:1.21 AS builder
WORKDIR /app
COPY . .
RUN go build -o benthos-flexprice main.go

FROM debian:bookworm-slim
COPY --from=builder /app/benthos-flexprice /usr/local/bin/
COPY vapi-config.yaml /config/
CMD ["benthos-flexprice", "-c", "/config/vapi-config.yaml"]
```

**Run:**
```bash
docker build -t vapi/benthos-flexprice:latest .

docker run -d \
  --name vapi-flexprice-collector \
  -e FLEXPRICE_API_HOST=api.cloud.flexprice.io \
  -e FLEXPRICE_API_KEY=fp_xxx \
  -e VAPI_KAFKA_BROKERS=kafka.vapi.ai:9092 \
  -e VAPI_KAFKA_USER=xxx \
  -e VAPI_KAFKA_PASSWORD=xxx \
  vapi/benthos-flexprice:latest
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: benthos-flexprice
spec:
  replicas: 2
  template:
    spec:
      containers:
      - name: benthos
        image: vapi/benthos-flexprice:latest
        env:
        - name: FLEXPRICE_API_HOST
          value: api.cloud.flexprice.io
        - name: FLEXPRICE_API_KEY
          valueFrom:
            secretKeyRef:
              name: flexprice-secrets
              key: api-key
        - name: VAPI_KAFKA_BROKERS
          value: kafka.vapi.ai:9092
        - name: VAPI_KAFKA_USER
          valueFrom:
            secretKeyRef:
              name: kafka-secrets
              key: username
        - name: VAPI_KAFKA_PASSWORD
          valueFrom:
            secretKeyRef:
              name: kafka-secrets
              key: password
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

---

## Data Flow

```
┌─────────────────┐
│  Your Kafka     │  Your event format
│  vapi-usage-    │  (duration_seconds, customer_id, etc.)
│     events      │
└────────┬────────┘
         │
         v
┌─────────────────┐
│    Benthos      │  Transform with Bloblang:
│  (Transform)    │  - Map your fields → Flexprice fields
│                 │  - No code changes needed
└────────┬────────┘
         │
         v
┌─────────────────┐
│   Flexprice     │  POST /v1/events
│      API        │  (Standard Flexprice format)
└─────────────────┘
         │
         v
┌─────────────────┐
│  Flexprice UI   │  Usage tracked automatically:
│                 │  - Real-time meters
│                 │  - Per-customer usage
│                 │  - Automated invoicing
└─────────────────┘
```

---

## Prerequisites in Flexprice

Before running the collector, set up in your Flexprice account:

1. **Create Meter**
   - Name: `voice_call`
   - Event name: `voice_call`
   - Aggregation: `SUM` of `duration_seconds`

2. **Import Customers**
   - Ensure `external_customer_id` in events matches your customer IDs in Flexprice

3. **Generate API Key**
   - Go to Settings → API Keys
   - Create key with `events:write` permission

---

## Key Features

- ✅ **No Code Changes** - Just config and deploy
- ✅ **Reliable** - Built-in retries and error handling
- ✅ **Scalable** - Run multiple replicas for high throughput
- ✅ **Flexible** - Transform any event format with Bloblang
- ✅ **Observable** - Prometheus metrics at `:4195/metrics`
- ✅ **Production-Ready** - Battle-tested Benthos core + Flexprice SDK

---

## Event Format Requirements

| Field | Required | Description | Example |
|-------|----------|-------------|---------|
| `event_name` | ✅ Yes | Must match Flexprice meter name | `"voice_call"` |
| `external_customer_id` | ✅ Yes | Your customer identifier | `"vapi_cust_456"` |
| `properties` | Optional | Event metadata (use numbers for SUM/COUNT) | `{"duration_seconds": 120}` |
| `timestamp` | Optional | Event time (defaults to now) | `"2025-12-01T10:30:00Z"` |
| `source` | Optional | Event source tracking | `"vapi-production"` |

**Important:** If your meter aggregates a property (e.g., `SUM` of `duration_seconds`), send it as a **number**, not a string.

---

## Monitoring & Observability

Benthos exposes metrics on port **4195**:

- **Prometheus Metrics**: `http://localhost:4195/metrics`
- **Health Check**: `http://localhost:4195/ping`
- **Runtime Stats**: `http://localhost:4195/stats`

### Key Metrics to Monitor

```
benthos_input_received_total          # Events received from Kafka
benthos_output_sent_total             # Events sent to Flexprice
benthos_output_error_total            # Failed sends
benthos_processor_error_total         # Transform errors
```

---

## Error Handling

| Status Code | Behavior |
|-------------|----------|
| **202 Accepted** | ✅ Success - event accepted |
| **400 Bad Request** | ❌ Dropped - validation error (logged) |
| **401/403 Auth Error** | 🛑 Stop - invalid API key |
| **429 Rate Limited** | 🔄 Retry with exponential backoff |
| **5xx Server Error** | 🔄 Retry with exponential backoff |
| **Network Error** | 🔄 Retry with exponential backoff |

---

## Troubleshooting

### Events not appearing in Flexprice

1. **Check event name matches meter:**
   ```bash
   # Look in Benthos logs:
   INFO 📤 Sending event: voice_call for customer: vapi_cust_456
   ```

2. **Verify customer exists:**
   - `external_customer_id` must match a customer in Flexprice

3. **Check property types:**
   ```json
   // ✅ Correct (number)
   {"duration_seconds": 120}
   
   // ❌ Wrong (string)
   {"duration_seconds": "120"}
   ```

4. **Check API response:**
   ```bash
   # Success:
   INFO ✅ Event accepted successfully, ID: evt_xxx
   
   # Error:
   ERROR Failed to send event: 400 Bad Request: {"error": "..."}
   ```

### Kafka connection issues

- **Verify brokers**: `echo $VAPI_KAFKA_BROKERS`
- **Test credentials**: Use `kafka-console-consumer` to verify access
- **Check TLS/SASL**: Ensure config matches your Kafka cluster auth

### High memory usage

- Reduce `max_processing_period` in Kafka config
- Lower `commit_period` to commit offsets more frequently
- Scale horizontally (more replicas) instead of vertically

---

## Support

- **Documentation**: [docs.flexprice.io](https://docs.flexprice.io)
- **GitHub Issues**: [github.com/flexprice/benthos-flexprice/issues](https://github.com/flexprice/benthos-flexprice/issues)
- **Benthos Docs**: [benthos.dev](https://www.benthos.dev/)
- **Contact**: support@flexprice.io

---

## Quick Start Checklist

- [ ] Clone repository and build binary
- [ ] Create meter in Flexprice (with matching `event_name`)
- [ ] Import customers to Flexprice
- [ ] Generate API key in Flexprice
- [ ] Create config file mapping your events → Flexprice format
- [ ] Set environment variables (API key, Kafka credentials)
- [ ] Test locally with `./benthos-flexprice -c your-config.yaml`
- [ ] Deploy to production (Docker/K8s)
- [ ] Monitor metrics at `:4195/metrics`
- [ ] Verify events in Flexprice UI

---

**Ready to get started?** Follow the setup steps above and you'll be streaming usage events to Flexprice in minutes! 🚀

