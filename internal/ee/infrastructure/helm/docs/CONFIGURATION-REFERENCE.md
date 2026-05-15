# Configuration Reference

The chart exposes everything the app needs through `values.yaml`. This
document groups the production-relevant keys by purpose. **For the full
inline documentation, read [`values.yaml`](../flexprice/values.yaml)
directly** — every key has a comment explaining when to set it.

## Image

```yaml
image:
  repository: ghcr.io/flexprice/flexprice
  tag: ""              # empty → resolves to .Chart.AppVersion
  pullPolicy: IfNotPresent

imagePullSecrets: []   # set if pulling from a private mirror
```

For private mirrors (regulated AWS accounts, air-gapped k8s):

```yaml
image:
  repository: 123456789012.dkr.ecr.us-east-1.amazonaws.com/flexprice
  tag: "v1.0.0"
imagePullSecrets:
  - name: my-ecr-pull-secret
```

The migration Job also uses three helper images (`postgres`, `busybox`,
`clickhouse-server`). Override their registries if needed:

```yaml
migration:
  images:
    postgres:   { registry: my-mirror.example.invalid, repository: postgres, tag: "15.3-alpine" }
    busybox:    { registry: my-mirror.example.invalid, repository: busybox,  tag: "1.36" }
    clickhouse: { registry: my-mirror.example.invalid, repository: clickhouse/clickhouse-server, tag: "24.8-alpine" }
```

## Workloads

Three Deployments, each independently configurable:

```yaml
api:        # HTTP API; receives ingress traffic
consumer:   # Kafka consumer; processes events_*, writes ClickHouse
worker:     # Temporal worker; runs billing/invoice workflows
```

Each accepts:

| Key                                  | Purpose                                                                 |
|--------------------------------------|-------------------------------------------------------------------------|
| `enabled`                            | Render the Deployment.                                                  |
| `replicaCount`                       | Static replica count (ignored if `autoscaling.enabled`).                |
| `resources.{requests,limits}`        | CPU/memory.                                                             |
| `autoscaling.{minReplicas,maxReplicas,targetCPUUtilizationPercentage}` | HPA. |
| `podDisruptionBudget.{enabled,minAvailable\|maxUnavailable}` | PDB.       |
| `strategy.{type,rollingUpdate.maxSurge,maxUnavailable}` | Rollout pacing. |
| `terminationGracePeriodSeconds`      | Graceful shutdown window.                                               |
| `preStop.{enabled,sleepSeconds}`     | Pre-stop sleep so endpoints controller drops the pod first.             |
| `nodeSelector`, `tolerations`, `affinity` | Per-component overrides on top of the global versions.            |
| `topologySpreadConstraints`          | Per-component override on top of the global list.                       |

The `consumer` also accepts a `keda` block:

```yaml
consumer:
  autoscaling:
    enabled: false        # turn off HPA when KEDA takes over
  keda:
    enabled: true
    minReplicas: 1
    maxReplicas: 10
    lagThreshold: 1000    # messages of consumer-group lag per replica
```

## Data services — choose your topology

For each backing service, the chart supports three modes:

1. **External (managed)** — preferred for production. Disable the bundled
   subchart, point at your managed endpoint.
2. **Bundled subchart** — fine for dev; **not** supported for prod (no
   backup, no HA story).
3. **Hand-rolled internal** — legacy, kept for the local dev path.

### PostgreSQL

```yaml
# External (recommended)
postgresql:
  enabled: false        # disable bundled bitnami subchart
postgres:
  external: { enabled: true }
  host: "rds.example.invalid"
  port: 5432
  user: flexprice
  database: flexprice
  sslmode: require      # or verify-full — see SECRETS.md
  maxOpenConns: 25
  maxIdleConns: 10
  connMaxLifetimeMinutes: 60
  readerHost: ""        # optional RDS reader endpoint
  readerPort: 5432
```

### ClickHouse

```yaml
clickhouse:
  mode: external        # standalone | altinity | external
  address: "ch.example.invalid:9440"
  tls: true
  username: default
  database: flexprice
  maxMemoryUsageGB: 90  # per-query bound
```

Other modes:
- `standalone` — single-replica StatefulSet rendered by the chart. Works
  out of the box, no operator. Fine for single-node prod, awkward for HA.
