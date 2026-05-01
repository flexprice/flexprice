# FlexPrice Local Kubernetes Setup Skill

This skill gives Claude Code deep, validated knowledge for spinning up the full FlexPrice stack on a local Kubernetes cluster using the Helm chart at `helm/flexprice/`. It covers every known failure mode, OrbStack nuances, Apple Silicon quirks, and recovery procedures.

---

## Prerequisites Checklist

Before running anything, verify these are installed and running:

```bash
# Required tools
brew install helm kubectl

# OrbStack (preferred on macOS, especially Apple Silicon)
# Download from https://orbstack.dev — free for personal use
# After install: open OrbStack → Settings → Kubernetes → Enable
# OrbStack k8s is ready when the status light is green

# Verify kubectl can reach OrbStack k8s
kubectl config use-context orbstack
kubectl get nodes
# Should show one node in "Ready" state

# Verify helm
helm version  # v3.x required; brew installs to /opt/homebrew/Cellar/helm/*/bin/helm

# Docker (OrbStack installs it as a drop-in Docker Desktop replacement)
docker info | grep "Server Version"
```

**Do NOT use `kind` on Apple Silicon (M1/M2/M3/M4).** kind fails on ARM64 — see the Apple Silicon section below for the full explanation.

---

## One-Command Setup (OrbStack)

From the repo root:

```bash
cd helm/

# Step 0: /etc/hosts — one time only
# OrbStack ingress IP is typically 192.168.139.2 (verify with the command below)
INGRESS_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo "192.168.139.2")
echo "$INGRESS_IP  api.flexprice.local temporal.flexprice.local flexprice.local" \
  | sudo tee -a /etc/hosts

# Step 1: Install ingress-nginx (one time only)
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.1/deploy/static/provider/kind/deploy.yaml
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s

# Step 2: Namespace
kubectl create namespace flexprice --dry-run=client -o yaml | kubectl apply -f -

# Step 3: Helm dependencies
helm dependency update ./flexprice

# Step 4: Phase 1 — infrastructure
helm upgrade --install flexprice ./flexprice \
  -f ./flexprice/values.yaml -f ./values-local.yaml \
  --set api.enabled=false --set consumer.enabled=false \
  --set worker.enabled=false --set frontend.enabled=false \
  --set migration.enabled=false \
  -n flexprice --wait --timeout 10m

# Step 5: Phase 2 — migrations
helm upgrade flexprice ./flexprice \
  -f ./flexprice/values.yaml -f ./values-local.yaml \
  --set api.enabled=false --set consumer.enabled=false \
  --set worker.enabled=false --set frontend.enabled=false \
  --set migration.enabled=true \
  -n flexprice --wait --timeout 5m

# Step 6: Phase 3 — app
helm upgrade flexprice ./flexprice \
  -f ./flexprice/values.yaml -f ./values-local.yaml \
  --set api.enabled=true --set consumer.enabled=true \
  --set worker.enabled=true --set frontend.enabled=false \
  --set migration.enabled=false \
  -n flexprice --wait --timeout 5m

# Verify
curl http://api.flexprice.local/health
# Expected: {"status":"ok"}
```

The committed `helm/values-local.yaml` already contains all required overrides — do not modify it unless intentionally changing dev defaults.

---

## Apple Silicon (M-series Mac) — Everything You Need to Know

### Why kind fails on Apple Silicon

`kind` (Kubernetes-in-Docker) on Apple Silicon has two insurmountable problems:

1. **ARM64 kind cluster + Bitnami images**: All Bitnami Helm chart images (PostgreSQL, Kafka, Redis) are amd64-only on ECR Public. `kind load docker-image` on OrbStack transfers only a 317-byte manifest stub rather than actual image layers, so pods fail with `ImagePullBackOff` even after "loading."

2. **AMD64 kind cluster (via `DOCKER_DEFAULT_PLATFORM=linux/amd64`)**: Rosetta 2 translates amd64 user-space binaries perfectly, but the Kubernetes control plane (`kubeadm`, `kube-scheduler`, `kube-apiserver`) uses low-level syscalls and eBPF that Rosetta does not support. The cluster initializes partially then stalls.

