# EKS Quickstart

End-to-end FlexPrice install on AWS EKS using managed services for every
backing component. Other platforms: [PLATFORMS.md](PLATFORMS.md).

## What you'll have at the end

```
                         Route53 A record: api.flexprice.yourcompany.com
                              │
                              ▼
                ┌──────── NLB (provisioned by ingress-nginx) ─────────┐
                │                                                     │
EKS cluster ─── │  ingress-nginx → flexprice-api Service              │
                │                                                     │
                │  flexprice-api / -consumer / -worker Deployments    │
                │     │             │              │                  │
                └─────┼─────────────┼──────────────┼──────────────────┘
                      ▼             ▼              ▼
                    RDS         MSK Cluster   Temporal Cloud
                  Postgres     (SCRAM auth)        ↑
                                                   │
                              ElastiCache    ClickHouse Cloud
                                Redis
```

**Estimated AWS cost**: ~$300–500/mo for the bare minimum (small RDS,
3-broker MSK, single ElastiCache node, 2× t3.medium EKS nodes). ClickHouse
Cloud + Temporal Cloud are separate billing.

## 0. Prerequisites

Verify these are installed locally:

```bash
aws --version    # ≥ 2.13
kubectl version --client
helm version     # ≥ 3.14
eksctl version   # optional, easier cluster bootstrap
```

You also need an AWS account with admin (or scoped equivalent) for the
initial setup, plus a public DNS zone in Route53 or your DNS provider.

## 1. EKS cluster

```bash
eksctl create cluster \
  --name flexprice-prod \
  --region us-east-1 \
  --version 1.30 \
  --nodegroup-name workers \
  --node-type t3.medium \
  --nodes 2 \
  --nodes-min 2 \
  --nodes-max 6 \
  --with-oidc \
  --managed
```

Wait ~15 min. Then verify:

```bash
aws eks update-kubeconfig --name flexprice-prod --region us-east-1
kubectl get nodes
kubectl get pods -A
```

## 2. Add-ons: AWS EBS CSI driver

Required if any pod uses a PVC (rare in our default prod setup, but
useful):

```bash
eksctl create addon --name aws-ebs-csi-driver \
  --cluster flexprice-prod --region us-east-1 --force
```

Create a `gp3` StorageClass:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: gp3
  annotations:
    storageclass.kubernetes.io/is-default-class: "true"
provisioner: ebs.csi.aws.com
volumeBindingMode: WaitForFirstConsumer
parameters: { type: gp3, encrypted: "true" }
EOF
```

If `gp2` was already default, demote it:

```bash
kubectl annotate storageclass gp2 storageclass.kubernetes.io/is-default-class-
```

## 3. ingress-nginx + cert-manager

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm repo add jetstack       https://charts.jetstack.io
helm repo update

helm install ingress-nginx ingress-nginx/ingress-nginx \
  -n ingress-nginx --create-namespace \
  --set controller.service.type=LoadBalancer \
  --set controller.service.annotations."service\.beta\.kubernetes\.io/aws-load-balancer-type"=nlb

helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace \
  --set installCRDs=true
```

Wait for the NLB to provision (3–5 min):

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller
# Note the EXTERNAL-IP (DNS name like xxx.elb.us-east-1.amazonaws.com)
```

Create a Route53 A-alias record (or CNAME for non-Route53 DNS) for
`api.flexprice.yourcompany.com` → that hostname.

Create the cert-manager `ClusterIssuer`:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata: { name: letsencrypt-prod }
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: ops@yourcompany.com
    privateKeySecretRef: { name: letsencrypt-prod-key }
    solvers:
      - http01: { ingress: { class: nginx } }
EOF
```

## 4. Provision managed data services

You can use Terraform, Pulumi, CloudFormation, or click-ops. Skeletons
below.

### 4.1 RDS Postgres

Console → RDS → Create database:
- Engine: Postgres 15
- Templates: Production
- DB instance identifier: `flexprice-prod`
- Master username: `flexprice`
- Master password: generate a strong one (save it)
- Instance: `db.t4g.medium` to start
- Storage: 100 GB gp3, autoscaling on
- VPC: same VPC as EKS
- Public access: **No**
- VPC security group: create a new one allowing 5432 from the EKS node security group
- Backup retention: 7+ days, enable Performance Insights