- `altinity` — `ClickHouseInstallation` CRD; needs the
  [Altinity operator](https://github.com/Altinity/clickhouse-operator)
  installed cluster-wide before `helm install`.

### Kafka

```yaml
kafka:
  enabled: false        # disable bundled bitnami subchart
kafkaConfig:
  external: { enabled: true }
  brokers: "b-1.kafka.example.invalid:9094,b-2...:9094"
  consumerGroup: flexprice-consumer
  topic: events
  topicLazy: events_lazy
  tls: true
  useSASL: true
  saslMechanism: "SCRAM-SHA-512"   # IAM not supported — see SECRETS.md
  saslUsername: flexprice
  clientId: flexprice-prod
```

### Redis

```yaml
redis:
  enabled: false        # disable bundled bitnami subchart
redisConfig:
  external: { enabled: true }
  host: "redis.example.invalid"
  port: 6379
  tls: true
redisExtended:
  clusterMode: true     # true for Redis Cluster / ElastiCache cluster-mode-enabled
  poolSize: 50
  keyPrefix: "flexprice:prod"
```

### Temporal

```yaml
temporal:
  enabled: false        # disable temporalio subchart
temporalConfig:
  external: { enabled: true }
  address: "ns.account.tmprl.cloud:7233"
  namespace: "ns.account"
  taskQueue: billing-task-queue
  tls: true
  bootstrapNamespaces: []   # Temporal Cloud — namespaces created out-of-band
```

For self-hosted Temporal, enable the bundled subchart instead:

```yaml
temporal:
  enabled: true
  server: { replicaCount: 3 }
temporalConfig:
  external: { enabled: false }
  bootstrapNamespaces:
    - name: default
      retention: 72h
```

## Secrets

```yaml
secrets:
  existingSecret: flexprice-secrets   # see docs/SECRETS.md
```

If unset, the chart renders its own Secret from plaintext values — **dev
only**.

## Ingress + TLS

```yaml
ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/proxy-body-size: "10m"
    nginx.ingress.kubernetes.io/limit-rps: "100"
  hosts:
    - host: api.flexprice.yourcompany.com
      paths: [{ path: /, pathType: Prefix }]
  tls:
    - secretName: flexprice-api-tls
      hosts: [api.flexprice.yourcompany.com]
```

Drop the Temporal Web ingress in prod (use external Temporal Cloud UI):

```yaml
temporalIngress:
  enabled: false
```

## Observability

```yaml
logging:
  level: info
  format: json
  otel:
    enabled: true
    endpoint: "otel-collector.observability.svc:4317"
    insecure: false
    auth:
      header: "signoz-ingestion-key"
      # value sourced from secrets.existingSecret key: logging-otel-auth-value
sentry:
  enabled: true
  environment: production
  sampleRate: 0.1
pyroscope:
  enabled: false        # opt-in
```

## Security

```yaml
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 1000
  runAsGroup: 3000
  fsGroup: 2000
  seccompProfile: { type: RuntimeDefault }

securityContext:
  allowPrivilegeEscalation: false
  privileged: false
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  capabilities: { drop: [ALL] }
  seccompProfile: { type: RuntimeDefault }

serviceAccount:
  create: true
  perComponent: false     # flip true for per-workload IRSA / Workload Identity
  components:
    api:      { annotations: { eks.amazonaws.com/role-arn: "arn:aws:iam::ACCT:role/flexprice-api" } }
    consumer: { annotations: { eks.amazonaws.com/role-arn: "arn:aws:iam::ACCT:role/flexprice-consumer" } }
    worker:   { annotations: { eks.amazonaws.com/role-arn: "arn:aws:iam::ACCT:role/flexprice-worker" } }
```

See [AWS-IAM.md](AWS-IAM.md) for the IAM policies each workload needs.

## NetworkPolicy

```yaml
networkPolicy:
  enabled: false   # opt-in; only renders an ingress allow for api when on
  ingress:
    fromIngressController:
      - namespaceSelector: { matchLabels: { kubernetes.io/metadata.name: ingress-nginx } }
        podSelector:       { matchLabels: { app.kubernetes.io/name: ingress-nginx } }
```

Egress is intentionally unrestricted. See chart commit history for the
reasoning (managed services rotate IPs; allow-lists are brittle).

## Migration Job

Runs as a Helm `pre-install` + `pre-upgrade` hook. All steps idempotent.

```yaml
migration:
  enabled: true
  timeout: 300                    # seconds per step
  activeDeadlineSeconds: 900      # whole-job timeout
  steps:
    postgresSetup: true           # schema + extensions
    clickhouse:    true
    kafka:         true           # topic creation
    ent:           true
    seed:          false          # seed default tenant/user — dev only
  images:
    postgres:   { registry: docker.io, repository: postgres,    tag: "15.3-alpine" }
    busybox:    { registry: docker.io, repository: busybox,     tag: "1.36" }
    clickhouse: { registry: docker.io, repository: clickhouse/clickhouse-server, tag: "24.8-alpine" }
```

## Per-platform reference values files

- [`values-local.yaml`](../values-local.yaml) — bundled subcharts, kind cluster, plaintext creds
- [`values-prod.example.yaml`](../values-prod.example.yaml) — external managed services, existingSecret only

## Anything else

Every key in [`values.yaml`](../flexprice/values.yaml) is commented. Search
the file for the feature you want; the comment explains when to set it.
