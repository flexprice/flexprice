# Changelog

All notable changes to the FlexPrice Helm chart are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Chart versions are independent of the application (`appVersion`) version —
`Chart.yaml#version` bumps on every chart change, `appVersion` follows the
FlexPrice app release.

## [Unreleased]

### Added
- Default `image.repository` set to `ghcr.io/flexprice/flexprice`; default
  `image.tag` resolves to `.Chart.AppVersion` when empty.
- Multi-arch (`linux/amd64`, `linux/arm64`) app-image publish workflow
  (`.github/workflows/publish-app-image.yml`) with SBOM, provenance attestation,
  and Trivy HIGH/CRITICAL scan.
- Temporal namespace bootstrap Helm `post-install`/`post-upgrade` hook Job
  ([templates/jobs/temporal-namespace-bootstrap.yaml](templates/jobs/temporal-namespace-bootstrap.yaml));
  configurable via `temporalConfig.bootstrapNamespaces`.
- `terminationGracePeriodSeconds` + optional `preStop` sleep on api, consumer,
  and worker for graceful shutdown (Kafka consumer-group rebalance, in-flight
  HTTP drain, Temporal activity completion).
- `topologySpreadConstraints` value (global + per-component override) for
  multi-AZ HA. Empty list by default.
- `helm test` connectivity probe at
  [templates/tests/test-api-health.yaml](templates/tests/test-api-health.yaml).
- PodDisruptionBudget for consumer + worker (matches the existing api PDB).
- Per-component RollingUpdate strategy: api/worker default to `maxSurge: 1,
  maxUnavailable: 0`; consumer defaults to `maxSurge: 0, maxUnavailable: 1` to
  minimise Kafka consumer-group rebalance churn during rollouts.
- Opt-in per-workload ServiceAccounts via `serviceAccount.perComponent=true`
  (with per-component annotations for IRSA / Workload Identity).
- Opt-in ingress-only NetworkPolicy for api (TCP/8080 from a configurable
  ingress controller selector). Egress is intentionally unrestricted.
- CI workflow [`.github/workflows/helm-validate.yml`](../../.github/workflows/helm-validate.yml):
  `helm lint` + `helm template | kubeconform` across three profiles +
  `helm install --dry-run` against a kind cluster on every PR touching `helm/**`.
- Renovate config ([`renovate.json`](../../renovate.json)) for chart subchart
  versions, image tags, and GitHub Actions, grouped and scheduled weekly.

### Security
- Hardened pod and container `securityContext` defaults to be
  PodSecurityStandard `restricted`-compatible: `runAsNonRoot: true`,
  `runAsUser/Group/fsGroup` non-zero, `readOnlyRootFilesystem: true`,
  `allowPrivilegeEscalation: false`, `privileged: false`, drop `ALL`
  capabilities, and `seccompProfile: RuntimeDefault` on both pod and container.

### Changed
- All bundled stateful subcharts (`postgresql`, `kafka`, `redis`, `temporal`)
  now default to `enabled: false`. Production deployments must point at
  externally managed services. Local development uses `values-local.yaml`
  which flips them back on with pinned image tags.

### Fixed
- (Tracked in `claude/redis-cluster-mode`) Redis client now branches between
  standalone and cluster topologies via `FLEXPRICE_REDIS_CLUSTER_MODE`
  (Helm: `redisExtended.clusterMode`); previously always used `ClusterClient`
  which broke against single-node ElastiCache. `redisExtended.clusterMode`
  **defaults to `true`** to preserve pre-1.1 behaviour for existing Redis
  Cluster / ElastiCache cluster-mode-enabled installs; flip to `false` for
  single-node Redis. `values-local.yaml` sets it to `false` since the bundled
  bitnami/redis subchart is single-node.

### Documentation
- ClickHouse 90 GB `max_memory_usage` now configurable as
  `clickhouse.maxMemoryUsageBytes` (per `PRODUCTION_READINESS.md#11`).
- Architecture diagram, troubleshooting runbook, and pre-ship validation
  procedure documented under [`helm/docs/`](../docs/).

---

## [1.2.0] - 2026-05-11

### Changed
- **BREAKING:** Plaintext password defaults removed from `values.yaml`.
  Installing the chart without `-f values-local.yaml` (dev) or
  `-f values-prod.example.yaml` / `secrets.existingSecret` (prod)
  will now fail fast with a clear `required: ...` error. This was
  the silent-insecure behavior that PRODUCTION_READINESS.md
  bucket #3 was tracking.
- Dependency versions in `Chart.yaml` are now pinned to exact-minor
  (postgresql 16.7.27, kafka 32.4.3, redis 20.13.4, temporal 0.74.0).
  This makes the declared versions visible to Renovate.

### CI
- Chart publish workflow now triggers on `chart-v*` tags, decoupling
  chart releases from app releases. Branch and app-tag triggers now
  only publish when `Chart.yaml` `version` changed.

### Docs
- `docs/MIGRATION-GUIDE.md` now documents `helm rollback` schema-change
  caveats.
- `PRODUCTION_READINESS.md` reconciled with actual chart state (several
  items were already done but still marked open).

---

## [1.0.0] — 2026-05-04

Initial public release of the FlexPrice Helm chart.

### Added
- Application Deployments: `api`, `consumer`, `worker`.
- HorizontalPodAutoscaler for api, consumer, worker.
- PodDisruptionBudget for api.
- Migration Job as Helm `pre-install`/`pre-upgrade` hook.
- Subchart dependencies (bundled tarballs in `charts/`):
  - `postgresql` 16.7.27 (bitnami)
  - `kafka` 32.4.3 (bitnami)
  - `redis` 20.13.4 (bitnami)
  - `temporal` 0.74.0 (temporalio)
- ClickHouse modes: `standalone`, `altinity` (CRD-managed), `external`.
- Externalized credentials via `secrets.existingSecret`.
- Ingress (api, frontend, temporal-web) with `nginx` className default.
- `values-prod.example.yaml` template for production overrides.
- `values-local.yaml` for local kind cluster bring-up.

[Unreleased]: https://github.com/flexprice/flexprice/compare/chart-v1.2.0...HEAD
[1.2.0]: https://github.com/flexprice/flexprice/compare/chart-v1.0.0...chart-v1.2.0
[1.0.0]: https://github.com/flexprice/flexprice/releases/tag/chart-v1.0.0
