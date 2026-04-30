# Flexprice Helm Chart — Production Readiness Checklist

Status legend: `[ ]` todo · `[~]` partial · `[x]` done

---

## 1. Chart hygiene

- [ ] `Chart.yaml` `version` follows semver; bump on every template change
- [ ] `Chart.yaml` `appVersion` matches the published image tag (no `latest`)
- [ ] `dependencies:` versions pinned to exact minor (no `x.x.x` floats in prod releases)
- [ ] `helm lint helm/flexprice` clean
- [ ] `helm template ... | kubeconform -strict` passes
- [ ] `values.yaml` documents every key with comments
- [ ] Separate values files: `values.yaml` (prod defaults), `values-local.yaml` (dev), `values-prod.example.yaml` (required overrides)
- [ ] Remove `bitnamilegacy` ARM64 pins from prod values (local-only workaround)
- [ ] `NOTES.txt` shows post-install URLs, secret retrieval commands, next steps

## 2. Images

- [ ] App image published to GHCR with semver tag (`ghcr.io/flexprice/flexprice:vX.Y.Z`)
- [ ] Multi-arch manifest (`linux/amd64`, `linux/arm64`)
- [ ] No `:latest` references in `values.yaml` defaults — pin to `appVersion`
- [ ] `imagePullPolicy: IfNotPresent` (not `Always`) for pinned tags
- [ ] `imagePullSecrets` documented for private GHCR consumers
- [ ] SBOM + provenance attestations enabled in build (`provenance: true`)
- [ ] Image scanning in CI (Trivy / Grype) — fail on HIGH/CRITICAL

## 3. Secrets

- [ ] No plaintext secrets in any values file
- [ ] All sensitive fields support `existingSecret` references
- [ ] Documented secret keys + format in `values.yaml` comments
- [ ] External Secrets Operator / Sealed Secrets integration documented
- [ ] Postgres / ClickHouse / Redis / Kafka / Temporal credentials all externalized
- [ ] Stripe / Chargebee / payment provider keys via `existingSecret` only

## 4. External state (do not run stateful workloads in-chart for prod)

- [ ] Postgres: `postgresql.enabled=false` default in prod values; require external DSN
- [ ] ClickHouse: external cluster (Altinity operator path documented in `Chart.yaml` comment)
- [ ] Kafka: `kafka.enabled=false`; require external brokers (MSK / Confluent / Strimzi)
- [ ] Redis: `redis.enabled=false`; require external endpoint
- [ ] Temporal: `temporal.enabled=false`; require external Temporal (Cloud or self-hosted)
- [ ] Connection strings, TLS, SASL/SCRAM auth all configurable

## 5. Known app blockers (must fix before prod)

- [ ] Redis client cluster-mode mismatch (`internal/redis/client.go` always creates `ClusterClient`) — wire `redisExtended.clusterMode` env or fix client to branch
- [ ] Temporal default-namespace bootstrap moved from `local-up.sh` into a Helm `post-install` Job
- [ ] ClickHouse password wiring verified end-to-end (see prior debug — credentials in env match secret)

## 6. Workload reliability

- [ ] Resource `requests` AND `limits` set on every container (api, consumer, worker)
- [ ] Liveness / readiness / startup probes on all services
- [ ] `PodDisruptionBudget` for api + consumer + worker
- [ ] `HorizontalPodAutoscaler` for api (CPU + custom metric if available)
- [ ] `topologySpreadConstraints` across zones
- [ ] Graceful shutdown: `terminationGracePeriodSeconds` + SIGTERM handling
- [ ] `preStop` hook for consumer to drain in-flight Kafka messages
- [ ] Rolling update strategy with `maxSurge` / `maxUnavailable` tuned

## 7. Security