**Solution: Use OrbStack's built-in Kubernetes.** OrbStack runs its own k8s node (Linux VM) with an arm64-native control plane. User-space amd64 pods (databases, brokers) run transparently via Rosetta 2 inside that VM — exactly what we need.

### OrbStack-specific behavior

| Behavior | Detail |
|----------|--------|
| Image sharing | OrbStack k8s and Docker share the same image store. `docker build` output is immediately available to pods — no `kind load` step needed. |
| Ingress IP | `192.168.139.2` (the OrbStack VM IP). Get the exact IP with `kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'` |
| Rosetta 2 | amd64 images work transparently in pods. Performance penalty is ~5–15% for CPU-bound workloads; negligible for I/O-bound services (databases). |
| Memory | OrbStack VM defaults to ~50% of host RAM. On a 16 GiB Mac this is 8 GiB. The full FlexPrice stack needs ~4–5 GiB with the optimized `values-local.yaml` limits. |
| Port 80/443 | OrbStack exposes the ingress-nginx controller directly on the OrbStack VM IP — no `kubectl port-forward` needed. Add the IP to `/etc/hosts` and hostnames resolve. |
| Docker socket | `/var/run/docker.sock` — same as Docker Desktop. Tools that use the Docker socket work identically. |

### Memory configuration

OrbStack VM memory can be changed in **OrbStack → Settings → Resources → Memory**. The FlexPrice stack fits in 6 GiB. If you have 16+ GiB, leaving the default is fine. If OrbStack defaults to less than 6 GiB, increase it before deploying.

---

## values-local.yaml — What It Does and Why

The file `helm/values-local.yaml` is committed and contains all dev-safe overrides. **Do not commit real credentials into this file.**

### Required passwords (chart fails without these)

`helm/flexprice/templates/app/secret.yaml` validates that all four are non-empty:

```yaml
secrets:
  encryptionKey: "dev-encryption-key-32-chars-here"
auth:
  provider: "flexprice"
  secret: "dev-auth-secret-local"
postgres:
  password: "flexprice123"
clickhouse:
  password: "flexprice123"
```

### ECR Public registry

Bitnami removed all versioned image tags from Docker Hub in November 2023. `public.ecr.aws/bitnami/` carries all versioned tags:

```yaml
global:
  security:
    allowInsecureImages: true   # Bitnami whitelist doesn't include ECR

postgresql:
  image:
    registry: public.ecr.aws
redis:
  image:
    registry: public.ecr.aws
kafka:
  image:
    registry: public.ecr.aws
```

### Memory limits

Default chart values exceed 6 GiB RAM on a single node. These overrides bring it under 5 GiB:

```yaml
clickhouse:
  standalone:
    resources:
      requests: { memory: "512Mi", cpu: "250m" }
      limits:   { memory: "1Gi",   cpu: "1"    }

postgresql:
  primary:
    resources:
      requests: { memory: "256Mi" }
      limits:   { memory: "512Mi" }

kafka:
  controller:
    replicaCount: 1
    resources:
      limits: { memory: "512Mi" }
  broker:
    resources:
      limits: { memory: "512Mi" }

temporal:
  elasticsearch: { enabled: false }  # -3× Elasticsearch pods (~1 GiB each)
  prometheus:    { enabled: false }  # -1 Prometheus pod (~300 MiB)
  grafana:       { enabled: false }  # -1 Grafana pod (~200 MiB)
```

### Temporal server config

Temporal server v1.30.3 changed config-loading: it reads an embedded Cassandra template by default and ignores the chart's ConfigMap unless told otherwise.

```yaml
temporal:
  server:
    setConfigFilePath: true      # injects TEMPORAL_SERVER_CONFIG_FILE_PATH env var
    configMapsToMount: "sprig"   # sprig template uses # enable-template marker server v1.30+ recognizes
```

