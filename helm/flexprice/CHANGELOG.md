# Changelog

All notable changes to the FlexPrice Helm chart are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Chart versions are independent of the application (`appVersion`) version —
`Chart.yaml#version` bumps on every chart change, `appVersion` follows the
FlexPrice app release.

## [1.0.0] - 2026-05-11

Initial GA release of the FlexPrice Helm chart. Production-ready packaging
for the FlexPrice billing and pricing platform — API, Kafka consumer,
Temporal worker, schema migrations, and bundled or external stateful
dependencies (PostgreSQL, Kafka, Redis, Temporal, ClickHouse).

### Added

#### Workloads & autoscaling
- Application Deployments for `api`, `consumer`, and `worker`.
- HorizontalPodAutoscaler for api, consumer, and worker.
- PodDisruptionBudget for api, consumer, and worker.
- Per-component RollingUpdate strategy: api/worker default to
  `maxSurge: 1, maxUnavailable: 0`; consumer defaults to
  `maxSurge: 0, maxUnavailable: 1` to minimise Kafka consumer-group
  rebalance churn during rollouts.
- `terminationGracePeriodSeconds` + optional `preStop` sleep on api,
  consumer, and worker for graceful shutdown (Kafka consumer-group
  rebalance, in-flight HTTP drain, Temporal activity completion).
- `topologySpreadConstraints` value (global + per-component override) for
  multi-AZ HA. Empty list by default.
- Opt-in KEDA scaling support for consumer (Kafka lag) and worker
  (Temporal task queue depth).

#### Jobs & lifecycle
- Migration Job as Helm `pre-install`/`pre-upgrade` hook.
- Temporal namespace bootstrap Job as `post-install`/`post-upgrade` hook
  ([templates/jobs/temporal-namespace-bootstrap.yaml](templates/jobs/temporal-namespace-bootstrap.yaml));
  configurable via `temporalConfig.bootstrapNamespaces`.
- `helm test` connectivity probe at
  [templates/tests/test-api-health.yaml](templates/tests/test-api-health.yaml).

#### Stateful dependencies
- Subchart dependencies (bundled tarballs in `charts/`, pinned to
  exact-minor for Renovate visibility):
  - `postgresql` 16.7.27 (bitnami)
  - `kafka` 32.4.3 (bitnami)
  - `redis` 20.13.4 (bitnami)
  - `temporal` 0.74.0 (temporalio)
- ClickHouse modes: `standalone`, `altinity` (CRD-managed), `external`.
- `helm.sh/resource-policy: keep` annotations on PersistentVolumeClaims
  and StatefulSet `volumeClaimTemplates` across all bundled infra
  (postgres, kafka, redis, temporal, clickhouse) — on by default so
  `helm uninstall` cannot destroy customer data.
- `ClickHouse` `max_memory_usage` configurable via
  `clickhouse.maxMemoryUsageBytes` (defaults to 90 GB per query).

#### Networking & ingress
- Ingress (api, frontend, temporal-web) with `nginx` className default.
- API Service named with `-api` suffix; ingress backend updated to match.
- Opt-in ingress-only NetworkPolicy for api (TCP/8080 from a
  configurable ingress controller selector). Egress is intentionally
  unrestricted.

#### Security & identity
- Hardened pod and container `securityContext` defaults that are
  PodSecurityStandard `restricted`-compatible: `runAsNonRoot: true`,
  non-zero `runAsUser/Group/fsGroup`, `readOnlyRootFilesystem: true`,
  `allowPrivilegeEscalation: false`, `privileged: false`, drop `ALL`
  capabilities, and `seccompProfile: RuntimeDefault` on both pod and
  container.
- Externalized credentials via `secrets.existingSecret`.
- Plaintext password defaults removed from `values.yaml`. Installing
  the chart without `-f values-local.yaml` (dev) or
  `-f values-prod.example.yaml` / `secrets.existingSecret` (prod) fails
  fast with a clear `required: ...` error.
