# FlexPrice Helm Chart

Deploy the full FlexPrice billing platform on any Kubernetes cluster with a single `helm install`.

## Architecture

```
Internet
    │
    ▼
 Ingress (nginx)
    │
    ▼
 API Service  ─────────────────────────────────────────────┐
    │                                                       │
    ├── PostgreSQL  (auth, billing, subscriptions)         │
    ├── Redis       (caching, rate limiting)               │
    └── Kafka       (publish events)                       │
                         │                                 │
                         ▼                                 │
                    Consumer Service                       │
                         │                                 │
                         ├── ClickHouse  (events)          │
                         └── PostgreSQL  (reads)           │
                                                           │
                    Temporal Worker  <────────────────────-┘
                         │
                         └── Temporal Server
                                  │
                                  └── PostgreSQL (workflow state)
```

**Three Go services, one image** — mode is set via `FLEXPRICE_DEPLOYMENT_MODE`:

| Service | Mode | Role |
|---------|------|------|
| `api` | `api` | HTTP server, validates requests, publishes events to Kafka |
| `consumer` | `consumer` | Reads Kafka, writes to ClickHouse and Postgres |
| `worker` | `temporal_worker` | Runs billing workflows, invoicing, subscriptions |

**Infrastructure** — each component is either a subchart or your own managed service:

| Component | Subchart | Toggle |
|-----------|----------|--------|
| PostgreSQL | bitnami/postgresql | `postgresql.enabled` |
| Kafka | bitnami/kafka (KRaft, no Zookeeper) | `kafka.enabled` |
| Redis | bitnami/redis | `redis.enabled` |
| ClickHouse | hand-rolled StatefulSet | always internal or external |
| Temporal | temporalio/temporal | `temporal.enabled` |

---

## Quick start

```bash
# Add Helm repos
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo add temporalio https://go.temporal.io/helm-charts
helm repo update

# Pull subchart dependencies
helm dependency update ./helm/flexprice

# Install — everything runs in-cluster, no HA
helm install flexprice ./helm/flexprice \
  --set postgres.password=changeme \
  --set postgresql.auth.password=changeme \
  --set clickhouse.password=changeme \
  --set auth.secret=changeme \
  --set secrets.encryptionKey=$(openssl rand -hex 32)
```

Before the application pods start, the migration job:
1. Waits for Postgres and ClickHouse to be ready
2. Creates the Postgres `extensions` schema and `uuid-ossp` extension
3. Creates all Kafka topics (idempotent)
4. Runs ClickHouse SQL migrations
5. Runs Ent ORM schema migrations via the `migrate` binary

---

## Required values

| Key | Description |
|-----|-------------|
| `postgres.password` | PostgreSQL password used by the app |
| `postgresql.auth.password` | Must match `postgres.password` (passed to bitnami subchart) |
| `clickhouse.password` | ClickHouse password |
| `auth.secret` | JWT signing secret |
| `secrets.encryptionKey` | 64-char hex key — generate with `openssl rand -hex 32` |

---

## Exposing the API

```yaml
ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: api.yourcompany.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: flexprice-tls
      hosts:
        - api.yourcompany.com
```

---

## Using external managed services

Disable the subchart and point to your own endpoint. Mix and match freely.

### PostgreSQL (e.g. RDS)

```yaml
postgresql:
  enabled: false

postgres:
  external:
    enabled: true
  host: mydb.us-west-2.rds.amazonaws.com
  port: 5432
  user: flexprice
  password: yourpassword
  dbname: flexprice
  sslmode: require
```

### Kafka (e.g. MSK, Confluent Cloud)

```yaml
kafka:
  enabled: false
  external:
    enabled: true
  brokers:
    - broker-1.kafka.us-west-2.amazonaws.com:9092
    - broker-2.kafka.us-west-2.amazonaws.com:9092
  tls: true
  useSASL: true
  saslMechanism: SCRAM-SHA-512
  saslUser: flexprice
  saslPassword: yourpassword
```

### Redis (e.g. ElastiCache)

```yaml
redis:
  enabled: false
  external:
    enabled: true
  host: mycluster.abc123.ng.0001.use1.cache.amazonaws.com
  port: 6379
  useTLS: true
```

