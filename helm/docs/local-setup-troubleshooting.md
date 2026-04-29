# FlexPrice Local Kubernetes Setup — Troubleshooting & Enhancement Guide

This document records every issue encountered running the FlexPrice Helm chart locally on **Apple Silicon (M-series) Mac + OrbStack**, along with their root causes and fixes. It doubles as an enhancement guide for making the setup more reliable.

---

## Quick-Start (TL;DR)

```bash
# Prerequisites: OrbStack running, kubectl context set to OrbStack k8s
brew install helm

cd helm/

# Add to /etc/hosts (one time)
sudo sh -c 'echo "$(kubectl get svc -n ingress-nginx ingress-nginx-controller \
  -o jsonpath="{.status.loadBalancer.ingress[0].ip}" 2>/dev/null \
  || echo 192.168.139.2) api.flexprice.local temporal.flexprice.local" >> /etc/hosts'

# Install ingress-nginx (one time)
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.10.1/deploy/static/provider/kind/deploy.yaml
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=120s

# Create namespace and deploy
kubectl create namespace flexprice --dry-run=client -o yaml | kubectl apply -f -
helm dependency update ./flexprice
helm upgrade --install flexprice ./flexprice \
  -f ./flexprice/values.yaml -f ./values-local.yaml \
  --set api.enabled=false --set consumer.enabled=false \
  --set worker.enabled=false --set frontend.enabled=false \
  --set migration.enabled=false \
  -n flexprice --wait --timeout 10m

# Run migrations
helm upgrade flexprice ./flexprice \
  -f ./flexprice/values.yaml -f ./values-local.yaml \
  --set api.enabled=false --set consumer.enabled=false \
  --set worker.enabled=false --set frontend.enabled=false \
  --set migration.enabled=true \
  -n flexprice --wait --timeout 5m

# Deploy app
helm upgrade flexprice ./flexprice \
  -f ./flexprice/values.yaml -f ./values-local.yaml \
  --set api.enabled=true --set consumer.enabled=true \
  --set worker.enabled=true --set frontend.enabled=false \
  --set migration.enabled=false \
  -n flexprice --wait --timeout 5m

# Verify
curl http://api.flexprice.local/health
# → {"status":"ok"}
```

---

## Environment

| Item | Details |
|------|---------|
| Machine | Apple Silicon (M-series, aarch64) |
| OS | macOS 15.x |
| Container runtime | OrbStack (replaces Docker Desktop) |
| Kubernetes | OrbStack built-in k8s cluster |
| Helm | 4.x (via `brew install helm`) |
| Chart | `helm/flexprice/` — Chart.yaml v1.0.0 |
| Temporal | server v1.30.3, chart v0.74.0 |

---

## Issue Index

