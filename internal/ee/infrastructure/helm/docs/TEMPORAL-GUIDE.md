# Temporal Guide

FlexPrice uses [Temporal](https://temporal.io) for billing-cycle,
invoice-processing, and subscription-change workflows. You have two
deployment options.

## Option A: Temporal Cloud (recommended)

Lowest operational burden. Pay per active namespace; suitable for
production scale.

```yaml
temporal:
  enabled: false        # disable bundled subchart

temporalConfig:
  external:
    enabled: true
  address: "REPLACE-WITH-NAMESPACE.account.tmprl.cloud:7233"
  namespace: "REPLACE-WITH-NAMESPACE.account"
  taskQueue: billing-task-queue
  tls: true
  # apiKey comes from existingSecret key: temporal-api-key
  bootstrapNamespaces: []   # namespaces created out-of-band in Temporal Cloud UI
```

### Setup steps

1. Sign in to https://cloud.temporal.io.
2. Create a namespace (e.g. `flexprice-prod`).
3. Generate an API key under **API Keys** for that namespace. Save the key.
4. Mirror it into your Kubernetes Secret as `temporal-api-key`:
   ```bash
   kubectl -n flexprice patch secret flexprice-secrets \
     -p='{"stringData":{"temporal-api-key":"<the-key>"}}'
   ```
5. Set `temporalConfig.address` and `temporalConfig.namespace` to the
   Cloud values shown on the namespace details page.

### Pre-install verification

```bash
# Install the temporal CLI locally:
brew install temporal   # or: curl -sSf https://temporal.download/cli.sh | sh

# From your workstation, with the API key in TEMPORAL_API_KEY:
temporal --address REPLACE-WITH-NAMESPACE.account.tmprl.cloud:7233 \
  --namespace REPLACE-WITH-NAMESPACE.account \
  --tls \
  --api-key "$TEMPORAL_API_KEY" \
  operator namespace describe
```

If this prints the namespace details, the chart will connect.

## Option B: Self-hosted in the same cluster

Run Temporal's own helm chart side-by-side with FlexPrice. Higher ops
burden — you operate Cassandra/Postgres, ElasticSearch, the four Temporal
services (frontend, history, matching, worker), and schema upgrades.

```bash
helm repo add temporal https://go.temporal.io/helm-charts
helm repo update

# Co-install in the flexprice namespace
helm install temporal temporal/temporal \
  -n flexprice --create-namespace \
  --set server.replicaCount=3 \
  --set cassandra.enabled=false \
  --set mysql.enabled=false \
  --set postgresql.enabled=true \
  --set postgresql.auth.password='REPLACE-WITH-PG-PW' \
  --set elasticsearch.enabled=false \
  --set prometheus.enabled=false \
  --set grafana.enabled=false
```

Then in the chart:

```yaml
temporal:
  enabled: false              # we already installed the temporal chart out-of-band

temporalConfig:
  external:
    enabled: true             # treat the in-cluster Temporal as "external" too
  address: "temporal-frontend.flexprice.svc.cluster.local:7233"
  namespace: "default"
  taskQueue: billing-task-queue
  tls: false
  bootstrapNamespaces:
    - name: default
      retention: 72h
```

The chart's post-install hook (`templates/jobs/temporal-namespace-bootstrap.yaml`)
will register the `default` namespace via the admin-tools image. For Cloud,
keep `bootstrapNamespaces: []` because namespaces are provisioned out-of-band.

## Option C: Bundled subchart (NOT recommended for prod)

The chart's own `temporal` subchart is enabled in `values-local.yaml` for
local dev. It uses the FlexPrice Postgres for storage and one replica of
each Temporal service. Fine for kicking the tires, **not** for production.

## Namespace bootstrap

For self-hosted Temporal, the chart's post-install/post-upgrade hook
registers namespaces in `temporalConfig.bootstrapNamespaces`:

```yaml
temporalConfig:
  bootstrapNamespaces:
    - name: default
      retention: 72h
      description: "Default Temporal namespace for FlexPrice workflows"
    - name: long-retention
      retention: 720h    # 30 days
```

The hook uses the `temporalio/admin-tools` image (override via
`temporalConfig.bootstrapImage.*`) and exits cleanly if the namespace
already exists.

For Temporal Cloud, set `bootstrapNamespaces: []` — the hook would try to
`tctl namespace register` on Cloud, which the API key doesn't allow.

## Worker scaling

The `worker` Deployment polls task queues. Scale based on activity rate,
not CPU:

```yaml
worker:
  replicaCount: 3
  autoscaling:
    enabled: false       # CPU HPA is the wrong signal for a Temporal worker
```

For Temporal Cloud, you can scale workers vertically (more
`maxConcurrentActivityExecutionSize`) or horizontally (more
`replicaCount`). Vertical works until a single worker becomes a
single-host bottleneck; horizontal works without limit.

## Observability

Temporal Cloud has its own dashboard. Self-hosted Temporal exposes
Prometheus metrics on the frontend service; scrape those separately —
they're not surfaced through the FlexPrice chart.

## Troubleshooting

```bash
# Is the worker connecting?
kubectl logs -n flexprice deploy/flexprice-worker | grep -iE 'temporal|connected'

# Bootstrap Job state
kubectl get job -n flexprice -l app.kubernetes.io/component=temporal-bootstrap
kubectl logs -n flexprice -l app.kubernetes.io/component=temporal-bootstrap

# tctl from inside the worker pod (self-hosted only)
WORKER=$(kubectl get pod -n flexprice -l app.kubernetes.io/component=worker -o name | head -1)
kubectl exec -n flexprice $WORKER -- /bin/sh -c \
  'echo workflows: ; /app/flexctl temporal list 2>&1 | head' || true
```

Common Temporal Cloud failure: `address` set to the Web URL instead of the
gRPC URL. The gRPC URL ends in `:7233`, not `:443`.