- Opt-in per-workload ServiceAccounts via
  `serviceAccount.perComponent=true` (with per-component annotations
  for IRSA / Workload Identity).

#### Images & supply chain
- Default `image.repository` set to `ghcr.io/flexprice/flexprice`;
  default `image.tag` resolves to `.Chart.AppVersion` when empty.
- Multi-arch (`linux/amd64`, `linux/arm64`) app-image publish workflow
  ([.github/workflows/publish-app-image.yml](../../.github/workflows/publish-app-image.yml))
  with SBOM, provenance attestation, and Trivy HIGH/CRITICAL scan.
- Migration container images externalised (separate
  `migration.image.repository` / `tag`).

#### Values & profiles
- `values-prod.example.yaml` template for production overrides
  (external services, `auth.provider: api_key`, ingress, TLS).
- `values-local.yaml` for local Kind cluster bring-up with bundled
  infra enabled and pinned image tags.
- `redisExtended.clusterMode` value (defaults to `true` to preserve
  ElastiCache / Redis Cluster behaviour; flip to `false` for single-node
  Redis — `values-local.yaml` does this since the bundled
  bitnami/redis subchart is single-node).
- One-click local kind provisioning script ([provision.sh](provision.sh))
  and [kind-cluster.yaml](kind-cluster.yaml) for OrbStack + Apple Silicon.

#### CI & tooling
- CI workflow [`.github/workflows/helm-validate.yml`](../../.github/workflows/helm-validate.yml):
  `helm lint` + `helm template | kubeconform` across three profiles +
  `helm install --dry-run` against a kind cluster on every PR touching
  `helm/**`.
- Chart publish workflow triggers on `chart-v*` tags, decoupling chart
  releases from app releases.
- Renovate config ([renovate.json](../../renovate.json)) for chart
  subchart versions, image tags, and GitHub Actions, grouped and
  scheduled weekly.

### Fixed
- Redis client now branches between standalone and cluster topologies
  via `FLEXPRICE_REDIS_CLUSTER_MODE` (Helm: `redisExtended.clusterMode`);
  previously always used `ClusterClient`, which broke against single-node
  ElastiCache.
- Temporal-enable boolean (`FLEXPRICE_TEMPORAL_ENABLED`) no longer
  silently coerced to `true` by Sprig's `default` filter when explicitly
  set to `false`.
- Local Kind provision no longer trips on immutable
  `volumeClaimTemplates` annotation diffs across upgrades.

### Documentation
- Operator runbook for OSS consumers covering EKS, GKE/AKS, and
  bare-metal deployments ([docs/](../docs/)).
- Architecture diagram, troubleshooting runbook, and pre-ship
  validation procedure under [helm/docs/](../docs/).
- [PRODUCTION_READINESS.md](../PRODUCTION_READINESS.md) tracks
  go-live gaps and reconciliation with chart state.
- [docs/MIGRATION-GUIDE.md](../docs/MIGRATION-GUIDE.md) documents
  `helm rollback` schema-change caveats.

### Known caveats
- `helm rollback` does **not** revert applied database schema
  migrations. Schema changes are forward-only; rolling the chart back
  past a migration requires a manual database rollback. See
  [docs/MIGRATION-GUIDE.md](../docs/MIGRATION-GUIDE.md).
- `helm.sh/resource-policy: keep` on PVCs means `helm uninstall` will
  leave PostgreSQL, Kafka, Redis, Temporal, and ClickHouse PVCs
  behind. This is intentional. Delete them manually with `kubectl
  delete pvc -l app.kubernetes.io/instance=<release>` once you have
  confirmed the data is no longer needed.
- Bundled stateful subcharts (`postgresql`, `kafka`, `redis`,
  `temporal`) default to `enabled: false`. Production deployments must
  point at externally managed services or explicitly opt-in.

[1.0.0]: https://github.com/flexprice/flexprice/releases/tag/chart-v1.0.0