1. [Required values not documented — chart fails without passwords](#1-required-values-not-documented)
2. [Bitnami images removed from Docker Hub](#2-bitnami-images-removed-from-docker-hub)
3. [kind cannot run on Apple Silicon (ARM64)](#3-kind-cannot-run-on-apple-silicon)
4. [OrbStack k8s: no need for `kind load`](#4-orbstack-k8s-shares-docker-daemon)
5. [Bitnami security check rejects ECR registry](#5-bitnami-security-check-rejects-ecr)
6. [Temporal deployed Elasticsearch unnecessarily](#6-temporal-deploys-elasticsearch-by-default)
7. [Temporal server ignores chart ConfigMap (reads embedded Cassandra template)](#7-temporal-server-reads-embedded-cassandra-config)
8. [Memory pressure: OOMKill loops on 6 GiB node](#8-memory-pressure-on-6-gib-node)
9. [PostgreSQL OOMKilled under Temporal schema load](#9-postgresql-oomkilled-during-temporal-schema)
10. [Kafka controller quorum broken after replica reduction](#10-kafka-controller-quorum-breaks-when-reducing-replicas)
11. [Helm `--wait` timing out due to pending-upgrade lock](#11-helm-pending-upgrade-lock)
12. [Ingress hostname mismatch](#12-ingress-hostname-mismatch)
13. [Pipeline exit-code masking in local-up.sh](#13-pipeline-exit-code-masking)

---

## 1. Required values not documented

**Symptom**
```
Error: secrets.existingSecret not set and clickhouse.password is empty
```

**Root cause**
`helm/flexprice/templates/app/secret.yaml` requires four plaintext values when no existing secret is configured:
- `postgres.password`
- `clickhouse.password`
- `auth.secret`
- `secrets.encryptionKey`

The defaults in `values.yaml` are empty strings. The chart fails with a REQUIRED validation error unless they are all supplied.

**Fix**
`values-local.yaml` (committed) supplies all four:
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

**Enhancement needed**
`values.yaml` should document these as REQUIRED with a comment `# REQUIRED: set this` and the README should call them out explicitly.

---

## 2. Bitnami images removed from Docker Hub

**Symptom**
```
failed to pull image "docker.io/bitnami/redis:7.4.3-debian-12-r0": not found
failed to pull image "docker.io/bitnami/kafka:...": not found
```

**Root cause**
Bitnami removed all versioned image tags from Docker Hub in November 2023. Only the `:latest` tag remains for redis/postgresql. Kafka images are completely absent from Docker Hub.

**Fix**
Pull from ECR Public (`public.ecr.aws/bitnami/`) which carries all versioned tags:
```yaml
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

**Apple Silicon note**
All Bitnami images on ECR Public are **amd64 only**. OrbStack transparently runs amd64 containers via Rosetta 2 on Apple Silicon, so they just work on OrbStack k8s. On native arm64 k8s (kind on ARM) they do not work — see Issue 3.

**Enhancement needed**
The default `values.yaml` should ship with `registry: public.ecr.aws` for all three Bitnami subcharts. All the commented examples in values.yaml mention this but the actual defaults still point to Docker Hub.

---

## 3. kind cannot run on Apple Silicon

**Symptom**
```
[ERROR] preflight: running with swap on is not supported
kube-scheduler refused connection
kube-apiserver refused connection
```
Or with `DOCKER_DEFAULT_PLATFORM=linux/amd64`:
```
kubeadm init failed — k8s control plane binaries do not work under Rosetta
```

**Root cause**
- ARM64 kind cluster: Bitnami amd64 images cannot be loaded via `kind load docker-image` on OrbStack (the tool transfers only a 317-byte manifest, not actual layers).
- AMD64 kind cluster: Rosetta 2 does not support all syscalls needed by `kubeadm` and the k8s control plane.

**Fix / Recommendation**
**Use OrbStack's built-in Kubernetes** instead of kind on Apple Silicon Macs:
1. Enable OrbStack k8s in the OrbStack settings UI
2. Run `kubectl config use-context orbstack`
3. No cluster creation step needed; the cluster is always running

`local-up.sh` currently requires kind. The script should be updated to support an `--orbstack` flag or auto-detect OrbStack.

**If you need to use kind**
Use an Intel Mac or a Linux x86_64 machine. kind works perfectly on those.

---

## 4. OrbStack k8s shares Docker daemon

**Impact**
When running on OrbStack k8s (instead of kind), you do **not** need `kind load docker-image`. Any image built with `docker build` is immediately available to OrbStack k8s pods — the two share the same Docker socket.

`local-up.sh` has this pattern:
```bash
docker build -t flexprice-app:local ...
kind load docker-image flexprice-app:local --name "$CLUSTER_NAME"
```

On OrbStack, the `kind load` step should be skipped. The Helm values already set `imagePullPolicy: IfNotPresent` so the image is used directly from the local daemon.

**Fix**
Update `local-up.sh` to detect OrbStack and skip the `kind load` steps:
```bash
if [[ "$KUBECTL_CONTEXT" == "orbstack" ]]; then
  info "OrbStack detected — skipping kind load (images shared via Docker daemon)"
else
  kind load docker-image flexprice-app:local --name "$CLUSTER_NAME"
fi
```

---

## 5. Bitnami security check rejects ECR

**Symptom**
Helm upgrade succeeds (exit code 0 due to pipeline masking — see Issue 13) but pods fail with:
```
Unrecognized images: public.ecr.aws/bitnami/redis:7.4.3-debian-12-r0
```

**Root cause**
Bitnami charts validate that images come from a known-safe registry list. ECR Public is not on the list.

**Fix**
```yaml
global:
  security:
    allowInsecureImages: true
```

This is required in `values-local.yaml` whenever using ECR Public or any non-standard registry for Bitnami subcharts.

---

## 6. Temporal deploys Elasticsearch by default

**Symptom**
Phase 1 helm install times out waiting for:
```
resource StatefulSet/flexprice/elasticsearch-master not ready. Ready: 0/3
```
Three Elasticsearch replicas (each requesting ~1 GiB RAM) deploy even though the chart configures PostgreSQL visibility.

**Root cause**
The `temporalio/temporal` Helm chart (v0.74.0) sets `elasticsearch.enabled: true` by default. The FlexPrice `values.yaml` correctly configures Temporal to use PostgreSQL for visibility, but does not explicitly disable Elasticsearch.

**Fix**
```yaml
temporal:
  elasticsearch:
    enabled: false
```

---

## 7. Temporal server reads embedded Cassandra config

**Symptom**
All four Temporal server pods (`frontend`, `history`, `matching`, `worker`) crash-loop with:
```
Loading configuration from environment variables only
Processing config file as template; filename=config_template_embedded.yaml
Unable to load configuration: Persistence.DataStores[default](value).Cassandra.Hosts: zero value.
```

**Root cause**
Temporal server v1.30.3 changed its config-loading behavior. By default it reads an **embedded** config template that uses Cassandra. The Helm chart generates a PostgreSQL-configured ConfigMap and mounts it at `/etc/temporal/config/config_template.yaml`, but the server ignores this file unless explicitly told to use it.

The chart has two config template formats:
- `dockerize`: Go template with `{{ .Env.X }}` syntax (for older images that used dockerize)  
- `sprig`: Uses `{{ env "X" | quote }}` + `# enable-template` marker (for server v1.30+)

The chart defaults to `configMapsToMount: "dockerize"` which is wrong for v1.30+.

**Fix**
```yaml
temporal:
  server:
    setConfigFilePath: true      # injects TEMPORAL_SERVER_CONFIG_FILE_PATH env var
    configMapsToMount: "sprig"   # uses sprig template that server v1.30+ processes natively
```

**Verification**
After fix, server log shows:
```json
{"msg":"Starting server for services","value":{"frontend":{}}}
```
instead of the Cassandra error.

**Enhancement needed**
The FlexPrice `values.yaml` `temporal:` section should default to these settings since it pins `server.image.tag: 1.30.3`.

---

## 8. Memory pressure on 6 GiB node

**Symptom**
Pods (especially PostgreSQL) repeatedly get OOMKilled (exit code 137) after running for 1–3 minutes.

**Root cause**
Default memory limits sum to ~127% of the 6 GiB OrbStack node:
- ClickHouse: 4 GiB (default in values.yaml)
- Kafka controller × 3: ~768 MiB each = 2.3 GiB
- Temporal monitoring (Prometheus + Grafana + Alertmanager): ~1.5 GiB
- Everything else: ~2 GiB

**Fixes applied in `values-local.yaml`**

| Service | Before | After |
|---------|--------|-------|
| ClickHouse limit | 4 GiB | 1 GiB |
| Kafka controller replicas | 3 | 1 |
| Kafka controller limit | 768 MiB | 512 MiB |
| Kafka broker limit | unlimited | 512 MiB |
| PostgreSQL limit | 192 MiB | 512 MiB |
| Prometheus | enabled | **disabled** |
| Grafana | enabled | **disabled** |
| Elasticsearch | enabled | **disabled** |

**Result**
Memory limits reduced from ~127% to ~101%, node MemoryPressure resolved.

**Enhancement needed**
`values-local.yaml` should be shipped as a template with these resource limits pre-configured. The current defaults are appropriate for production but make the chart impossible to run on a developer laptop.

---

## 9. PostgreSQL OOMKilled during Temporal schema

**Symptom**
PostgreSQL pod repeatedly OOMKilled (exit code 137) specifically when the `flexprice-temporal-schema-*` Job runs its init containers (which run SQL migrations against postgres).

**Root cause**
The Bitnami PostgreSQL chart defaults to a 192 MiB memory limit. When Temporal's schema migration tool runs concurrent SQL operations (6 init containers, some running in parallel), PostgreSQL needs significantly more than 192 MiB.

**Fix**
```yaml
postgresql:
  primary:
    resources:
      requests:
        memory: "256Mi"
      limits:
        memory: "512Mi"
```

**Note**
This is a cascading issue: if PostgreSQL is OOMKilled while a Temporal schema Job init container is running, that init container fails. The Job retries with exponential backoff. The retry will succeed once PostgreSQL comes back up, but this creates confusing failure noise.

---

## 10. Kafka controller quorum breaks when reducing replicas

**Symptom**
After reducing `kafka.controller.replicaCount` from 3 to 1, the single kafka-controller pod crashes with:
```
UnknownHostException: flexprice-kafka-controller-1.flexprice-kafka-controller-headless...
UnknownHostException: flexprice-kafka-controller-2.flexprice-kafka-controller-headless...
ERROR [RaftManager id=0] Graceful shutdown of RaftClient failed: TimeoutException
```

**Root cause**
The KRaft metadata stored in the Kafka controller PVC still references all three controller IDs (0, 1, 2). A single controller cannot form a quorum (needs majority of 3 = 2 nodes) and continuously tries to contact the missing controllers.

**Fix**
Delete the stale PVC to force fresh initialization:
```bash
kubectl delete pod flexprice-kafka-controller-0 -n flexprice --force --grace-period=0
kubectl delete pvc data-flexprice-kafka-controller-0 -n flexprice
# StatefulSet recreates pod with fresh PVC; initializes 1-controller cluster
kubectl wait pod/flexprice-kafka-controller-0 -n flexprice \
  --for=condition=ready --timeout=120s
```

**Note**
This is only needed when changing replica count on an existing cluster. Fresh installs (no PVC) work correctly with 1 replica.

---

## 11. Helm `pending-upgrade` lock

**Symptom**
```
Error: UPGRADE FAILED: another operation (install/upgrade/rollback) is in progress
```

**Root cause**
When a helm operation times out (e.g., `--wait --timeout 10m` and pods don't become ready), helm leaves the release in `pending-install` or `pending-upgrade` state. Subsequent helm commands fail.

**Diagnosis**
```bash
helm status flexprice -n flexprice | grep STATUS
# STATUS: pending-install  (or pending-upgrade)
```

**Fix**
Identify and delete the pending helm release secret:
```bash
kubectl get secret -n flexprice -l "owner=helm,name=flexprice" -o name
# Find the one with status=pending-install or pending-upgrade in its labels
kubectl get secret sh.helm.release.v1.flexprice.v3 \
  -n flexprice -o jsonpath='{.metadata.labels.status}'

# Delete it to break the lock
kubectl delete secret sh.helm.release.v1.flexprice.v3 -n flexprice
```

---

## 12. Ingress hostname mismatch

**Symptom**
`http://api.flexprice.local/health` returns nginx 404 even though the API pod is healthy.

**Root cause**
`values.yaml` defaults ingress host to `flexprice.example.com`. The local-up.sh documentation says to add `api.flexprice.local` to `/etc/hosts`, but the ingress isn't configured to match that hostname.

**Fix in `values-local.yaml`**
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

**Note on route prefix**
The API's health endpoint is at `/health` (not `/v1/health`). The ingress routes everything at `/` to the API service, so:
- `http://api.flexprice.local/health` → `{"status":"ok"}` ✅
- `http://api.flexprice.local/v1/health` → 404 (no such route) ✅ expected

---

## 13. Pipeline exit-code masking in local-up.sh

**Symptom**
`local-up.sh` appears to succeed (prints "Phase 1 complete") even when helm returns an error.

**Root cause**
```bash
helm upgrade ... 2>&1 | tail -5; echo "exit: $?"
```
The `echo "exit: $?"` captures the exit code of `tail`, not of `helm`. `tail` exits 0 if it successfully printed output, regardless of whether helm failed.

**Fix**
Either use `set -o pipefail` (already present in the script header, but only applies to the outermost pipeline) or capture the exit code explicitly:
```bash
helm upgrade ... 2>&1 | tee /tmp/helm-out.log
HELM_EXIT=${PIPESTATUS[0]}
tail -5 /tmp/helm-out.log
[[ $HELM_EXIT -eq 0 ]] || die "helm upgrade failed (exit $HELM_EXIT)"
```

Or simply don't pipe helm output at all:
```bash
helm upgrade ... || die "helm upgrade failed"
```

---

## Enhancement Roadmap

### Must-fix before calling this "one-click"

1. **Update `local-up.sh` to support OrbStack** (skip `kind` cluster creation, skip `kind load`). Add auto-detection or `--orbstack` flag.
2. **Ship a `values-local.yaml` template** (committed — see `helm/values-local.yaml`). All required passwords, ECR registries, Temporal config, and resource limits pre-configured.
3. **Fix `values.yaml` Bitnami image registry** — default to `public.ecr.aws` for `postgresql.image.registry`, `redis.image.registry`, `kafka.image.registry`.
4. **Fix `values.yaml` Temporal config defaults** — set `temporal.server.setConfigFilePath: true` and `temporal.server.configMapsToMount: "sprig"` since the chart pins server v1.30.3.
5. **Fix memory limits** — ClickHouse 4 GiB is too high for local dev. Default should be 1 GiB or parameterized.
6. **Document required values** in the README with explicit call-outs for `postgres.password`, `clickhouse.password`, `auth.secret`, `secrets.encryptionKey`.

### Nice-to-have

7. **Add `/etc/hosts` instruction to local-up.sh** — detect the OrbStack ingress IP and print the exact `echo` command.
8. **Temporal UI ingress** — add `temporalIngress` to the values-local.yaml and test it.
9. **Health check script** — a quick `verify-local.sh` that hits each service's health endpoint and reports pass/fail.
10. **Teardown script** — `helm uninstall flexprice -n flexprice && kubectl delete namespace flexprice` with PVC cleanup.

---

## OrbStack-Specific Notes

### OrbStack Kubernetes IP
OrbStack's built-in k8s cluster uses `192.168.139.2` as its ingress IP (may vary, check with `kubectl get svc -n ingress-nginx`). Add to `/etc/hosts`:
```
192.168.139.2 api.flexprice.local temporal.flexprice.local flexprice.local
```

### OrbStack Docker / k8s image sharing
Docker images built on the host are immediately available to OrbStack k8s pods. No `kind load` needed. This is because OrbStack k8s and Docker share the same containerd/OCI image store.

### Rosetta 2 and amd64 containers
OrbStack runs amd64 container images transparently via Apple's Rosetta 2 translation layer. This works for **user space processes** (databases, message brokers, app servers). It does **not** work for the k8s control plane itself — which is why you can't run an amd64 kind cluster on Apple Silicon.

### Memory allocation
OrbStack's default VM gets ~50% of host RAM. On a 16 GiB MacBook, the VM gets ~8 GiB. The FlexPrice stack with all optimizations fits comfortably in 6 GiB. OrbStack VM memory can be configured in **Settings → Resources**.

### Ports 80/443
OrbStack's k8s exposes the ingress-nginx controller on the host directly (no port-forwarding needed). The ingress load balancer gets a real IP that you can add to `/etc/hosts`.

---

## Quick Reference Commands

```bash
# Check all pods
kubectl get pods -n flexprice

# API health
curl http://api.flexprice.local/health

# Temporal UI
open http://temporal.flexprice.local

# PostgreSQL direct access
kubectl exec -it flexprice-postgresql-0 -n flexprice -- \
  psql -U flexprice -d flexprice

# ClickHouse direct access
kubectl exec -it flexprice-clickhouse-0 -n flexprice -- \
  clickhouse-client --user=default --password=flexprice123 --database=flexprice

# View API logs
kubectl logs -n flexprice -l app.kubernetes.io/component=api -f

# Restart app pods (after image rebuild)
kubectl rollout restart deployment flexprice-api flexprice-consumer flexprice-worker -n flexprice

# Break helm pending lock
kubectl delete secret -n flexprice \
  $(kubectl get secret -n flexprice -l "owner=helm,name=flexprice" \
    -o jsonpath='{.items[?(@.metadata.labels.status=="pending-upgrade")].metadata.name}')

# Teardown
helm uninstall flexprice -n flexprice
kubectl delete namespace flexprice
```