Without these, all four Temporal pods (`frontend`, `history`, `matching`, `worker`) crash-loop with:
```
Unable to load configuration: Persistence.DataStores[default](value).Cassandra.Hosts: zero value
```

### Ingress hostnames

```yaml
ingress:
  hosts:
    - host: api.flexprice.local
      paths:
        - path: /
          pathType: Prefix

temporalIngress:
  host: temporal.flexprice.local
```

The chart defaults to `flexprice.example.com` which doesn't match `api.flexprice.local` in `/etc/hosts`.

---

## Known Failure Modes and Fixes

### Pods stuck in ImagePullBackOff

**Cause A: Bitnami Docker Hub images removed**
```bash
kubectl describe pod flexprice-postgresql-0 -n flexprice | grep "Failed to pull"
# failed to pull image "docker.io/bitnami/postgresql:16.x..."
```
Fix: Ensure `values-local.yaml` has `registry: public.ecr.aws` for postgresql/redis/kafka and `global.security.allowInsecureImages: true`.

**Cause B: App image not built**
```bash
kubectl describe pod flexprice-api-... -n flexprice | grep "not found"
# flexprice-app:local not found
```
Fix: Build the image — it's shared with OrbStack k8s automatically:
```bash
docker build -f Dockerfile.local -t flexprice-app:local .
# No kind load needed on OrbStack
```

### Temporal pods crash-loop with "Cassandra.Hosts: zero value"

```bash
kubectl logs -n flexprice flexprice-temporal-frontend-xxx | grep -E "error|Error"
# Unable to load configuration: Persistence.DataStores[default](value).Cassandra.Hosts: zero value
```
Fix: Add to `values-local.yaml` (already present in committed version):
```yaml
temporal:
  server:
    setConfigFilePath: true
    configMapsToMount: "sprig"
```
Then: `helm upgrade flexprice ./flexprice -f ./flexprice/values.yaml -f ./values-local.yaml -n flexprice`

### Helm "another operation is in progress"

```
Error: UPGRADE FAILED: another operation (install/upgrade/rollback) is in progress
```

A previous helm operation timed out without rolling back. The release is locked in `pending-install` or `pending-upgrade`.

```bash
# Find the pending secret
kubectl get secret -n flexprice -l "owner=helm,name=flexprice" \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.status}{"\n"}{end}'

# Delete whichever shows "pending-install" or "pending-upgrade"
kubectl delete secret sh.helm.release.v1.flexprice.vN -n flexprice
# (replace N with the version number from the output above)

# Retry the helm command
```

### Kafka controller quorum error after changing replicaCount

```
UnknownHostException: flexprice-kafka-controller-1.flexprice-kafka-controller-headless
ERROR [RaftManager id=0] Graceful shutdown of RaftClient failed: TimeoutException
```

The Kafka PVC still has KRaft metadata referencing 3 controllers. A single controller can't form quorum.

```bash
kubectl delete pod flexprice-kafka-controller-0 -n flexprice --force --grace-period=0
kubectl delete pvc data-flexprice-kafka-controller-0 -n flexprice
# StatefulSet recreates pod with fresh PVC
kubectl wait pod/flexprice-kafka-controller-0 -n flexprice --for=condition=ready --timeout=120s
```

This only happens when changing replica count on an existing cluster. Fresh deploys with `replicaCount: 1` work correctly.

### PostgreSQL OOMKilled repeatedly

```bash
kubectl describe pod flexprice-postgresql-0 -n flexprice | grep -A2 "Last State"
# Reason: OOMKilled  Exit Code: 137
```

**Cause**: ClickHouse at 4 GiB + Kafka at ~2.3 GiB (3 controllers) + Temporal monitoring (~1.5 GiB) leaves no room for PostgreSQL.

**Fix**: Ensure `values-local.yaml` sets `postgresql.primary.resources.limits.memory: "512Mi"` AND disables monitoring (see Memory Limits section). Both are pre-configured in the committed `values-local.yaml`.

If the OOMKill is during Temporal schema job: This is a cascade — postgres dies while Temporal schema init containers run. Just wait; the Job retries with exponential backoff and succeeds once postgres stabilizes.