### ClickHouse (e.g. ClickHouse Cloud)

```yaml
clickhouse:
  external:
    enabled: true
  address: abc123.us-east-1.aws.clickhouse.cloud:9440
  tls: true
  username: flexprice
  password: yourpassword
  database: flexprice
```

### Temporal (e.g. Temporal Cloud)

```yaml
temporal:
  enabled: false
  external:
    enabled: true
  address: yournamespace.tmprl.cloud:7233
  namespace: yournamespace.account
  tls: true
  apiKey: yourapikey
```

---

## High Availability

HA is off by default — everything runs as a single replica. To enable for production, put overrides in a separate file and layer it on:

```bash
helm upgrade flexprice ./helm/flexprice -f values.yaml -f values-prod.yaml
```

**Example `values-prod.yaml`:**

```yaml
# Go services
api:
  replicaCount: 3
  autoscaling:
    enabled: true
    minReplicas: 3
    maxReplicas: 10

consumer:
  replicaCount: 3

worker:
  replicaCount: 2

# Postgres: primary + 1 read replica
postgresql:
  primary:
    replicaCount: 1
  readReplicas:
    replicaCount: 1

# Point app at the read replica for read-heavy queries
postgres:
  readerHost: myreplica.us-west-2.rds.amazonaws.com

# Kafka: 3 brokers (replication factor <= broker count)
kafka:
  replicaCount: 3
  controller:
    replicaCount: 3

# Redis: primary + replica + sentinel for automatic failover
redis:
  architecture: replication
  sentinel:
    enabled: true

# Temporal: multiple server pods
temporal:
  server:
    replicaCount: 2
```

---

## Migration job

Runs as a Helm `pre-install` / `pre-upgrade` hook. The release will not proceed until all steps pass.

| Step | values.yaml flag | What it does |
|------|-----------------|--------------|
| 1. Wait for Postgres | always runs | Polls `pg_isready` until the DB accepts connections |
| 2. Wait for ClickHouse | `migration.steps.clickhouse` | `nc` check on HTTP port |
| 3. Wait for Kafka | `migration.steps.kafka` | `nc` check on port 9092 |
| 4. Postgres schema | `migration.steps.postgresSetup` | `CREATE SCHEMA extensions` + `uuid-ossp` extension |
| 5. ClickHouse migrations | `migration.steps.clickhouse` | Runs `/app/migrations/clickhouse/*.sql` in order |
| 6. Kafka topics | `migration.steps.kafka` | Creates all required topics with `--if-not-exists` |
| 7. Ent migrations | `migration.steps.ent` | Runs `/app/migrate` binary for ORM schema changes |
| 8. Seed data | `migration.steps.seed` | Optional seed data, disabled by default |

Topic creation and ClickHouse migrations are idempotent — safe on every upgrade.

---

## Kafka topics

| Topic | Purpose |
|-------|---------|
| `events` | Raw usage events from API |
| `events_lazy` | Events on the lazy processing path |
| `events_post_processing` | Post-processed event queue |
| `events_post_processing_backfill` | Backfill queue |
| `system_events` | Webhook delivery events |

`autoCreateTopicsEnable` is deliberately `false` on the broker. All topics are created explicitly by the migration job so partition counts and replication factors are always under your control.

---

## Auth providers

```yaml
# Built-in API key auth (default)
auth:
  provider: flexprice
  secret: yourjwtsecret
  apiKey:
    header: x-api-key
```

```yaml
# Supabase
auth:
  provider: supabase
  supabase:
    baseUrl: https://yourproject.supabase.co
    serviceKey: yourservicekey
```

---

## Optional integrations

```yaml
sentry:
  enabled: true
  dsn: https://...@sentry.io/...
  environment: production

pyroscope:
  enabled: true
  serverAddress: https://pyroscope.example.com

s3:
  enabled: true
  region: us-west-2
  invoice:
    bucket: my-flexprice-invoices

email:
  enabled: true
  resendApiKey: re_...
  fromAddress: billing@yourcompany.com

webhook:
  svixConfig:
    enabled: true
    authToken: yoursvixtoken
```

