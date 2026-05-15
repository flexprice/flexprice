# FlexPrice Helm — Documentation

Operator-facing documentation for the FlexPrice Helm chart. Use this as
your entry point.

## I want to install FlexPrice on…

| Where           | Start here                                                                  |
|-----------------|-----------------------------------------------------------------------------|
| **AWS EKS**     | [EKS-QUICKSTART.md](EKS-QUICKSTART.md) — full end-to-end RDS+MSK+ElastiCache+ClickHouse Cloud + Temporal Cloud |
| **GKE / AKS / bare metal** | [PLATFORMS.md](PLATFORMS.md) — service mapping + per-platform deltas |
| **Local dev (kind / OrbStack)** | [PLATFORMS.md#local-development](PLATFORMS.md#local-development-kind--orbstack--docker-desktop) — or run `helm/provision.sh` |

## I need to understand…

| Topic                                     | Doc                                                  |
|-------------------------------------------|------------------------------------------------------|
| What needs to exist in my cluster first   | [PREREQUISITES.md](PREREQUISITES.md)                 |
| What goes in the Kubernetes Secret        | [SECRETS.md](SECRETS.md) — minimum-viable + ESO patterns |
| What every values knob does               | [CONFIGURATION-REFERENCE.md](CONFIGURATION-REFERENCE.md) — and [`values.yaml`](../flexprice/values.yaml) |
| How the chart is laid out                 | [ARCHITECTURE.md](ARCHITECTURE.md)                   |
| Temporal Cloud vs self-hosted             | [TEMPORAL-GUIDE.md](TEMPORAL-GUIDE.md)               |
| IAM permissions per workload (EKS)        | [AWS-IAM.md](AWS-IAM.md)                             |
| Backup + restore strategies               | [BACKUPS.md](BACKUPS.md)                             |

## I'm operating it…

| Task                                      | Doc                                                  |
|-------------------------------------------|------------------------------------------------------|
| Pre-release validation gates              | [PRE-SHIP-VALIDATION.md](PRE-SHIP-VALIDATION.md)     |
| Upgrading the chart across majors         | [MIGRATION-GUIDE.md](MIGRATION-GUIDE.md)             |
| Diagnosing pod failures                   | [TROUBLESHOOTING.md](TROUBLESHOOTING.md)             |
| Local-only setup quirks                   | [local-setup-troubleshooting.md](local-setup-troubleshooting.md) |

## I want to know what's still pending

[`PRODUCTION_READINESS.md`](../PRODUCTION_READINESS.md) — the per-section
checklist (chart hygiene, security, observability, CI, ...) with `[x]`
ticks for what's done.

## I want to release a new chart version

`helm/flexprice/Chart.yaml` bumps `version` (chart) and `appVersion`
(app). [`CHANGELOG.md`](../flexprice/CHANGELOG.md) documents what
changed. The publish pipelines (`.github/workflows/publish-app-image.yml`
+ `publish-helm-chart.yml`) fire on `v*` tags.

## Where the chart lives

- **Chart**: `oci://ghcr.io/flexprice/charts/flexprice`
- **App image**: `ghcr.io/flexprice/flexprice`

Pull instructions:

```bash
# Latest stable
helm pull oci://ghcr.io/flexprice/charts/flexprice
docker pull ghcr.io/flexprice/flexprice:latest

# A specific version
helm pull oci://ghcr.io/flexprice/charts/flexprice --version 1.1.0
docker pull ghcr.io/flexprice/flexprice:v1.0.0
```

## When to file an issue

- Chart bug (renders invalid YAML, missing knob, doc inaccurate):
  https://github.com/flexprice/flexprice/issues
- Operational question (your specific platform / regulatory setup):
  start a Discussion on the same repo.
- Security: see the project's `SECURITY.md` for the responsible-disclosure
  process; do not file public issues for security bugs.
