# Flexprice Event Collector - Customer Guide

Stream usage events from Kafka/Postgres/APIs to Flexprice for usage-based billing. No code changes required.

> Built on [Bento](https://github.com/warpstreamlabs/bento) - Open source stream processor (MIT license)

---

## Quick Start (5 Minutes)

### 1. Download Binary

```bash
# Download from GitHub releases
curl -L https://github.com/flexprice/flexprice/releases/latest/download/bento-flexprice-linux -o bento-flexprice
chmod +x bento-flexprice
```

### 2. Create Config File

**Example: Vapi (Voice AI company) streaming call usage from Kafka**

**`config.yaml`:**
```yaml
input:
  kafka:
    addresses: ["${KAFKA_BROKERS}"]
    topics: ["usage-events"]
    consumer_group: "flexprice-collector"
    
    # Auth (Confluent Cloud / AWS MSK)
    tls:
      enabled: true
    sasl:
      mechanism: PLAIN
      user: "${KAFKA_USER}"
      password: "${KAFKA_PASSWORD}"

pipeline:
  processors:
    # Transform your events → Flexprice format
    - mapping: |
        root.event_name = "voice_call"
        root.external_customer_id = this.customer_id
        root.properties = {
          "duration_seconds": this.duration_seconds.string(),
          "model": this.model
        }
        root.timestamp = this.timestamp
        root.source = "production"

output:
  flexprice:
    api_host: "${FLEXPRICE_API_HOST}"
    api_key: "${FLEXPRICE_API_KEY}"
    scheme: https
    
    # Bulk event batching (10x fewer API calls)
    batching:
      count: 100       # Send when 100 events accumulated
      period: 5s       # Or after 5 seconds
    
    max_in_flight: 10  # Concurrent requests

logger:
  level: INFO
```

### 3. Set Environment Variables

```bash
# Flexprice (from dashboard → Settings → API Keys)
export FLEXPRICE_API_HOST=api.cloud.flexprice.io
export FLEXPRICE_API_KEY=fp_live_xxxxx

# Your Kafka cluster
export KAFKA_BROKERS=pkc-xxxxx.aws.confluent.cloud:9092
export KAFKA_USER=your-api-key
export KAFKA_PASSWORD=your-api-secret
```

### 4. Run

```bash
./bento-flexprice -c config.yaml
```

**Expected Output:**
```
INFO Flexprice output connected and ready
INFO Input type kafka is now active
INFO 📦 Sending bulk batch: 100 events
INFO ✅ Bulk batch accepted successfully: 100 events processed
```

---

## Production Deployment

### Docker

```dockerfile
FROM debian:bookworm-slim
COPY bento-flexprice /usr/local/bin/
COPY config.yaml /etc/bento/
CMD ["bento-flexprice", "-c", "/etc/bento/config.yaml"]
```

```bash
docker run -d \
  --name flexprice-collector \
  --restart unless-stopped \
  -e FLEXPRICE_API_HOST=api.cloud.flexprice.io \
  -e FLEXPRICE_API_KEY=fp_live_xxx \
  -e KAFKA_BROKERS=your-kafka:9092 \
  -e KAFKA_USER=xxx \
  -e KAFKA_PASSWORD=xxx \
  your-org/flexprice-collector:latest
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: flexprice-collector
spec:
  replicas: 2  # Scale horizontally
  template:
    spec:
      containers:
      - name: bento
        image: your-org/flexprice-collector:latest
        env:
        - name: FLEXPRICE_API_KEY
          valueFrom:
            secretKeyRef:
              name: flexprice
              key: api-key
        - name: KAFKA_BROKERS
          value: "kafka:9092"
        resources:
          requests:
            memory: "256Mi"
            cpu: "200m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

---

## Before You Start

### In Flexprice Dashboard:

1. **Create Meter**
   - Name: `voice_call` (must match `event_name` in config)
   - Aggregation: `SUM` of `duration_seconds`

2. **Import Customers**
   - Ensure `external_customer_id` matches your IDs

3. **Generate API Key**
   - Settings → API Keys → Create Key

---

## Event Format

Your events are transformed to Flexprice format:

```yaml
# Your Kafka event:
{
  "customer_id": "cust_123",
  "duration_seconds": 120,
  "model": "gpt-4"
}

# Transformed to Flexprice:
{
  "event_name": "voice_call",           # Required
  "external_customer_id": "cust_123",   # Required
  "properties": {
    "duration_seconds": "120",          # For SUM aggregation
    "model": "gpt-4"
  },
  "timestamp": "2025-12-02T10:00:00Z",  # Optional
  "source": "production"                # Optional
}
```

**⚠️ Important:** Convert numeric properties to strings using `.string()` in mapping - Flexprice API converts them back for aggregation.

---

## Data Sources

### Kafka (shown above)

### PostgreSQL CDC

```yaml
input:
  sql_select:
    driver: postgres
    dsn: "postgres://user:pass@localhost/db"
    table: "usage_events"
    columns: ["*"]
    where: "created_at > $1"
    args_mapping: "root = [timestamp_unix()]"
```

### HTTP Webhook

```yaml
input:
  http_server:
    address: "0.0.0.0:8080"
    path: "/webhook"
```

### File/S3

```yaml
input:
  aws_s3:
    bucket: "usage-logs"
    prefix: "events/"
```

[See all 200+ inputs](https://warpstreamlabs.github.io/bento/docs/components/inputs/about)

---

## Monitoring

Metrics available at `http://localhost:4195/metrics` (Prometheus format):

```
# Key metrics
bento_input_received_total     # Events from Kafka
bento_output_sent_total        # Events to Flexprice  
bento_output_batch_sent_total  # Bulk batches sent
bento_output_error_total       # Failed sends
```

**Health check:** `http://localhost:4195/ping`

---

## Bulk Event Batching

The collector automatically batches events for 10-100x better performance:

```yaml
batching:
  count: 100    # Batch size
  period: 5s    # Max wait time
```

**Benefits:**
- 1000 events = 10 API calls (instead of 1000)
- Lower latency and costs
- Automatic optimization (1 event = single API call)

---

## Error Handling

| Status | Behavior |
|--------|----------|
| 202 | ✅ Success |
| 400 | ❌ Dropped (logged) |
| 401/403 | 🛑 Fatal (bad API key) |
| 429/5xx | 🔄 Retry with backoff |

---

## Troubleshooting

### Events not showing in Flexprice?

1. **Check event name:** Must match meter name exactly
   ```
   INFO 📤 Sending event: voice_call for customer: cust_123
   ```

2. **Verify customer exists:** `external_customer_id` must be in Flexprice

3. **Check logs for errors:**
   ```
   ERROR Failed to send event: 400 Bad Request
   ```

### Kafka not connecting?

```bash
# Test credentials
echo $KAFKA_BROKERS
echo $KAFKA_USER

# Check TLS/SASL settings match your cluster
```

### High memory usage?

- Scale horizontally (more replicas)
- Reduce batch size
- Add `fetch_buffer_cap: 256` to Kafka config

---

## Production Checklist

- [ ] Download/build `bento-flexprice` binary
- [ ] Create meter in Flexprice (matching `event_name`)
- [ ] Import customers to Flexprice
- [ ] Generate API key
- [ ] Create config file with event mapping
- [ ] Test locally with sample events
- [ ] Deploy to production (Docker/K8s)
- [ ] Set up monitoring (`/metrics` endpoint)
- [ ] Verify events in Flexprice dashboard

---

## Example Companies Using This

- **Vapi** - Voice AI call duration tracking
- **Modal** - GPU compute time tracking  
- **Liveblocks** - Collaborative editing events
- **Incident.io** - API usage tracking

---

## Support

- **Docs:** [docs.flexprice.io](https://docs.flexprice.io)
- **Bento Docs:** [warpstreamlabs.github.io/bento](https://warpstreamlabs.github.io/bento)
- **Issues:** [github.com/flexprice/flexprice/issues](https://github.com/flexprice/flexprice/issues)
- **Email:** support@flexprice.io

---

**Ready in 5 minutes.** Download, configure, deploy. 🚀
