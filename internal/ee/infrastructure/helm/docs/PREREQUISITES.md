# Cluster Prerequisites

Before `helm install flexprice`, your cluster needs the pieces below. Most
managed-Kubernetes offerings (EKS, GKE, AKS) give you a cluster with **none**
of these wired by default — they're additive.

## 1. Kubernetes version

| Floor | Recommended |
|-------|-------------|
| 1.24  | 1.28 – 1.32 |

The `kubeVersion` in [`Chart.yaml`](../flexprice/Chart.yaml) enforces the floor.
1.24 is EOL upstream; we keep it as the floor because the chart's manifests use
only stable APIs, but **upgrade your cluster** — your security team won't
accept a chart that depends on an EOL Kubernetes.

```bash
kubectl version --short
```

## 2. Ingress controller

The chart renders an `Ingress` resource by default (`ingress.enabled: true`)
with `ingress.className: nginx`. If the cluster has no controller, the
Ingress object lands in apiserver but routes nothing — the api pod is only
reachable via `kubectl port-forward`.

Install [ingress-nginx](https://kubernetes.github.io/ingress-nginx/):

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo update
helm install ingress-nginx ingress-nginx/ingress-nginx \
  -n ingress-nginx --create-namespace \
  --set controller.service.type=LoadBalancer
```

Cloud-specific notes:
- **EKS**: provisions an AWS Network Load Balancer (NLB) automatically when
  `controller.service.type=LoadBalancer`. Public IP / DNS appears in `kubectl
  get svc -n ingress-nginx` after ~3 min.
- **GKE**: same flow — provisions a Google TCP/UDP Network Load Balancer.
- **AKS**: provisions an Azure Standard Load Balancer.
- **bare metal**: no cloud LB exists; install
  [MetalLB](https://metallb.universe.tf/) or use
  `controller.hostNetwork: true` on the controller's DaemonSet.

Alternatives: `traefik`, `haproxy-ingress`, `contour`. Set `ingress.className`
to match.

## 3. TLS via cert-manager (recommended)

For HTTPS on your api hostname, install
[cert-manager](https://cert-manager.io/):

```bash
helm repo add jetstack https://charts.jetstack.io
helm repo update
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace \
  --set installCRDs=true
```

Then create a ClusterIssuer (Let's Encrypt example):

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: ops@yourcompany.com
    privateKeySecretRef:
      name: letsencrypt-prod-key
    solvers:
      - http01:
          ingress:
            class: nginx
```

The chart wires the issuer via `ingress.annotations`:

```yaml
ingress:
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: api.flexprice.yourcompany.com
      paths: [{ path: /, pathType: Prefix }]
  tls:
    - secretName: flexprice-api-tls
      hosts: [api.flexprice.yourcompany.com]
```

## 4. StorageClass (only if you use bundled subcharts)

For prod, the bundled `postgresql`, `kafka`, `redis`, and `temporal` subcharts
are off by default — no PVCs, no StorageClass needed.

If you flip any on (we only recommend this for dev), check:

```bash
kubectl get storageclass
# Expect at least one with (default) annotation
```

EKS default: `gp2`. To use `gp3` (faster, cheaper), explicitly create it:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: gp3
  annotations:
    storageclass.kubernetes.io/is-default-class: "true"
provisioner: ebs.csi.aws.com
volumeBindingMode: WaitForFirstConsumer
parameters:
  type: gp3
  encrypted: "true"
```

Make sure the
[AWS EBS CSI driver](https://github.com/kubernetes-sigs/aws-ebs-csi-driver) is
installed (EKS console → Add-ons → AWS EBS CSI Driver).

## 5. External services (managed or self-hosted)

Production must have **external** services for these — the chart's bundled
subcharts default to `enabled: false` for good reason (no backup, no HA, no
upgrade story):

| Service     | Managed options                                       | Self-host options                          |
|-------------|-------------------------------------------------------|--------------------------------------------|
| PostgreSQL  | RDS / Aurora, Cloud SQL, Azure DB for PostgreSQL      | bitnami/postgresql, CloudNativePG, Zalando |
| Kafka       | MSK (SCRAM only — see note), Confluent Cloud, Aiven  | Strimzi, bitnami/kafka                     |
| ClickHouse  | ClickHouse Cloud                                      | Altinity operator, bitnami/clickhouse      |
| Redis       | ElastiCache, MemoryDB, Upstash, Azure Cache for Redis | bitnami/redis, Redis Operator              |
| Temporal    | Temporal Cloud                                        | temporalio/temporal helm chart             |

**MSK SCRAM-only note**: The app only supports `SCRAM-SHA-256/512` SASL; AWS
MSK IAM auth is **not wired** in the consumer (`internal/kafka/base.go`).
Either provision your MSK cluster with SCRAM users or use Confluent Cloud /
Aiven, which support SCRAM out of the box.

Set up these endpoints before installing the chart. See per-platform
quickstarts:
- [EKS](EKS-QUICKSTART.md)
- [Other platforms](PLATFORMS.md)

## 6. Metrics & monitoring (optional, recommended)

- **Prometheus Operator** (`kube-prometheus-stack`) — the chart exposes a
  `serviceMonitor.*` value but the app does not emit `/metrics` yet, so this
  is parked.
- **OpenTelemetry collector** — the chart wires `logging.otel.*` to forward
  structured logs to any OTLP-compatible endpoint (Grafana Loki, Datadog
  Agent, AWS Distro, SigNoz). Recommended for prod.

## 7. KEDA (optional, only if using consumer.keda)

If you flip `consumer.keda.enabled=true` for Kafka-lag autoscaling, install
[KEDA](https://keda.sh) cluster-wide first:

```bash
helm repo add kedacore https://kedacore.github.io/charts
helm repo update
helm install keda kedacore/keda -n keda --create-namespace
```

## 8. cluster-scoped operators (only if needed)

- **Altinity ClickHouse operator** — only if `clickhouse.mode=altinity`.
  Install once per cluster, **before** the chart:
  ```bash
  kubectl apply -f https://raw.githubusercontent.com/Altinity/clickhouse-operator/release-0.24/deploy/operator/clickhouse-operator-install-bundle.yaml
  ```
- **External Secrets Operator** — strongly recommended for prod; lets the
  chart's `secrets.existingSecret` point at a Secret synced from AWS Secrets
  Manager / GCP Secret Manager / Azure Key Vault / HashiCorp Vault. See
  [SECRETS.md](SECRETS.md#external-secrets-operator).

## 9. DNS

You need a public DNS A/AAAA record pointing at the ingress controller's
external address:

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller -o wide
# Copy EXTERNAL-IP / HOSTNAME → create A record for api.flexprice.yourcompany.com
```

## Pre-flight checklist

Run this before `helm install`:

```bash
# 1. Kubernetes version
kubectl version --short

# 2. Ingress controller present
kubectl get svc -n ingress-nginx 2>/dev/null && echo "ingress OK" || echo "INSTALL ingress-nginx"

# 3. cert-manager (if using HTTPS)
kubectl get crd clusterissuers.cert-manager.io 2>/dev/null && echo "cert-manager OK" || echo "INSTALL cert-manager"

# 4. Default StorageClass (only if bundled subcharts)
kubectl get sc | grep -q '(default)' && echo "default SC OK" || echo "SET A DEFAULT STORAGECLASS"

# 5. External services reachable from inside the cluster (test from a debug pod)
kubectl run -it --rm debug --image=busybox --restart=Never -- \
  sh -c 'nc -zv $POSTGRES_HOST 5432 && nc -zv $CLICKHOUSE_HOST 9000'
```

If every step prints "OK", you're ready to install the chart.
