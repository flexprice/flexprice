# FlexPrice Helm — Troubleshooting

Common production failure modes and how to debug them. For local-kind issues,
see [`local-setup-troubleshooting.md`](local-setup-troubleshooting.md).

## First-pass diagnostics

```bash
NS=flexprice
REL=flexprice

# Are all pods Running?
kubectl get pods -n "$NS"

# Anything looping / crash-looping?
kubectl get pods -n "$NS" -o wide --watch-only=false | awk '$3 ~ /Crash|Error|ImagePull/'

# Most recent events (sorted)
kubectl get events -n "$NS" --sort-by=.lastTimestamp | tail -30

# Pod-level logs (last 100 lines, all containers)
for p in $(kubectl get pods -n "$NS" -l app.kubernetes.io/name=flexprice -o name); do
  echo "=== $p ==="
  kubectl logs -n "$NS" "$p" --tail=100 --all-containers=true
done

# Migration job status (Helm hook)
kubectl get jobs -n "$NS" -l app.kubernetes.io/component=migration
kubectl logs  -n "$NS" -l app.kubernetes.io/component=migration --tail=200

# Temporal bootstrap status
kubectl get jobs -n "$NS" -l app.kubernetes.io/component=temporal-bootstrap
kubectl logs  -n "$NS" -l app.kubernetes.io/component=temporal-bootstrap --tail=100
```

## Symptoms → fixes

### `ErrImagePull` / `ImagePullBackOff`

Default image is `ghcr.io/flexprice/flexprice:<appVersion>`. Confirm:

```bash
helm get values "$REL" -n "$NS" | grep -A3 '^image:'
kubectl describe pod -n "$NS" <pod> | grep -A3 'Failed to pull'
```

- Wrong tag → set `image.tag` explicitly to a published version.
- Private registry → set `imagePullSecrets: [{name: my-secret}]` and create the secret.
- EKS + ECR → use IRSA on the node IAM role (`AmazonEC2ContainerRegistryReadOnly`), not a pull secret.

### Migration Job fails

```bash
kubectl logs -n "$NS" job/${REL}-migration --tail=300
```

Common causes:
- Postgres unreachable from cluster → check `postgres.host`/`postgres.port`, NetworkPolicy.
- Wrong password → recreate the existingSecret with the correct `postgres-password` key.
- `relation already exists` on a re-run → migration is idempotent; re-run by deleting the Job and triggering `helm upgrade`.

### Consumer not committing offsets

```bash
kubectl logs -n "$NS" deploy/${REL}-consumer --tail=200 | grep -i 'kafka\|offset\|rebalance'
```

- Kafka brokers unreachable → check `kafkaConfig.brokers`, TLS/SASL flags.
- Consumer pod restarting → look at `terminationGracePeriodSeconds` (must exceed your longest message processing time).
- Rebalance storm during rolling update → confirm `consumer.preStop.enabled=true`.

### ClickHouse out-of-memory at query time

Error: `Memory limit (total) exceeded` or `Memory limit (for query) exceeded`.

- Lower `clickhouse.maxMemoryUsageGB` (default 90) to fit the cluster's available RAM.
- For ClickHouse Cloud, the server-side `max_memory_usage` setting overrides the client side.

### Temporal namespace not found at startup

```bash
kubectl logs -n "$NS" deploy/${REL}-worker --tail=100 | grep -i namespace

# Confirm which namespaces actually exist
kubectl exec -n "$NS" deploy/${REL}-temporal-admintools -- \
  temporal operator namespace list | grep "NamespaceInfo.Name"
# Expect: temporal-system AND default. Missing 'default' → seed it.
```

- Bootstrap Job missed → re-run `helm upgrade --reuse-values $REL ./flexprice -n $NS` (post-install hook fires again).
- Bootstrap Job succeeded but `default` is still missing (e.g. it ran before Temporal frontend was Ready, or `provision.sh` aborted mid-Phase-1) → seed manually:

  ```bash
  kubectl exec -n "$NS" deploy/${REL}-temporal-admintools -- \
    temporal operator namespace create --namespace default --retention 72h
  ```

- Temporal Cloud → namespaces must be created out-of-band; set `temporalConfig.bootstrapNamespaces: []` to disable the Job.

### Pods OOMKilled

```bash
kubectl get pods -n "$NS" -o json | jq -r '.items[] | select(.status.containerStatuses[]?.lastState.terminated.reason=="OOMKilled") | .metadata.name'
```

- Bump `resources.limits.memory` for the affected component (api / consumer / worker).
- Check `clickhouse.maxMemoryUsageGB` if api pods are OOMing during analytics queries.

### Helm upgrade hangs / `release in progress`

```bash
helm history "$REL" -n "$NS"
helm rollback "$REL" <last-good-revision> -n "$NS"
```

If stuck pre-install hook:
```bash
kubectl delete job -n "$NS" "${REL}-migration"
helm upgrade --reuse-values "$REL" ./flexprice -n "$NS"
```

### Probe failures

```bash
kubectl describe pod -n "$NS" <pod> | grep -A5 'Liveness\|Readiness\|Startup'
```

- `/health` returning non-200 → app failed to connect to a dependency. Check app logs.
- Probe timing out → bump `livenessProbe.timeoutSeconds` if your DB has high cold-start latency.

## Log locations

| Component   | Get logs                                                             |
|-------------|----------------------------------------------------------------------|
| api         | `kubectl logs -n $NS deploy/$REL-api`                                |
| consumer    | `kubectl logs -n $NS deploy/$REL-consumer`                           |
| worker      | `kubectl logs -n $NS deploy/$REL-worker`                             |
| migration   | `kubectl logs -n $NS job/$REL-migration`                             |
| temporal bootstrap | `kubectl logs -n $NS job/$REL-temporal-bootstrap`             |

For long-term log retention, configure OTEL output via `logging.otel.*` values
(see [`values-prod.example.yaml`](../values-prod.example.yaml)).

## Asking for help

When opening an issue, please include:

```bash
helm version
kubectl version --short
helm get values "$REL" -n "$NS" > /tmp/values.yaml      # redact secrets first
kubectl get pods,svc,ingress -n "$NS" -o wide
kubectl describe pod -n "$NS" <failing-pod>
```
