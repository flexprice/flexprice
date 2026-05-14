# FlexPrice Helm Chart `1.0.0` — GA Release

**Release date:** 2026-05-11
**Chart version:** `1.0.0`
**App version:** `1.0.0`
**Install:**

```bash
helm install flexprice oci://ghcr.io/flexprice/charts/flexprice \
  --version 1.0.0 \
  -f values-prod.example.yaml
```

---

## Highlights

This is the first GA release of the FlexPrice Helm chart. It packages the
full FlexPrice billing and pricing platform — HTTP API, Kafka consumer,
Temporal worker, schema migrations, and either bundled or externally
managed stateful dependencies — into a single chart suitable for both
local development and production deployments.

- **Production-ready defaults.** PodSecurityStandard `restricted`
  containers, externalised secrets, fail-fast on missing credentials,
  PDBs and HPAs on every workload, graceful Kafka and Temporal shutdown.
- **Multi-arch images.** `linux/amd64` and `linux/arm64` with SBOM,
  provenance attestation, and Trivy HIGH/CRITICAL scanning gating
  every push.
- **Pluggable stateful tier.** PostgreSQL, Kafka, Redis, and Temporal
  can run bundled (Bitnami / temporalio subcharts) or external; ClickHouse
  supports `standalone`, `altinity` (CRD-managed), and `external` modes.
- **Local development in one command.** `provision.sh` brings up the
  entire stack on Kind for OrbStack / Apple Silicon.
- **Data protection on by default.** All bundled databases get
  `helm.sh/resource-policy: keep` on their PVCs so `helm uninstall`
  cannot accidentally destroy customer data.
- **CI-validated.** Every PR runs `helm lint`, `helm template |
  kubeconform`, and `helm install --dry-run` against a Kind cluster
  across three values profiles.

---

## What's in the box

### Workloads

| Workload | Replicas | HPA | PDB | Rolling strategy |
|----------|----------|-----|-----|------------------|
| `api` | 2+ | yes | yes | `maxSurge: 1, maxUnavailable: 0` |
| `consumer` | 2+ | yes | yes | `maxSurge: 0, maxUnavailable: 1` |
| `worker` | 2+ | yes | yes | `maxSurge: 1, maxUnavailable: 0` |

The consumer strategy is deliberately surge-zero to avoid double-membership
during Kafka consumer-group rebalances on rollout.

### Jobs

- **Migration Job** runs as a `pre-install` / `pre-upgrade` Helm hook.
- **Temporal namespace bootstrap Job** runs as `post-install` /
  `post-upgrade` and creates any namespaces declared in
  `temporalConfig.bootstrapNamespaces`.
- **`helm test`** ships a connectivity probe against the API.

### Stateful dependencies

| Dependency | Bundled subchart (pinned) | External mode |
|------------|---------------------------|---------------|
| PostgreSQL | `bitnami/postgresql` 16.7.27 | yes |
| Kafka      | `bitnami/kafka` 32.4.3       | yes |
| Redis      | `bitnami/redis` 20.13.4      | yes (incl. cluster mode) |
| Temporal   | `temporalio/temporal` 0.74.0 | yes |
| ClickHouse | `standalone` / `altinity` CRD | yes |

All bundled subcharts default to `enabled: false`. Production installs
**must** point at managed services (e.g. RDS, MSK, ElastiCache, Temporal
Cloud) or explicitly opt in.

### Security defaults

Container-level (`securityContext`):

```yaml
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
fsGroup: 65532
readOnlyRootFilesystem: true
allowPrivilegeEscalation: false
privileged: false
capabilities:
  drop: [ALL]
seccompProfile:
  type: RuntimeDefault
```

This is PodSecurityStandard `restricted`-compatible out of the box.

Credentials must be supplied via `secrets.existingSecret` or per-component
values in `values-prod.example.yaml`. Plaintext defaults were removed in
this release — installing without them fails fast at template time with
a clear `required: ...` error.

### Supply chain

- Multi-arch images published to `ghcr.io/flexprice/flexprice`.
- SBOM and SLSA provenance attestation on every push.
- Trivy HIGH/CRITICAL scan gates the publish workflow.
- Chart published via OCI to `ghcr.io/flexprice/charts/flexprice`
  on `chart-v*` tags, decoupled from app releases.

---

## Installation

### Production

1. Provision external Postgres, Kafka, Redis, and Temporal (or Temporal
   Cloud).
2. Create a Kubernetes secret with FlexPrice credentials (DB password,
   Kafka SASL, Redis password, Temporal client cert, API key signing
   secret).
3. Copy `helm/values-prod.example.yaml` and edit it for your
   environment.
4. Install:

```bash
helm install flexprice oci://ghcr.io/flexprice/charts/flexprice \
  --version 1.0.0 \
  --namespace flexprice --create-namespace \
  -f values-prod.example.yaml
```

5. Verify:

```bash
helm test flexprice -n flexprice
```

### Local (Kind)

```bash
cd helm
./provision.sh
```

This brings up a Kind cluster, builds the local FlexPrice image, loads
it into the cluster, installs the chart with `values-local.yaml`, runs
migrations, and bootstraps the Temporal `flexprice` namespace.

---

## Upgrade & rollback

`helm upgrade` is supported for in-place upgrades within `1.x`.

> **⚠️ Schema migrations are forward-only.** `helm rollback` does **not**
> revert applied database migrations. If you roll the chart back past a
> migration boundary you must roll the schema back manually. See
> [docs/MIGRATION-GUIDE.md](MIGRATION-GUIDE.md).

> **⚠️ PVCs survive uninstall.** `helm.sh/resource-policy: keep` is set
> on every bundled-database PVC so `helm uninstall` will leave them
> behind. Delete them explicitly once you've confirmed the data is no
> longer needed:
>
> ```bash
> kubectl delete pvc -l app.kubernetes.io/instance=flexprice -n flexprice
> ```

---

## Known caveats

- Bundled subcharts default to disabled; production installs must
  externalise the stateful tier or opt in.
- `helm rollback` does not revert DB migrations (see above).
- Bundled-database PVCs are retained on uninstall (see above).
- NetworkPolicy ships ingress-only for the api workload; egress is
  intentionally unrestricted in this release.

---

## Compatibility

- **Kubernetes:** `>=1.24` (CI tests against the latest two minor
  releases; floor bumps per the cadence in `PRODUCTION_READINESS.md`).
- **Helm:** `>=3.10`.
- **Architectures:** `linux/amd64`, `linux/arm64`.

---

## Full changelog

See [helm/flexprice/CHANGELOG.md](../flexprice/CHANGELOG.md) for the
complete itemised changelog.

## License

The chart is distributed under the same license as FlexPrice core
(AGPL-3.0-only). Enterprise features (`internal/ee/`) require a
commercial license.
