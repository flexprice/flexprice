# FlexPrice Helm Chart

This Helm chart deploys the FlexPrice billing and pricing platform on a Kubernetes cluster.

## Prerequisites

- Kubernetes 1.24+
- Helm 3.0+
- PostgreSQL database (or external managed PostgreSQL service)
- ClickHouse database (or external managed ClickHouse service)
- Kafka cluster (or external managed Kafka service)
- Temporal service (or external managed Temporal service)

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

### Database Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `postgres.host` | PostgreSQL host | `postgres-service` |
| `postgres.port` | PostgreSQL port | `5432` |
| `postgres.user` | PostgreSQL user | `flexprice` |
| `postgres.password` | PostgreSQL password | **(required)** |
| `postgres.dbname` | PostgreSQL database name | `flexprice` |
| `clickhouse.address` | ClickHouse address | `clickhouse-service:9000` |
| `clickhouse.username` | ClickHouse username | `flexprice` |
| `clickhouse.password` | ClickHouse password | **(required)** |

### Kafka Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `kafka.brokers` | Kafka broker addresses | `["kafka-service:9092"]` |
| `kafka.consumerGroup` | Kafka consumer group | `flexprice-consumer` |
| `kafka.topic` | Kafka topic for events | `events` |
| `kafka.topicLazy` | Kafka topic for lazy events | `events_lazy` |

### Temporal Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `temporal.address` | Temporal server address | `temporal-service:7233` |
| `temporal.taskQueue` | Temporal task queue | `billing-task-queue` |
| `temporal.namespace` | Temporal namespace | `default` |

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