After ~10 min, copy the endpoint hostname.

```sql
-- Connect with psql from a bastion or kubectl debug pod
CREATE DATABASE flexprice;
CREATE DATABASE temporal;            -- only if self-hosting Temporal
```

### 4.2 ClickHouse Cloud

https://clickhouse.com/cloud/sign-up → create a Service.
- Region: same as your EKS region
- Compute: Development tier to start (upgrade to Production tier when load increases)

Once provisioned:
- **Connections** page → copy the **Native protocol** address
  (`xxxx.us-east-1.aws.clickhouse.cloud:9440`).
- Copy the default password.

### 4.3 MSK (SCRAM auth — IAM not supported)

Console → MSK → Create cluster:
- Cluster type: Provisioned
- Apache Kafka version: 3.7
- Broker type: `kafka.m5.large` × 3 (one per AZ)
- Storage: 100 GB per broker
- Authentication: ☑ SASL/SCRAM
- Encryption: TLS in transit + TLS between brokers
- VPC: same as EKS
- Security group: allow 9094 (TLS-SCRAM) from EKS node SG

After cluster creation (~20 min):

```bash
# Create a SCRAM secret in Secrets Manager and associate with the cluster:
aws secretsmanager create-secret \
  --name AmazonMSK_flexprice \
  --secret-string '{"username":"flexprice","password":"REPLACE-WITH-MSK-PW"}' \
  --kms-key-id alias/aws/secretsmanager

aws kafka batch-associate-scram-secret \
  --cluster-arn $MSK_CLUSTER_ARN \
  --secret-arn-list $SCRAM_SECRET_ARN

# Bootstrap brokers (you'll need these for kafkaConfig.brokers):
aws kafka get-bootstrap-brokers --cluster-arn $MSK_CLUSTER_ARN \
  --query 'BootstrapBrokerStringSaslScram' --output text
```

### 4.4 ElastiCache Redis

Console → ElastiCache → Create cluster:
- Cluster mode: Disabled (for `redisExtended.clusterMode: false`) or Enabled (for `true`)
- Engine: Redis 7
- Node type: `cache.t4g.small` to start
- Number of replicas: 1+ for HA
- VPC: same as EKS
- Encryption: ☑ in-transit, ☑ at-rest
- Auth: enable Redis AUTH (recommended)

Copy the primary endpoint hostname.

### 4.5 Temporal Cloud

See [TEMPORAL-GUIDE.md](TEMPORAL-GUIDE.md). Create a namespace, generate
an API key.

## 5. IAM roles for IRSA

See [AWS-IAM.md](AWS-IAM.md) for the per-workload policies. Create three
roles: `flexprice-api`, `flexprice-consumer`, `flexprice-worker`. Note
the ARNs.

## 6. Create the Kubernetes Secret

See [SECRETS.md](SECRETS.md) for the full key inventory. Minimum:

```bash
kubectl create namespace flexprice

kubectl create secret generic flexprice-secrets -n flexprice \
  --from-literal=encryption-key="$(openssl rand -hex 32)" \
  --from-literal=auth-secret="$(openssl rand -hex 32)" \
  --from-literal=postgres-password='REPLACE-WITH-RDS-PW' \
  --from-literal=clickhouse-password='REPLACE-WITH-CH-PW' \
  --from-literal=kafka-sasl-password='REPLACE-WITH-MSK-PW' \
  --from-literal=redis-password='REPLACE-WITH-REDIS-PW' \
  --from-literal=temporal-api-key='REPLACE-WITH-TEMPORAL-KEY'
```