---

## Repository layout

```
helm/flexprice/
├── Chart.yaml              # metadata + subchart dependencies
├── values.yaml             # all defaults with inline comments
├── README.md               # this file
└── templates/
    ├── _helpers.tpl        # service address resolution (edit here, not in each template)
    ├── NOTES.txt
    ├── app/                # the three Go services
    │   ├── configmap.yaml
    │   ├── secret.yaml
    │   ├── serviceaccount.yaml
    │   ├── deployment-api.yaml
    │   ├── deployment-consumer.yaml
    │   ├── deployment-worker.yaml
    │   ├── service.yaml
    │   └── ingress.yaml
    ├── infra/              # fallback internal infra (used when subcharts are disabled)
    │   ├── clickhouse.yaml   # always rendered (no subchart for ClickHouse)
    │   ├── postgres.yaml     # only rendered when postgresql.enabled=false AND external=false
    │   ├── kafka.yaml        # only rendered when kafka.enabled=false AND external=false
    │   ├── redis.yaml        # only rendered when redis.enabled=false AND external=false
    │   └── temporal.yaml     # only rendered when temporal.enabled=false AND external=false
    ├── jobs/
    │   └── migration.yaml  # pre-install/pre-upgrade hook
    ├── autoscaling/
    │   ├── hpa-api.yaml
    │   ├── hpa-consumer.yaml
    │   └── hpa-worker.yaml
    └── reliability/
        └── pdb-api.yaml    # ensures at least 1 API pod during node drain
```

### How address resolution works

`_helpers.tpl` defines one named template per infrastructure service. Every other template calls these — nothing hardcodes a hostname:

| Template | Subchart mode | External mode |
|----------|--------------|---------------|
| `flexprice.postgresHost` | `<release>-postgresql` | `postgres.host` |
| `flexprice.postgresPort` | `5432` | `postgres.port` |
| `flexprice.kafkaBrokers` | `<release>-kafka:9092` | `kafka.brokers` joined |
| `flexprice.redisHost` | `<release>-redis-master` | `redis.host` |
| `flexprice.redisPort` | `6379` | `redis.port` |
| `flexprice.clickhouseAddress` | `<release>-clickhouse:9000` | `clickhouse.address` |
| `flexprice.temporalAddress` | `<release>-temporal-frontend:7233` | `temporal.address` |

To switch from internal to external for any component, flip two flags — no template changes needed.

---

## Provisioning Script

[`../provision.sh`](../provision.sh) provisions the full cluster in the correct order. It handles the dependency chain that Helm hooks alone cannot guarantee on a cold cluster.

### Running order

| Step | What happens | How it waits |
|------|-------------|--------------|
| 1 | Create namespace + write K8s Secret | `kubectl apply` (idempotent) |
| 2 | Deploy infra only (apps + migration disabled) | `kubectl rollout status statefulset/...` |
| 3 | Ping each database | psql `SELECT 1`, ClickHouse `/ping`, Redis `PING`, `nc -z` for Kafka |
| 4 | Enable migration job (apps still off) | `helm --wait` blocks until pre-upgrade hook job completes |
| 5 | Enable app deployments, ingress off | `kubectl rollout status deployment/...` + port-forward health check |
| 6 | Enable ingress | `helm --wait` |
| 7 | External health check via `INGRESS_HOST` | 24 × 5s retries |

### Usage

```bash
# Required secrets
export POSTGRES_PASSWORD=...
export CLICKHOUSE_PASSWORD=...
export AUTH_SECRET=...           # JWT signing key
export ENCRYPTION_KEY=...        # secrets encryption key

# Optional
export REDIS_PASSWORD=...
export KAFKA_SASL_PASSWORD=...
export INGRESS_HOST=api.your-domain.com

# Run
./helm/provision.sh \
  --release flexprice \
  --namespace flexprice \
  --values ./helm/flexprice/values.yaml

# Dry-run (prints commands, executes nothing)
./helm/provision.sh --dry-run

# Upgrade only — skip infra deploy, still pings and re-runs migrations
./helm/provision.sh --skip-infra
```

### Why not rely solely on the migration hook?

