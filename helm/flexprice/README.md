# FlexPrice Helm Chart

This Helm chart deploys the FlexPrice billing and pricing platform on a Kubernetes cluster.

## Prerequisites

- Kubernetes 1.24+
- Helm 3.0+

### External Services (Optional)

You can either use external services or deploy them internally with the chart:

- **PostgreSQL**: Managed database service (RDS, Cloud SQL, etc.) or internal deployment
- **ClickHouse**: Managed database service or internal deployment
- **Kafka**: Managed Kafka service (MSK, Confluent Cloud, etc.) or internal deployment
- **Temporal**: Managed Temporal service or internal deployment

**Note**: For production environments, it's recommended to use external managed services for better reliability and scalability.

## Installation

### Install with default values

```bash
helm install flexprice ./helm/flexprice
```

### Install with custom values

```bash
helm install flexprice ./helm/flexprice -f custom-values.yaml
```

### Install with secrets from a file

Create a `secrets.yaml` file with your sensitive values:

```yaml
postgres:
  password: "your-postgres-password"

clickhouse:
  password: "your-clickhouse-password"

auth:
  secret: "your-auth-secret"

secrets:
  encryptionKey: "your-encryption-key"
```

Then install:

```bash
helm install flexprice ./helm/flexprice -f values.yaml -f secrets.yaml
```

## Configuration

The following table lists the configurable parameters and their default values.

### Global Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Image repository | `flexprice-app` |
| `image.tag` | Image tag | `latest` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `replicaCount` | Number of replicas | `2` |

### API Deployment

| Parameter | Description | Default |
|-----------|-------------|---------|
| `api.enabled` | Enable API deployment | `true` |
| `api.replicaCount` | Number of API replicas | `2` |
| `api.resources` | Resource requests/limits | See values.yaml |
| `api.autoscaling.enabled` | Enable HPA | `true` |
| `api.podDisruptionBudget.enabled` | Enable PDB | `true` |

### Consumer Deployment

| Parameter | Description | Default |
|-----------|-------------|---------|
| `consumer.enabled` | Enable consumer deployment | `true` |
| `consumer.replicaCount` | Number of consumer replicas | `2` |
| `consumer.resources` | Resource requests/limits | See values.yaml |
| `consumer.autoscaling.enabled` | Enable HPA | `true` |

### Worker Deployment

| Parameter | Description | Default |
|-----------|-------------|---------|
| `worker.enabled` | Enable worker deployment | `true` |
| `worker.replicaCount` | Number of worker replicas | `2` |
| `worker.resources` | Resource requests/limits | See values.yaml |
| `worker.autoscaling.enabled` | Enable HPA | `false` |

### Service Configuration (External vs Internal)

The chart supports both external managed services and internal deployments. Use `{service}.external.enabled=false` to deploy services internally.

#### PostgreSQL Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `postgres.external.enabled` | Use external PostgreSQL | `true` |
| `postgres.host` | PostgreSQL host (external) | `postgres-service` |
| `postgres.port` | PostgreSQL port | `5432` |
| `postgres.user` | PostgreSQL user | `flexprice` |
| `postgres.password` | PostgreSQL password | **(required)** |
| `postgres.dbname` | PostgreSQL database name | `flexprice` |
| `postgres.sslmode` | SSL mode (external only) | `require` |
| `postgres.internal.image.repository` | Internal PostgreSQL image | `postgres` |
| `postgres.internal.image.tag` | Internal PostgreSQL tag | `15.3` |
| `postgres.internal.persistence.enabled` | Enable persistent storage | `true` |
| `postgres.internal.persistence.size` | Storage size | `20Gi` |

#### ClickHouse Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `clickhouse.external.enabled` | Use external ClickHouse | `true` |
| `clickhouse.address` | ClickHouse address (external) | `clickhouse-service:9000` |
| `clickhouse.username` | ClickHouse username | `flexprice` |
| `clickhouse.password` | ClickHouse password | **(required)** |
| `clickhouse.database` | ClickHouse database | `flexprice` |
| `clickhouse.internal.image.repository` | Internal ClickHouse image | `clickhouse/clickhouse-server` |
| `clickhouse.internal.image.tag` | Internal ClickHouse tag | `24.9-alpine` |
| `clickhouse.internal.persistence.enabled` | Enable persistent storage | `true` |
| `clickhouse.internal.persistence.size` | Storage size | `50Gi` |

#### Kafka Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `kafka.external.enabled` | Use external Kafka | `true` |
| `kafka.brokers` | Kafka broker addresses (external) | `["kafka-service:9092"]` |
| `kafka.consumerGroup` | Kafka consumer group | `flexprice-consumer` |
| `kafka.topic` | Kafka topic for events | `events` |
| `kafka.topicLazy` | Kafka topic for lazy events | `events_lazy` |
| `kafka.internal.image.repository` | Internal Kafka image | `confluentinc/cp-kafka` |
| `kafka.internal.image.tag` | Internal Kafka tag | `7.7.1` |
| `kafka.internal.persistence.enabled` | Enable persistent storage | `true` |
| `kafka.internal.persistence.size` | Storage size | `20Gi` |