### API returns 404 for all routes (ingress issue)

```bash
curl -v http://api.flexprice.local/health
# < HTTP/1.1 404 Not Found  (from nginx, not from the app)
```

**Cause A: Wrong IP in /etc/hosts**
OrbStack ingress IP is `192.168.139.2`, not `127.0.0.1`. The script notes say `127.0.0.1` (for kind), which is wrong for OrbStack.

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
# 192.168.139.2  (or similar)

# /etc/hosts should have:
# 192.168.139.2  api.flexprice.local temporal.flexprice.local
```

**Cause B: Ingress hostname mismatch**
Check the ingress resource:
```bash
kubectl get ingress -n flexprice
# HOST column should show "api.flexprice.local"
# If it shows "flexprice.example.com", values-local.yaml ingress override is missing
```

**Cause C: API pods not running**
```bash
kubectl get pods -n flexprice | grep api
# flexprice-api-xxx   0/1   CrashLoopBackOff
kubectl logs -n flexprice -l app.kubernetes.io/component=api --tail=50
```

### Migrations fail ("postgres connection refused")

Migration pods try to connect to postgres during a startup window. If postgres is still initializing (first install) or restarting (OOMKill), migrations fail.

```bash
kubectl get jobs -n flexprice
kubectl describe job flexprice-migrate -n flexprice | tail -20
# "BackoffLimitExceeded" means it failed too many times
```

Fix: Delete the job and rerun migrations after postgres is healthy:
```bash
kubectl delete job flexprice-migrate -n flexprice 2>/dev/null || true
kubectl wait pod/flexprice-postgresql-0 -n flexprice --for=condition=ready --timeout=120s
# Then re-run the Phase 2 helm upgrade
```

### Temporal schema job fails

```bash
kubectl get pods -n flexprice | grep schema
# flexprice-temporal-schema-xxx  0/1  Error
```

Same root cause as migrations above — postgres was restarting. The Job retries automatically. Watch it:
```bash
kubectl get events -n flexprice --sort-by='.lastTimestamp' | tail -20
# Look for "Backoff" events on the schema job
```

It will succeed within 2–3 retries once postgres is stable (usually within 5 minutes).

---

## Verification Steps

After the three-phase deploy completes:

```bash
# 1. All pods running
kubectl get pods -n flexprice
# Expected: All 1/1 Running (or Completed for jobs)

# 2. API health
curl http://api.flexprice.local/health
# Expected: {"status":"ok"}

# 3. Auth route (note: uses /auth/signup not /v1/auth)
curl -s -X POST http://api.flexprice.local/auth/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","password":"testpass123"}' | jq .

# 4. Temporal UI (open in browser)
open http://temporal.flexprice.local
# Temporal workflows dashboard should appear

# 5. PostgreSQL direct access
kubectl exec -it flexprice-postgresql-0 -n flexprice -- \
  psql -U flexprice -d flexprice -c '\dt'

# 6. ClickHouse direct access
kubectl exec -it flexprice-clickhouse-0 -n flexprice -- \
  clickhouse-client --user=default --password=flexprice123 --database=flexprice \
  --query="SHOW TABLES"
```

---

## Day-to-Day Operations

### Rebuild and redeploy the app image

```bash
# Rebuild (automatically available to OrbStack k8s)
docker build -f Dockerfile.local -t flexprice-app:local .

# Rolling restart (faster than helm upgrade)
kubectl rollout restart deployment flexprice-api flexprice-consumer flexprice-worker -n flexprice
kubectl rollout status deployment flexprice-api -n flexprice
```

### View logs

```bash
kubectl logs -n flexprice -l app.kubernetes.io/component=api -f
kubectl logs -n flexprice -l app.kubernetes.io/component=consumer -f
kubectl logs -n flexprice -l app.kubernetes.io/component=worker -f
# Show last 100 lines from all app pods:
kubectl logs -n flexprice --selector=app.kubernetes.io/name=flexprice --tail=100
```

### Run a specific migration

```bash
kubectl exec -it flexprice-postgresql-0 -n flexprice -- \
  psql -U flexprice -d flexprice
