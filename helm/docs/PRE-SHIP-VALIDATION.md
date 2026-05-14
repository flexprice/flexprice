# Pre-Ship Validation Procedure

Runbook for §14 of [`PRODUCTION_READINESS.md`](../PRODUCTION_READINESS.md).
Every chart release candidate must pass all six gates before being tagged.

The procedure assumes:
- A staging Kubernetes cluster (EKS preferred, kind acceptable for smoke).
- Managed Postgres / ClickHouse / Kafka / Redis / Temporal endpoints, **separate**
  from production (use a dedicated `flexprice-staging-rds`, etc.).
- The release candidate chart packaged: `helm package helm/flexprice -d /tmp/`.

```bash
NS=flexprice-rc
REL=flexprice-rc
RC_VERSION=2.0.0-rc.1
CHART=/tmp/flexprice-${RC_VERSION}.tgz
```

## Gate 1 — End-to-end smoke deploy

```bash
helm install "$REL" "$CHART" \
  -n "$NS" --create-namespace \
  -f staging-values.yaml \
  --timeout 10m --wait

helm test "$REL" -n "$NS"
```

**Pass criteria**
- All Deployments reach `Available` within timeout.
- Migration Job exits 0.
- Temporal bootstrap Job exits 0.
- `helm test` returns Success.
- Synthetic event ingest → invoice generation completes within 5 min.

## Gate 2 — Failover

Kill one pod of each component, verify the deployment heals and processes
continue:

```bash
for c in api consumer worker; do
  POD=$(kubectl get pod -n "$NS" -l app.kubernetes.io/component=$c -o name | head -1)
  kubectl delete "$POD" -n "$NS"
done

# Watch for replacement pods to reach Ready within 60s
kubectl get pods -n "$NS" --watch
```

**Pass criteria**
- New pods Ready within 60 s.
- No 5xx spike during the kill window (check ingress logs or synthetic probe).
- Consumer rebalance completes without duplicate processing
  (`flexprice_events_processed_total` counter increases monotonically with no
  retroactive gap).

## Gate 3 — Upgrade with zero downtime

```bash
# Install vN
helm install "$REL" "/tmp/flexprice-${PREVIOUS}.tgz" -n "$NS" -f staging-values.yaml --wait

# Drive synthetic traffic in the background (e.g. hey, vegeta).
hey -z 5m -c 10 "https://$(staging api host)/health" &

# Upgrade to vN+1
helm upgrade "$REL" "$CHART" -n "$NS" -f staging-values.yaml --wait --timeout 10m

# Stop synthetic traffic, inspect results
```

**Pass criteria**
- Synthetic traffic shows zero failed requests (`p99 < 500ms`, `0 5xx`).
- New ReplicaSet rolls out cleanly (`maxUnavailable: 0` honored for api).
- Migration Job either no-ops or completes within 2 min.

## Gate 4 — Rollback

```bash
helm rollback "$REL" 1 -n "$NS" --wait --timeout 10m
helm test "$REL" -n "$NS"
```

**Pass criteria**
- Rollback completes within timeout.
- `helm test` passes.
- Schema rollback NOT required (forward-only migrations); document any
  manually-required DB rollback steps in the per-release notes.

## Gate 5 — Load test

```bash
# Drive realistic event volume via the load script
./scripts/load-test-events.sh --rate 1000/s --duration 30m --target $REL-api.$NS:80
```

**Pass criteria**
- p95 event-ingest latency < 200 ms over the run.
- No OOMKill events on api, consumer, or worker.
- ClickHouse query latency stable (no growth in p95 across the run).
- Kafka consumer lag returns to 0 within 5 min of load stop.

## Gate 6 — Disaster recovery

```bash
# In a clean staging cluster:
NS_DR=flexprice-dr
kubectl create namespace "$NS_DR"

# Restore Postgres + ClickHouse snapshots into a fresh managed endpoint.
# Install chart pointing at the restored endpoints:
helm install "$REL" "$CHART" -n "$NS_DR" -f dr-values.yaml --wait
helm test "$REL" -n "$NS_DR"

# Verify customer/subscription/invoice counts match pre-disaster snapshot
kubectl exec -n "$NS_DR" deploy/${REL}-api -- /app/bin/flexctl verify-counts
```

**Pass criteria**
- Restore completes within the documented RTO (target: 4 h).
- Row counts on `customers`, `subscriptions`, `invoices`, `events` match
  pre-disaster snapshot ±0.1%.
- Consumer can replay events from the configured Kafka retention horizon.

---

## Sign-off

Record results in the release PR description:

| Gate | Status | Evidence (log link / screenshot) | Reviewer |
|------|--------|----------------------------------|----------|
| 1. Smoke      | ☐ pass / ☐ fail |  |  |
| 2. Failover   | ☐ pass / ☐ fail |  |  |
| 3. Upgrade    | ☐ pass / ☐ fail |  |  |
| 4. Rollback   | ☐ pass / ☐ fail |  |  |
| 5. Load       | ☐ pass / ☐ fail |  |  |
| 6. DR         | ☐ pass / ☐ fail |  |  |

All six gates must be `pass` before promoting the chart to GA. Failed gates
block the release; file a fix-forward PR rather than carrying known regressions
into the release.
