# Platform Deltas (GKE / AKS / bare-metal / local)

[EKS-QUICKSTART.md](EKS-QUICKSTART.md) is the canonical step-by-step. This
doc captures the deltas for other platforms — what changes, what stays the
same.

## Service mapping

| Component       | EKS                       | GKE                         | AKS                                  | Bare metal / on-prem             |
|-----------------|---------------------------|-----------------------------|--------------------------------------|----------------------------------|
| Postgres        | RDS / Aurora              | Cloud SQL for PostgreSQL    | Azure DB for PostgreSQL Flexible     | CloudNativePG, Zalando, bitnami  |
| Kafka           | MSK *(SCRAM only)*        | Confluent Cloud on GCP       | Confluent Cloud on Azure / Event Hubs Kafka | Strimzi, bitnami/kafka      |
| ClickHouse      | ClickHouse Cloud          | ClickHouse Cloud            | ClickHouse Cloud                     | Altinity operator, bitnami       |
| Redis           | ElastiCache / MemoryDB    | Memorystore                 | Azure Cache for Redis                | bitnami/redis, Redis Operator    |
| Temporal        | Temporal Cloud            | Temporal Cloud              | Temporal Cloud                       | temporalio/temporal helm chart   |
| Object storage  | S3                        | GCS                         | Azure Blob                           | MinIO                            |
| Load balancer   | NLB (auto via ingress-nginx) | GCP TCP/UDP LB           | Azure Standard LB                    | MetalLB / kube-vip               |
| Identity        | IRSA / Pod Identity       | Workload Identity            | Azure AD Workload Identity           | static SA tokens / cert-manager  |
| TLS certs       | cert-manager + Let's Encrypt | same                     | same                                 | same                             |
| DNS             | Route53                   | Cloud DNS                    | Azure DNS                            | your registrar                   |
| Secrets         | Secrets Manager + ESO     | Secret Manager + ESO         | Key Vault + ESO                      | Sealed Secrets / Vault           |

## GKE

### Cluster

```bash
gcloud container clusters create-auto flexprice-prod \
  --region us-central1 \
  --release-channel regular
```

Autopilot is the easiest path. For Standard, enable Workload Identity
explicitly:

```bash
gcloud container clusters create flexprice-prod \
  --workload-pool=PROJECT.svc.id.goog \
  --release-channel regular \
  --num-nodes 2 --machine-type e2-standard-2 \
  --region us-central1
```

### Workload Identity

Per workload, bind a Google Service Account to the Kubernetes
ServiceAccount the chart creates. With
`serviceAccount.perComponent=true`, the chart's SAs are named
`flexprice-{api,consumer,worker}`.

```bash
for c in api consumer worker; do
  GSA="flexprice-${c}@PROJECT.iam.gserviceaccount.com"
  gcloud iam service-accounts add-iam-policy-binding $GSA \
    --role roles/iam.workloadIdentityUser \
    --member "serviceAccount:PROJECT.svc.id.goog[flexprice/flexprice-$c]"
done
```

In values:

```yaml
serviceAccount:
  perComponent: true
  components:
    api:      { annotations: { iam.gke.io/gcp-service-account: flexprice-api@PROJECT.iam.gserviceaccount.com } }
    consumer: { annotations: { iam.gke.io/gcp-service-account: flexprice-consumer@PROJECT.iam.gserviceaccount.com } }
    worker:   { annotations: { iam.gke.io/gcp-service-account: flexprice-worker@PROJECT.iam.gserviceaccount.com } }
```

### Cloud SQL

Create Postgres 15 instance. Two connectivity options:
- **Public IP** + Cloud SQL Auth Proxy as a sidecar (recommended).
- **Private IP** via VPC peering — simpler from the chart's POV (set
  `postgres.host` to the private IP).

The chart doesn't bundle a Cloud SQL Auth Proxy sidecar — if you go that
route, patch the Deployment with `extraVolumes`/`extraContainers` (not
yet supported via the chart's passthrough; you'd fork or use Kustomize).

For most users, **private IP** is the right answer.

### Kafka

GCP has no native Kafka. Three options:
1. **Confluent Cloud on GCP** (recommended) — SCRAM auth, works
   identically to EKS+MSK SCRAM.
2. **Aiven for Kafka** — single-tenant managed offering.
3. **Strimzi** — self-host, run the operator + KafkaCluster CRD in the
   same cluster.

Wire the same way as MSK in [EKS-QUICKSTART.md](EKS-QUICKSTART.md).

### Memorystore

Memorystore for Redis Standard tier or Cluster tier. Cluster tier maps to
`redisExtended.clusterMode: true`.

