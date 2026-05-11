# Flexprice Helm Chart — Production Readiness Checklist

Status legend: `[ ]` todo · `[~]` partial · `[x]` done

---

## 1. Chart hygiene

- [x] `Chart.yaml` `version` follows semver; bump on every template change *(bumped to 1.1.0)*
- [x] `Chart.yaml` `appVersion` matches the published image tag (no `latest`)
- [ ] `dependencies:` versions pinned to exact minor (no `x.x.x` floats in prod releases)
- [x] `helm lint helm/flexprice` clean
- [ ] `helm template ... | kubeconform -strict` passes *(needs CI step)*
- [x] `values.yaml` documents every key with comments
- [x] Separate values files: `values.yaml` (prod defaults), `values-local.yaml` (dev), `values-prod.example.yaml` (required overrides)
- [x] Remove `bitnamilegacy` ARM64 pins from prod values (local-only workaround)
- [x] `NOTES.txt` shows post-install URLs, secret retrieval commands, next steps

## 2. Images

- [x] App image publish pipeline added: [`.github/workflows/publish-app-image.yml`](../.github/workflows/publish-app-image.yml) — runs on tag push
- [x] Multi-arch manifest (`linux/amd64`, `linux/arm64`)
- [x] No `:latest` references in `values.yaml` defaults — `image.tag` defaults to `.Chart.AppVersion`
- [x] `imagePullPolicy: IfNotPresent` (not `Always`) for pinned tags
- [x] `imagePullSecrets` documented for private GHCR consumers
- [x] SBOM + provenance attestations enabled in build (`provenance: true`)
- [x] Image scanning in CI (Trivy / Grype) — fail on HIGH/CRITICAL

## 3. Secrets

- [ ] No plaintext secrets in any values file
- [ ] All sensitive fields support `existingSecret` references
- [ ] Documented secret keys + format in `values.yaml` comments
- [ ] External Secrets Operator / Sealed Secrets integration documented
- [ ] Postgres / ClickHouse / Redis / Kafka / Temporal credentials all externalized
- [ ] Stripe / Chargebee / payment provider keys via `existingSecret` only

## 4. External state (do not run stateful workloads in-chart for prod)

- [x] Postgres: chart default `postgresql.enabled=false`; require external DSN
- [x] ClickHouse: external cluster (Altinity operator path documented in `Chart.yaml` comment)
- [x] Kafka: chart default `kafka.enabled=false`; require external brokers (MSK / Confluent / Strimzi)
- [x] Redis: chart default `redis.enabled=false`; require external endpoint
- [x] Temporal: chart default `temporal.enabled=false`; require external Temporal (Cloud or self-hosted)
- [x] Connection strings, TLS, SASL/SCRAM auth all configurable

## 5. Known app blockers (must fix before prod)

- [x] Redis client cluster-mode branch on a separate branch `claude/redis-cluster-mode` — to be merged after isolated testing
- [x] Temporal namespace bootstrap implemented as Helm `post-install`/`post-upgrade` hook ([`templates/jobs/temporal-namespace-bootstrap.yaml`](flexprice/templates/jobs/temporal-namespace-bootstrap.yaml))
- [ ] ClickHouse password wiring verified end-to-end (see prior debug — credentials in env match secret)

## 6. Workload reliability

- [x] Resource `requests` AND `limits` set on every container (api, consumer, worker)
- [x] Liveness / readiness / startup probes on all services
- [~] `PodDisruptionBudget` for api only — add consumer + worker (P1 follow-up)
- [x] `HorizontalPodAutoscaler` for api (CPU + custom metric if available)
- [x] `topologySpreadConstraints` across zones *(opt-in via `topologySpreadConstraints` value)*
- [x] Graceful shutdown: `terminationGracePeriodSeconds` + SIGTERM handling
- [x] `preStop` hook for consumer to drain in-flight Kafka messages
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
- [x] ClickHouse per-query memory limit surfaced as configurable (`clickhouse.maxMemoryUsageGB`, default 90)
- [ ] Connection pool sizes (Postgres, Redis) tunable per-environment

## 12. Release & distribution

- [x] Chart published (publish-helm-chart.yml → AWS Public ECR + GHCR on tag)
- [ ] GHCR package set to **public** (or pull-secret docs provided)
- [ ] Chart signed with `cosign` (optional)
- [x] `CHANGELOG.md` created ([`helm/flexprice/CHANGELOG.md`](flexprice/CHANGELOG.md))
- [ ] Chart releases tagged independently from app: `chart-vX.Y.Z`
- [x] Install instructions in `helm/flexprice/README.md`

## 13. CI

- [ ] `helm lint` on every PR touching `helm/**`
- [ ] `helm template` + `kubeconform` on every PR
- [ ] `helm install --dry-run` against a kind cluster on PR
- [ ] Image build pushes to GHCR on tag (multi-arch)
- [ ] Chart publish to GHCR on tag
- [ ] Renovate / Dependabot enabled for chart `dependencies:` and image tags

## 14. Pre-ship validation

Procedure documented in [`docs/PRE-SHIP-VALIDATION.md`](docs/PRE-SHIP-VALIDATION.md). Gates:

- [ ] Gate 1 — End-to-end smoke deploy to staging cluster
- [ ] Gate 2 — Failover test: kill api pod, kill consumer pod, verify recovery
- [ ] Gate 3 — Upgrade test: install vN, upgrade to vN+1, verify no downtime
- [ ] Gate 4 — Rollback test: `helm rollback` works cleanly
- [ ] Gate 5 — Load test against staging with realistic event volume
- [ ] Gate 6 — DR test: restore from backup into fresh cluster

## 15. Documentation

- [x] `helm/flexprice/README.md`: install, upgrade, uninstall, common values
- [x] `helm/flexprice/values.yaml`: every key commented
- [x] Migration guide between chart majors → [`docs/MIGRATION-GUIDE.md`](docs/MIGRATION-GUIDE.md)
- [x] Troubleshooting runbook: common pod failures, log locations → [`docs/TROUBLESHOOTING.md`](docs/TROUBLESHOOTING.md)
- [x] Architecture diagram showing in-cluster vs external components → [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)

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
