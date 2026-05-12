# FlexPrice Helm — Chart Migration Guide

Tracks breaking changes between chart **major** versions. Patch and minor
upgrades are drop-in via `helm upgrade --reuse-values`.

Chart versions follow [SemVer](https://semver.org/). A new major (`X.0.0`)
indicates a value-key rename, default flip, or required infra prerequisite.

---

## Schema changes and rollback

`helm rollback` reverts Kubernetes manifests but does **not** reverse
schema migrations that the upgrade Job applied. If a chart upgrade ran
Ent migrations and ClickHouse migrations that change column types,
drop columns, or otherwise produce data the rollback target can't read,
`helm rollback` alone will leave the cluster in a broken state.

**Before upgrading a production release:**
1. Snapshot Postgres (see `docs/BACKUPS.md`).
2. Snapshot ClickHouse (see `docs/BACKUPS.md`).
3. Run the upgrade.
4. If the new release fails health checks within the first hour and
   you need to roll back:
   - `helm rollback <release> <previous-revision>` reverts manifests.
   - Restore Postgres from the snapshot if the migration was
     destructive. Ent migrations are forward-only and have no
     built-in down step.
   - Restore ClickHouse from the snapshot if the migration changed
     table schemas.

If you are uncertain whether a chart upgrade will run a destructive
migration, run `helm template` against the new chart and inspect
`templates/jobs/migration.yaml` — every `IF NOT EXISTS` / `ON CONFLICT
DO NOTHING` clause indicates an additive step; anything else warrants
a snapshot first.

---

## 1.x → 2.x  *(Unreleased — preview)*

> Apply these changes before `helm upgrade` from any 1.x chart.

### Required infra changes

- **Stateful subcharts default OFF.** `postgresql`, `kafka`, `redis`, and
  `temporal` now default to `enabled: false`. Production deployments must
  point at externally managed services via the `*.external.enabled` blocks.

  Migration path:
  - **You already use external services**: nothing to do. The chart now matches your topology.
  - **You ran with bundled subcharts in production**: stop. Treat 1.x in-cluster stateful workloads as ephemeral. Migrate state to managed services *before* upgrading:
    1. Snapshot Postgres + ClickHouse (out-of-band, not via this chart).
    2. Provision RDS / ClickHouse Cloud / MSK / ElastiCache / Temporal Cloud.
    3. Restore snapshots into managed services.
    4. Set `postgres.external.enabled=true` and friends in your values overrides.
    5. `helm upgrade` — bundled subcharts will be uninstalled (their PVCs remain until you delete them).

  To explicitly keep the bundled subcharts on (NOT recommended for prod):

  ```yaml
  postgresql:
    enabled: true
  kafka:
    enabled: true
  redis:
    enabled: true
  temporal:
    enabled: true
  ```

### Renamed values

| 1.x key                                | 2.x key                              |
|----------------------------------------|--------------------------------------|
| (none — values are additive in 2.x; key renames will be tracked here if introduced) | |

### New value keys

| Key                                       | Default              | Why                                                                 |
|-------------------------------------------|----------------------|---------------------------------------------------------------------|
| `temporalConfig.bootstrapNamespaces`      | `[{name: default, retention: 72h}]` | Replaces the ad-hoc namespace creation from `provision.sh`. Set to `[]` for Temporal Cloud. |
| `temporalConfig.bootstrapImage.*`         | `temporalio/admin-tools:1.24.2-...` | Image used by the bootstrap Helm hook Job.                          |
| `api.terminationGracePeriodSeconds`       | `30`                 | Graceful HTTP drain.                                                |
| `consumer.terminationGracePeriodSeconds`  | `60`                 | Kafka in-flight + rebalance drain.                                  |
| `worker.terminationGracePeriodSeconds`    | `60`                 | Temporal activity completion.                                       |
| `api.preStop` / `consumer.preStop` / `worker.preStop` | `enabled: bool, sleepSeconds: int` | Sleep before SIGTERM so endpoints controller drops the pod first.   |
| `topologySpreadConstraints`               | `[]`                 | Multi-AZ HA; global + per-component override.                       |
| `clickhouse.maxMemoryUsageGB`             | `90`                 | Per-query memory bound, previously hardcoded.                       |

### Image defaults

- `image.repository` now defaults to `ghcr.io/flexprice/flexprice` (was `flexprice-app`).
- `image.tag` defaults to empty → resolves to `.Chart.AppVersion`. If you previously relied on `tag: "local"`, set it explicitly.

### Helm hooks

- A new `post-install`/`post-upgrade` hook Job is added for Temporal namespace
  bootstrap. If your CI applies the chart with `--no-hooks`, also re-run
  namespace registration manually.

### CI/CD

- App image is now published from `.github/workflows/publish-app-image.yml`
  on tag push. Multi-arch (amd64/arm64), SBOM + provenance attached, Trivy
  HIGH/CRITICAL gates the publish.

---

## Pre-flight before any major upgrade

1. **Snapshot state**: Postgres dump + ClickHouse backup, out-of-band.
2. **Pin to a chart version**: `helm pull oci://... --version <X.Y.Z>` so a
   rerun produces the same manifests.
3. **Render and diff**: `helm template ... | tee new.yaml`, compare against
   the previously rendered manifests.
4. **Stage first**: deploy to a non-prod namespace, run `helm test`.
5. **Verify**: `helm test <release>` after upgrade, then sanity-check ingress
   + a synthetic billing event end-to-end.
6. **Rollback ready**: keep `helm history` revisions ≥ 3 and rehearse
   `helm rollback` on staging.

## Compatibility matrix

| Chart    | App (`appVersion`) | Kubernetes        | Notes                                                  |
|----------|--------------------|-------------------|--------------------------------------------------------|
| 1.0.x    | 1.0.x              | 1.24 – 1.30       | Initial release. Bundled subcharts on by default.      |
| 2.0.x (preview) | 1.x         | 1.27 – 1.32       | External services required for prod. Temporal hook.    |

`kubeVersion` in `Chart.yaml` is the enforced floor; upper bound is the
highest minor we test in CI.