## AKS

### Cluster

```bash
az aks create \
  --name flexprice-prod \
  --resource-group flexprice-rg \
  --node-count 2 \
  --node-vm-size Standard_B2s \
  --enable-oidc-issuer \
  --enable-workload-identity \
  --enable-managed-identity \
  --kubernetes-version 1.30
```

### Workload Identity

Annotate per-workload SAs with the federated identity:

```yaml
serviceAccount:
  perComponent: true
  components:
    api:
      annotations:
        azure.workload.identity/client-id: <UAMI-CLIENT-ID-API>
    consumer:
      annotations:
        azure.workload.identity/client-id: <UAMI-CLIENT-ID-CONSUMER>
    worker:
      annotations:
        azure.workload.identity/client-id: <UAMI-CLIENT-ID-WORKER>
```

Add the pod label too (required by the Azure Workload Identity webhook):

```yaml
podLabels:
  azure.workload.identity/use: "true"
```

The chart currently doesn't expose a global `podLabels`; until it does,
add this via Kustomize overlay or chart fork.

### Azure DB for PostgreSQL Flexible Server

```bash
az postgres flexible-server create \
  --resource-group flexprice-rg \
  --name flexprice-prod \
  --location eastus \
  --tier GeneralPurpose --sku-name Standard_D2ds_v4 \
  --version 15 --storage-size 128 \
  --high-availability ZoneRedundant
```

Use the private endpoint hostname in `postgres.host`. Set
`postgres.sslmode: require` — Azure DB requires TLS.

### Event Hubs (Kafka API) — caveat

Azure's Event Hubs Kafka surface is **mostly** compatible but has quirks
(no transactional producers, no compaction). For production, prefer
Confluent Cloud on Azure or run Strimzi.

### Azure Cache for Redis

Standard tier (`redisExtended.clusterMode: false`) or Enterprise tier with
clustering enabled (`true`).

## Bare metal / on-prem

This is the hardest path because you bring everything yourself.

### Cluster

[kubeadm](https://kubernetes.io/docs/setup/production-environment/tools/kubeadm/)
or [k3s](https://k3s.io) for smaller deployments. Make sure the cluster
has:
- A CNI (Cilium, Calico, Flannel) with NetworkPolicy support.
- A default StorageClass backed by your storage (NFS, Ceph, Longhorn,
  TopoLVM).
- [MetalLB](https://metallb.universe.tf/) configured with a pool of IPs
  for `LoadBalancer` services.

### Backing services

Everything self-hosted:
- **Postgres**: CloudNativePG operator, Zalando Postgres Operator, or
  bitnami/postgresql with WAL-G backups to MinIO.
- **Kafka**: Strimzi.
- **ClickHouse**: Altinity operator.
- **Redis**: Redis Operator or bitnami/redis with sentinel.
- **Temporal**: temporalio/temporal helm chart with internal Postgres.

The trade-off: you operate all of this yourself. Most teams pick this path
only when regulatory/sovereignty constraints rule out cloud.

### Object storage

For invoice PDF storage with `s3.enabled=true`, run
[MinIO](https://min.io/) and point the app at it. The chart currently
uses the AWS SDK for S3 — set the AWS endpoint URL via env var (this is
not yet a chart value; document as an extension point).

### Identity

No IRSA / Workload Identity equivalent. Use:
- A dedicated SA with a long-lived token, mounted as a projected volume.
- Or `cert-manager`-issued mTLS client certs to talk to Vault, where
  Vault holds the actual cloud creds.

## Local development (kind / OrbStack / Docker Desktop)

The chart ships [`values-local.yaml`](../values-local.yaml) — flips all
bundled subcharts back on with dev-safe credentials.

```bash
kind create cluster --config helm/kind-cluster.yaml
helm install flexprice helm/flexprice \
  -n flexprice --create-namespace \
  -f helm/values-local.yaml \
  --wait
```

Then visit `http://flexprice.local` (after adding to `/etc/hosts`).

Use `helm/provision.sh` for an opinionated one-command bring-up that also
installs ingress-nginx + creates the kind cluster.

## Multi-platform values pattern

Rather than maintain N copies of `values-prod.yaml`, layer:

```bash
# Common prod settings (replicas, resources, autoscaling, ingress)
values-prod-common.yaml

# Per-platform endpoints + IAM annotations
values-prod-eks.yaml
values-prod-gke.yaml
values-prod-aks.yaml

helm install flexprice ./flexprice \
  -f values-prod-common.yaml \
  -f values-prod-eks.yaml
```

The later `-f` wins, so platform-specific overrides take precedence.
