# BYOC: AWS OpenTelemetry Collector

Route FlexPrice application telemetry (traces, metrics, logs) to AWS native
observability services — X-Ray, CloudWatch Logs, and CloudWatch Metrics (EMF) —
via an in-cluster ADOT (AWS Distro for OpenTelemetry) collector.

## What it does

The chart includes an optional ADOT collector sidecar and OTel instrumentation
in the FlexPrice app. When enabled:

1. **App → OTLP**: FlexPrice emits OpenTelemetry Protocol (OTLP) signals over
   gRPC to `<release>-flexprice-adot-collector.<ns>.svc:4317` (in-cluster endpoint).
2. **Collector → X-Ray**: Traces are forwarded to AWS X-Ray for distributed tracing.
3. **Collector → CloudWatch Logs**: Metrics (EMF format) and application logs
   are sent to CloudWatch Logs under `/flexprice/otel/metrics` and
   `/flexprice/otel/logs`.

This setup is **single-destination** — turn off SigNoz if you were using it.

## Prerequisites

1. **Cluster**: EKS with OIDC provider and working IAM IRSA.
2. **IAM role**: Create `flexprice-adot-collector` role with permissions for
   X-Ray and CloudWatch. See [AWS-IAM.md](AWS-IAM.md) (section
   `flexprice-adot-collector`).
3. **Role ARN**: Note the ARN for the role, e.g.:
   ```
   arn:aws:iam::123456789012:role/flexprice-adot-collector
   ```

## Enable in the chart

Add the following to your Helm values:

```yaml
adot:
  enabled: true
  region: us-east-1                      # AWS region where X-Ray + CloudWatch are
  image:
    digest: sha256:abc123...             # ADOT collector image digest
  serviceAccount:
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flexprice-adot-collector

otel:
  enabled: true
  protocol: grpc
  insecure: true                         # Plaintext OTLP within the cluster; TLS not needed
  traces:
    enabled: true
    endpoint: myrelease-flexprice-adot-collector.flexprice.svc:4317
  metrics:
    enabled: true
    endpoint: myrelease-flexprice-adot-collector.flexprice.svc:4317
  logs:
    enabled: true
    endpoint: myrelease-flexprice-adot-collector.flexprice.svc:4317
```

Replace `<release>`, `<ns>` with your actual Helm release name and namespace.
In the example above, the release name is `myrelease` and the namespace is
`flexprice`. The endpoint format is:
```
<release>-flexprice-adot-collector.<namespace>.svc:4317
```

> **Note**: the Service name follows `<fullname>-adot-collector`, where
> `fullname` is `<release>-flexprice` (the chart name is appended to the
> release name), UNLESS the release name already contains "flexprice" (e.g.
> a release named `flexprice` or `flexprice-prod`), in which case `fullname`
> is just `<release>` and the Service is `<release>-adot-collector`.

> **Note**: `otel.insecure: true` applies to all signals (traces, metrics,
> logs). This is safe and expected — all traffic stays within the cluster.

## Thin metrics caveat

By default, only database and cache instruments export metrics:

- `postgres.*` (connection pools, query timing)
- `redis.*` (connection pools, operation timing)

These appear in CloudWatch under `/flexprice/otel/metrics`.

**To enable request/RED metrics** (HTTP server latency, error rate, throughput):

```yaml
otel:
  metrics:
    enabled: true
    endpoint: myrelease-flexprice-adot-collector.flexprice.svc:4317
    httpServerEnabled: true              # Enable HTTP server instrumentation
```

Without this, CloudWatch will show only database and cache metrics, missing
visibility into API response times and error rates. Add it if the API is
exposed to internal or external traffic.

## Verifying it's working

### 1. Check the ADOT collector pod

```bash
kubectl get pod -n flexprice -l app.kubernetes.io/component=adot-collector
kubectl logs -n flexprice <pod-name> | head -20
```

The logs should show the collector listening on port 4317.

### 2. Check X-Ray console

```
AWS Console → X-Ray → Traces
```

You should see traces appearing within 30 seconds of the app processing requests.

### 3. Check CloudWatch Logs

```
AWS Console → CloudWatch → Log Groups
```

Look for:
- `/flexprice/otel/metrics` — EMF-formatted metrics (database, cache, optionally HTTP)
- `/flexprice/otel/logs` — application logs from the OTel collector

Log streams are auto-created per pod.

## Disabling

To turn off ADOT and OTel:

```yaml
adot:
  enabled: false
otel:
  enabled: false
```

If you were using SigNoz, also disable it:

```yaml
signoz:
  enabled: false
```

## Troubleshooting

### Collector pod crashes

Check the logs:

```bash
kubectl logs -n flexprice -l app.kubernetes.io/component=adot-collector
```

Common issues:
- **Authentication**: The IRSA role ARN annotation is missing or incorrect on the
  collector's ServiceAccount. Verify:
  ```bash
  kubectl get sa -n flexprice flexprice-adot-collector -o yaml | grep role-arn
  ```
- **Image digest**: The ADOT image digest is invalid. Verify it exists in
  `public.ecr.aws/aws-observability/aws-for-fluent-bit:latest`.

### No traces in X-Ray

1. Verify the app is running and generating traffic.
2. Check the ADOT collector logs for export errors (auth, network).
3. Verify the X-Ray role has `xray:PutTraceSegments` + `xray:PutTelemetryRecords` (see [AWS-IAM.md](AWS-IAM.md)).

### No metrics in CloudWatch

1. Check `/flexprice/otel/metrics` log group exists.
2. Verify `otel.metrics.enabled: true`.
3. If only cache/DB metrics appear and you expect HTTP metrics, set `otel.metrics.httpServerEnabled: true`.
4. Check the ADOT collector logs for metric export errors.

### Endpoint not reachable

If the app logs show connection errors to `<endpoint>:4317`:

```bash
# From a FlexPrice pod, verify DNS resolves:
kubectl exec -n flexprice <app-pod> -- nslookup myrelease-flexprice-adot-collector.flexprice.svc
kubectl exec -n flexprice <app-pod> -- nc -zv myrelease-flexprice-adot-collector.flexprice.svc 4317
```

The collector pod must be running and healthy.

## See also

- [AWS-IAM.md](AWS-IAM.md) — IRSA roles for the ADOT collector
- [EKS-QUICKSTART.md](EKS-QUICKSTART.md) — full EKS setup
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md) — general Kubernetes troubleshooting