The migration job is a `pre-install`/`pre-upgrade` Helm hook. On a cold cluster, Kubernetes creates all resources in parallel — the migration pod and the database StatefulSets race each other. The migration's init containers retry for ~2 minutes, which is usually enough, but not guaranteed.

The provisioning script eliminates this race by:
1. Deploying infra first and waiting for `rollout status` before proceeding
2. Confirming each database responds to a real query (not just a port check)
3. Only then running the migration helm upgrade

### Secrets design

Passwords are injected once in Step 1 as a Kubernetes Secret (`<release>-secrets`). The Helm chart reads them via `secretKeyRef` — passwords never need to appear in `values.yaml` in production. The provisioning script takes passwords from environment variables so nothing sensitive touches disk or version control.

---

## Node groups (EKS)

This Helm chart deploys workloads onto existing nodes — it does **not** create node groups. Node groups are AWS EC2 Auto Scaling Groups registered with EKS and must be provisioned before `helm install`.

### Recommended node group layout for production

| Node group | Instance type | Purpose |
|------------|--------------|---------|
| `flexprice-app` | `t3.large` (2 vCPU, 8 GB) | api, consumer, worker pods |
| `flexprice-infra` | `r6g.large` (2 vCPU, 16 GB) | PostgreSQL, Redis, ClickHouse StatefulSets (memory-heavy) |
| `flexprice-kafka` | `m6i.large` (2 vCPU, 8 GB) | Kafka controller + broker StatefulSets (I/O-heavy) |

A single `general` node group works fine for development.

### Provision node groups with eksctl

```bash
eksctl create nodegroup \
  --cluster  my-eks-cluster \
  --region   ap-south-1 \
  --name     flexprice-app \
  --node-type t3.large \
  --nodes-min 2 \
  --nodes-max 6 \
  --managed

eksctl create nodegroup \
  --cluster  my-eks-cluster \
  --region   ap-south-1 \
  --name     flexprice-infra \
  --node-type r6g.large \
  --nodes-min 1 \
  --nodes-max 3 \
  --managed \
  --node-labels role=infra \
  --node-taints dedicated=infra:NoSchedule
```

EKS automatically labels every node with `eks.amazonaws.com/nodegroup: <name>`.

### Pin pods to node groups via values.yaml

Use `nodeSelector` to target a specific node group. Set it globally (all pods) or per component (api/consumer/worker independently):

```yaml
# Pin all FlexPrice app pods to the flexprice-app node group
nodeSelector:
  eks.amazonaws.com/nodegroup: flexprice-app

# Or pin each component to a different node group
api:
  nodeSelector:
    eks.amazonaws.com/nodegroup: flexprice-app

consumer:
  nodeSelector:
    eks.amazonaws.com/nodegroup: flexprice-app

worker:
  nodeSelector:
    eks.amazonaws.com/nodegroup: flexprice-app

# Pin ClickHouse StatefulSet to infra nodes
clickhouse:
  standalone:
    nodeSelector:
      eks.amazonaws.com/nodegroup: flexprice-infra
    tolerations:
      - key: dedicated
        operator: Equal
        value: infra
        effect: NoSchedule
```

### Spread pods across Availability Zones

For HA, use `affinity` to prefer different AZs:

```yaml
api:
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchLabels:
                app.kubernetes.io/name: flexprice
                app.kubernetes.io/component: api
            topologyKey: topology.kubernetes.io/zone
```

### Provision node groups with Terraform

If you manage infrastructure with Terraform, use the `aws_eks_node_group` resource:

```hcl
resource "aws_eks_node_group" "flexprice_app" {
  cluster_name    = aws_eks_cluster.main.name
  node_group_name = "flexprice-app"
  node_role_arn   = aws_iam_role.node.arn
  subnet_ids      = var.private_subnet_ids

  instance_types = ["t3.large"]

  scaling_config {
    desired_size = 2
    min_size     = 2
    max_size     = 6
  }

  labels = {
    role = "app"
  }
}
```

The chart's `nodeSelector` key `eks.amazonaws.com/nodegroup: flexprice-app` matches the node group name automatically — no additional label configuration needed.