- [ ] `securityContext`: non-root, `runAsNonRoot: true`, `readOnlyRootFilesystem: true`
- [ ] `allowPrivilegeEscalation: false`, drop `ALL` capabilities
- [ ] `seccompProfile: RuntimeDefault`
- [ ] NetworkPolicies: default-deny + explicit allows for api↔db, consumer↔kafka, etc.
- [ ] ServiceAccount per workload (no shared default SA)
- [ ] PodSecurityStandard: `restricted` namespace label compatible
- [ ] No `hostNetwork` / `hostPath` / `privileged`
- [ ] Image pull from GHCR over HTTPS only

## 8. Networking & ingress

- [ ] Ingress with TLS via cert-manager (Let's Encrypt or internal CA)
- [ ] Real hostnames in prod values (no `.flexprice.local`)
- [ ] Ingress rate limiting + body size limits
- [ ] Drop Temporal Web ingress in prod (use external Temporal UI)
- [ ] Service type `ClusterIP` (no `LoadBalancer` per-service)
- [ ] CORS / allowed origins configurable

## 9. Observability

- [ ] Prometheus `ServiceMonitor` (or annotations) for api, consumer, worker
- [ ] Grafana dashboards committed under `helm/flexprice/dashboards/`
- [ ] Structured JSON logging enabled by default
- [ ] Log shipping documented (Loki / CloudWatch / Datadog)
- [ ] Tracing (OpenTelemetry) endpoint configurable
- [ ] Alert rules: pod restarts, error rate, queue lag, DB pool saturation

## 10. Data & migrations

- [ ] Migration Job runs as Helm `pre-install` + `pre-upgrade` hook
- [ ] Migration Job idempotent + safe to retry
- [ ] Backup/restore procedures documented (out-of-band, not in chart)
- [ ] `helm rollback` behavior documented (especially around schema changes)

## 11. Multi-tenancy & limits

- [ ] `ResourceQuota` + `LimitRange` examples documented
- [ ] ClickHouse 90 GB per-query memory limit surfaced as configurable
- [ ] Connection pool sizes (Postgres, Redis) tunable per-environment

## 12. Release & distribution

- [ ] Chart published to GHCR OCI: `oci://ghcr.io/flexprice/charts/flexprice`
- [ ] GHCR package set to **public** (or pull-secret docs provided)
- [ ] Chart signed with `cosign` (optional)
- [ ] `CHANGELOG.md` updated per release
- [ ] Chart releases tagged independently from app: `chart-vX.Y.Z`
- [ ] Install instructions in `helm/flexprice/README.md`

## 13. CI

- [ ] `helm lint` on every PR touching `helm/**`
- [ ] `helm template` + `kubeconform` on every PR
- [ ] `helm install --dry-run` against a kind cluster on PR
- [ ] Image build pushes to GHCR on tag (multi-arch)
- [ ] Chart publish to GHCR on tag
- [ ] Renovate / Dependabot enabled for chart `dependencies:` and image tags

## 14. Pre-ship validation

- [ ] End-to-end smoke deploy to staging cluster
- [ ] Failover test: kill api pod, kill consumer pod, verify recovery
- [ ] Upgrade test: install vN, upgrade to vN+1, verify no downtime
- [ ] Rollback test: `helm rollback` works cleanly
- [ ] Load test against staging with realistic event volume
- [ ] DR test: restore from backup into fresh cluster

## 15. Documentation

- [ ] `helm/flexprice/README.md`: install, upgrade, uninstall, common values
- [ ] `helm/flexprice/values.yaml`: every key commented
- [ ] Migration guide between chart majors
- [ ] Troubleshooting runbook: common pod failures, log locations
- [ ] Architecture diagram showing in-cluster vs external components

---

## Maintenance cadence

| Task | Frequency |
|---|---|
| Bump chart `dependencies:` minor versions | Monthly (Renovate PR) |
| Bump base image / Go version | Monthly |
| Image vulnerability rescan | Weekly (CI cron) |
| Chart `helm install --dry-run` against latest k8s | Per k8s minor release |
| Review `kubeVersion` floor | Quarterly |
| Backport security fixes to N-1 chart major | As needed |