```

### Check resource usage

```bash
kubectl top pods -n flexprice
kubectl top nodes
```

### Bootstrap Temporal namespace (if missing)

The `default` Temporal namespace must exist for workflows to run:
```bash
kubectl exec -n flexprice deploy/flexprice-temporal-admintools -- \
  temporal operator namespace describe --namespace default 2>&1 | grep -q "not found" && \
kubectl exec -n flexprice deploy/flexprice-temporal-admintools -- \
  temporal operator namespace create --namespace default --retention 72h
```

---

## Teardown

```bash
# Remove the helm release and all k8s resources
helm uninstall flexprice -n flexprice
kubectl delete namespace flexprice

# PVCs are not deleted by helm uninstall — clean them up:
kubectl delete pvc -n flexprice --all  # run before namespace delete, or they'll linger
```

The OrbStack k8s cluster persists (it's always-on). Reinstalling is just re-running the three helm phases.

---

## values-local.yaml Reference (full file)

The committed `helm/values-local.yaml`:

```yaml
global:
  security:
    allowInsecureImages: true

ingress:
  hosts:
    - host: api.flexprice.local
      paths:
        - path: /
          pathType: Prefix

temporalIngress:
  host: temporal.flexprice.local

secrets:
  encryptionKey: "dev-encryption-key-32-chars-here"

auth:
  provider: "flexprice"
  secret: "dev-auth-secret-local"

postgres:
  password: "flexprice123"

clickhouse:
  password: "flexprice123"
  standalone:
    resources:
      requests:
        memory: "512Mi"
        cpu: "250m"
      limits:
        memory: "1Gi"
        cpu: "1"

postgresql:
  image:
    registry: public.ecr.aws
  primary:
    resources:
      requests:
        memory: "256Mi"
      limits:
        memory: "512Mi"

redis:
  image:
    registry: public.ecr.aws

kafka:
  image:
    registry: public.ecr.aws
  controller:
    replicaCount: 1
    resources:
      limits:
        memory: "512Mi"
  broker:
    resources:
      limits:
        memory: "512Mi"

temporal:
  elasticsearch:
    enabled: false
  prometheus:
    enabled: false
  grafana:
    enabled: false
  server:
    setConfigFilePath: true
    configMapsToMount: "sprig"

frontend:
  enabled: false
```

---

## Quick Reference

```bash
# Pods status
kubectl get pods -n flexprice

# Events (useful for diagnosing failures)
kubectl get events -n flexprice --sort-by='.lastTimestamp' | tail -30

# Break helm pending lock
kubectl delete secret -n flexprice \
  $(kubectl get secret -n flexprice -l "owner=helm,name=flexprice" \
    -o jsonpath='{.items[?(@.metadata.labels.status=="pending-upgrade")].metadata.name}' \
    2>/dev/null || \
   kubectl get secret -n flexprice -l "owner=helm,name=flexprice" \
    -o jsonpath='{.items[?(@.metadata.labels.status=="pending-install")].metadata.name}')

# Force-delete stuck pod
kubectl delete pod <name> -n flexprice --force --grace-period=0

# Fix Kafka quorum (after replicaCount change)
kubectl delete pod flexprice-kafka-controller-0 -n flexprice --force --grace-period=0
kubectl delete pvc data-flexprice-kafka-controller-0 -n flexprice

# Full teardown
helm uninstall flexprice -n flexprice && kubectl delete namespace flexprice
```

---

## Relevant Files

| File | Purpose |
|------|---------|
| `helm/values-local.yaml` | All local dev overrides — committed, use as-is |
| `helm/local-up.sh` | 5-phase deploy script (kind-oriented; use manual steps above for OrbStack) |
| `helm/flexprice/values.yaml` | Chart defaults — do not modify for local dev |
| `helm/docs/local-setup-troubleshooting.md` | Extended troubleshooting with root-cause analysis |
| `Dockerfile.local` | Multi-stage Go build for local image |