For prod, route this through External Secrets Operator —
[SECRETS.md](SECRETS.md#external-secrets-operator).

## 7. Values file

Save as `values-flexprice-prod.yaml`:

```yaml
secrets:
  existingSecret: flexprice-secrets

image:
  repository: ghcr.io/flexprice/flexprice
  tag: ""    # resolves to .Chart.AppVersion
  pullPolicy: IfNotPresent

api:
  replicaCount: 2
  autoscaling: { enabled: true, minReplicas: 2, maxReplicas: 10 }

consumer:
  replicaCount: 2
  autoscaling: { enabled: true, minReplicas: 2, maxReplicas: 5 }

worker:
  replicaCount: 2

serviceAccount:
  create: true
  perComponent: true
  components:
    api:
      annotations:
        eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flexprice-api
    consumer:
      annotations:
        eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flexprice-consumer
    worker:
      annotations:
        eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flexprice-worker

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/proxy-body-size: "10m"
  hosts:
    - host: api.flexprice.yourcompany.com
      paths: [{ path: /, pathType: Prefix }]
  tls:
    - secretName: flexprice-api-tls
      hosts: [api.flexprice.yourcompany.com]

frontendIngress: { enabled: false }
temporalIngress: { enabled: false }

# ── External services ────────────────────────────────────────────────────────
postgresql: { enabled: false }
postgres:
  external: { enabled: true }
  host: "flexprice-prod.xxxxx.us-east-1.rds.amazonaws.com"
  port: 5432
  user: flexprice
  database: flexprice
  sslmode: require
  maxOpenConns: 25
  maxIdleConns: 10
  connMaxLifetimeMinutes: 60

clickhouse:
  mode: external
  address: "xxxx.us-east-1.aws.clickhouse.cloud:9440"
  tls: true
  username: default
  database: flexprice
  maxMemoryUsageGB: 90

kafka: { enabled: false }
kafkaConfig:
  external: { enabled: true }
  brokers: "b-1.flexprice-prod.xxxxxx.c2.kafka.us-east-1.amazonaws.com:9094,b-2.flexprice-prod.xxxxxx.c2.kafka.us-east-1.amazonaws.com:9094,b-3.flexprice-prod.xxxxxx.c2.kafka.us-east-1.amazonaws.com:9094"
  useSASL: true
  saslMechanism: "SCRAM-SHA-512"
  saslUsername: flexprice
  tls: true

redis: { enabled: false }
redisConfig:
  external: { enabled: true }
  host: "flexprice-prod.xxxxxx.use1.cache.amazonaws.com"
  port: 6379
  tls: true
redisExtended:
  clusterMode: false        # set true if you provisioned cluster-mode-enabled ElastiCache
  poolSize: 50

temporal: { enabled: false }
temporalConfig:
  external: { enabled: true }
  address: "your-namespace.account.tmprl.cloud:7233"
  namespace: "your-namespace.account"
  taskQueue: billing-task-queue
  tls: true
  bootstrapNamespaces: []   # Cloud — namespaces created out-of-band

# ── Observability ────────────────────────────────────────────────────────────
logging:
  level: info
  format: json
sentry:
  enabled: true
  environment: production
  sampleRate: 0.1
```

## 8. Install

```bash
helm install flexprice \
  oci://ghcr.io/flexprice/charts/flexprice \
  --version 1.1.0 \
  -n flexprice \
  -f values-flexprice-prod.yaml \
  --wait --timeout 10m
```

Watch the migration Job complete:

```bash
kubectl logs -n flexprice job/flexprice-migration -f
```

Then check the pods:

```bash
kubectl get pods -n flexprice
kubectl get ingress -n flexprice
```

## 9. Smoke test

```bash
helm test flexprice -n flexprice
curl -sSf https://api.flexprice.yourcompany.com/health
```

Both should succeed. If they don't, see [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## 10. Day-2 ops

- [BACKUPS.md](BACKUPS.md) — RDS / ClickHouse / Temporal Cloud
- [PRE-SHIP-VALIDATION.md](PRE-SHIP-VALIDATION.md) — gates to pass before promoting a new chart version
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — common failure modes
- [MIGRATION-GUIDE.md](MIGRATION-GUIDE.md) — chart major upgrades

## Common EKS gotchas

- **NLB stuck pending**: the EKS node security group needs to allow
  health-check probes from the NLB. The AWS Load Balancer Controller
  handles this automatically; if you skipped installing it, the NLB
  doesn't auto-configure SGs.
- **`gp2` becomes default again**: AWS occasionally re-promotes `gp2`
  after a cluster upgrade. Rerun the `kubectl annotate` step in #2.
- **MSK SCRAM "PermissionDenied"**: the SCRAM secret must be **associated**
  with the cluster (step 4.3). Creating the secret isn't enough.
- **Pods crash with "no AWS credentials"**: IRSA isn't wired. Verify the
  ServiceAccount has the annotation and the role trust policy `:sub`
  exactly matches `system:serviceaccount:flexprice:flexprice-<component>`.