#### Temporal Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `temporal.external.enabled` | Use external Temporal | `true` |
| `temporal.address` | Temporal server address (external) | `temporal-service:7233` |
| `temporal.taskQueue` | Temporal task queue | `billing-task-queue` |
| `temporal.namespace` | Temporal namespace | `default` |
| `temporal.internal.server.image.repository` | Internal Temporal image | `temporalio/auto-setup` |
| `temporal.internal.server.image.tag` | Internal Temporal tag | `1.26.2` |
| `temporal.internal.ui.enabled` | Enable Temporal UI | `true` |

### Ingress Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `nginx` |
| `ingress.hosts` | Ingress host configuration | See values.yaml |
| `ingress.tls` | TLS configuration | `[]` |

## Deployment Modes

The FlexPrice application supports three deployment modes:

1. **API Mode** (`deployment.mode: "api"`): Runs the REST API server
2. **Consumer Mode** (`deployment.mode: "consumer"`): Runs Kafka consumers for event processing
3. **Worker Mode** (`deployment.mode: "temporal_worker"`): Runs Temporal workers for workflow execution

You can enable/disable each component independently using the `api.enabled`, `consumer.enabled`, and `worker.enabled` flags.

## Scaling

The chart supports Horizontal Pod Autoscaling (HPA) for all three components:

- **API**: Autoscaling enabled by default (2-10 replicas)
- **Consumer**: Autoscaling enabled by default (2-5 replicas)
- **Worker**: Autoscaling disabled by default (can be enabled)

## Security

### Secrets Management

All sensitive configuration should be provided via Kubernetes secrets. The chart creates a secret resource, but you must provide the actual values:

- `postgres-password`: PostgreSQL database password
- `clickhouse-password`: ClickHouse database password
- `auth-secret`: Authentication secret key
- `encryption-key`: Encryption key for secrets storage
- `sentry-dsn`: Sentry DSN (if Sentry is enabled)
- `email-resend-api-key`: Resend API key (if email is enabled)

### Security Context

The chart includes security best practices:

- Runs as non-root user
- Read-only root filesystem (can be disabled)
- Drop all capabilities
- No privilege escalation

## Monitoring

### Health Checks

All deployments include:

- **Liveness probe**: Checks `/health` endpoint
- **Readiness probe**: Checks `/health` endpoint
- **Startup probe**: Ensures graceful startup

### Observability

The chart supports integration with:

- **Sentry**: Error tracking and monitoring
- **Pyroscope**: Continuous profiling
- **Prometheus**: Metrics via ServiceMonitor (if enabled)

## Examples

### Using External Services

```yaml
# values-external.yaml
postgres:
  external:
    enabled: true
  host: "my-rds-instance.region.rds.amazonaws.com"
  port: 5432
  sslmode: "require"

clickhouse:
  external:
    enabled: true
  address: "clickhouse-managed.example.com:9000"

kafka:
  external:
    enabled: true
  brokers:
    - "kafka-cluster.example.com:9092"

temporal:
  external:
    enabled: true
  address: "temporal-managed.example.com:7233"
```

### Using Internal Services

```yaml
# values-internal.yaml
postgres:
  external:
    enabled: false
  internal:
    persistence:
      size: 50Gi
      storageClass: "fast-ssd"

clickhouse:
  external:
    enabled: false
  internal:
    persistence:
      size: 100Gi

kafka:
  external:
    enabled: false
  internal:
    persistence:
      size: 50Gi

temporal:
  external:
    enabled: false
  internal:
    ui:
      enabled: true
```

### Production Deployment

```yaml
# values-production.yaml
api:
  replicaCount: 3
  autoscaling:
    minReplicas: 3
    maxReplicas: 20

consumer:
  replicaCount: 3
  autoscaling:
    minReplicas: 3
    maxReplicas: 10

worker:
  replicaCount: 2

ingress:
  enabled: true
  className: "nginx"
  hosts:
    - host: api.flexprice.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: flexprice-tls
      hosts:
        - api.flexprice.example.com

sentry:
  enabled: true
  environment: "production"

pyroscope:
  enabled: true
  serverAddress: "https://pyroscope.example.com"
```

```bash
helm install flexprice ./helm/flexprice -f values-production.yaml
```

### Development Deployment

```yaml
# values-dev.yaml
api:
  replicaCount: 1
  autoscaling:
    enabled: false

consumer:
  replicaCount: 1
  autoscaling:
    enabled: false

worker:
  replicaCount: 1
  autoscaling:
    enabled: false

logging:
  level: "debug"
```

```bash
helm install flexprice-dev ./helm/flexprice -f values-dev.yaml
```

## Troubleshooting

### View Pod Logs

```bash
# API logs
kubectl logs -l app.kubernetes.io/component=api --tail=100

# Consumer logs
kubectl logs -l app.kubernetes.io/component=consumer --tail=100

# Worker logs
kubectl logs -l app.kubernetes.io/component=worker --tail=100
```

### Check Pod Status

```bash
kubectl get pods -l app.kubernetes.io/name=flexprice
```

### Check Service Status

```bash
kubectl get svc -l app.kubernetes.io/name=flexprice
```

### Access Pod Shell

```bash
kubectl exec -it <pod-name> -- /bin/sh
```

## Upgrading

```bash
helm upgrade flexprice ./helm/flexprice -f values.yaml -f secrets.yaml
```

## Uninstalling

```bash
helm uninstall flexprice
```

## Support

For issues and questions, please open an issue on the [FlexPrice GitHub repository](https://github.com/flexprice/flexprice).

