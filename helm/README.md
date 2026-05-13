# FlexPrice Helm — Repo Workflow

This directory contains the FlexPrice Helm chart **and the dev tooling around it**.

> **Looking for chart usage docs (values, install commands, upgrade paths)?**
> → [`flexprice/README.md`](flexprice/README.md) — that's the README packaged into the published chart.

This file documents the *repo*: scripts, local kind workflow, and how the chart gets published.

---

## Layout

```
helm/
├── README.md                    ← this file (repo / dev workflow)
├── PRODUCTION_READINESS.md      ← pre-ship checklist
├── kind-cluster.yaml            ← kind config: maps host :80/:443 to ingress-nginx
├── provision.sh                 ← one-command deployer (--mode dev|prod)
├── values-local.yaml            ← dev overrides (small resources, plaintext secrets)
├── values-prod.example.yaml     ← production overrides template
└── flexprice/                   ← the chart itself
    ├── README.md                ← published chart docs (values, install, upgrade)
    ├── Chart.yaml
    ├── values.yaml              ← every key documented inline
    ├── charts/                  ← downloaded subchart tarballs
    └── templates/               ← k8s manifests (api, consumer, worker, infra, …)
```

---

## Local development on kind

### Prerequisites

```bash
brew install kind kubectl helm docker
```

### One command — full stack

```bash
cd helm/
./provision.sh
```

That:

1. Creates a kind cluster (`flexprice-local`)
2. Installs ingress-nginx
3. Builds the API image from `Dockerfile.local` and loads it into kind
4. Runs `helm install` in 5 phases (infra → migrations → app → tests → frontend)
5. Bootstraps the Temporal default namespace

### Add to /etc/hosts (one-time)

```bash
echo '127.0.0.1 flexprice.local api.flexprice.local temporal.flexprice.local' | sudo tee -a /etc/hosts
```

### Then visit

| | URL |
|---|---|
| API | http://api.flexprice.local |
| UI | http://flexprice.local |
| Temporal UI | http://temporal.flexprice.local |

### Teardown

```bash
kind delete cluster --name flexprice-local
```

---

## `provision.sh` flags

| Flag | Default | Effect |
|---|---|---|
| `--source local\|ghcr` | `local` | Build images locally vs. pull from `ghcr.io/flexprice/*` |
| `--version <tag>` | `v1.0.0-pre` | Chart version (ghcr mode) + image tag |
| `--image-tag <tag>` | = version | Override image tag separately |
| `--cluster <name>` | `flexprice-local` | kind cluster name |
| `--namespace <ns>` | `flexprice` | k8s namespace |
| `--release <name>` | `flexprice` | helm release name |
| `--skip-cluster` | off | Reuse existing kind cluster |
| `--skip-tests` | off | Skip integration sanity test |
| `--skip-frontend` | off | Skip frontend build/deploy |

### Common variations

```bash
# Test against the published GHCR build (no local docker build needed)
./provision.sh --source ghcr --version v1.0.0-pre

# Faster iteration (no tests, no frontend)
./provision.sh --skip-tests --skip-frontend

# Reuse a running cluster
./provision.sh --skip-cluster
```

---

## Production install — `--mode prod`

Same script, different mode. Assumes external state (RDS, MSK, ElastiCache, ClickHouse Cloud, Temporal Cloud) and a Kubernetes `Secret` built from env vars.

```bash
POSTGRES_PASSWORD=...    \
CLICKHOUSE_PASSWORD=...  \
AUTH_SECRET=...          \
ENCRYPTION_KEY=...       \
INGRESS_HOST=api.example.com \
./provision.sh --mode prod \
  --values flexprice/values.yaml \
  --values-extra values-prod.yaml \
  --skip-infra
```

Prod-only flags: `--skip-infra`, `--skip-db-ping`, `--skip-ext-health`, `--dry-run`.

See [`values-prod.example.yaml`](values-prod.example.yaml) for the prod values template, and [`PRODUCTION_READINESS.md`](PRODUCTION_READINESS.md) for the checklist.

**Data protection for in-cluster databases:** when running stateful workloads
inside the cluster (kicking the tires, single-node prod), the chart renders a
`reclaimPolicy: Retain` StorageClass and adds `helm.sh/resource-policy: keep`
to every stateful resource it owns. `helm uninstall` therefore keeps PVCs/PVs
in place. You **must** set `dataProtection.storageClass.provisioner` for the
StorageClass to render (e.g. `ebs.csi.aws.com` on EKS,
`pd.csi.storage.gke.io` on GKE, `rancher.io/local-path` on kind). See the
[chart README "Data protection" section](flexprice/README.md#data-protection--keep-pvs-across-helm-uninstall)
for the full wiring (including Bitnami subchart pointers) and the cleanup
procedure when decommissioning a release.

---

## How the chart gets published

| Workflow | Trigger | What it does |
|---|---|---|
| `.github/workflows/deploy.yml` | tag `v*` (also branches for ECR) | Builds + pushes the **app image** to GHCR (multi-arch: amd64, arm64, arm/v7) |
| `.github/workflows/publish-helm-chart.yml` | tag `v*` / release | Packages + pushes the **chart** to GHCR OCI |

Published artifacts:

```bash
docker pull ghcr.io/flexprice/flexprice:v1.0.0-pre
helm pull   oci://ghcr.io/flexprice/charts/flexprice --version v1.0.0-pre
```

To test the publish flow under your own GHCR namespace before merging:

```bash
echo $CR_PAT | helm registry login ghcr.io -u <gh-user> --password-stdin
helm package flexprice --version 1.0.0-test
helm push    flexprice-1.0.0-test.tgz oci://ghcr.io/<gh-user>/charts
```

---

## Where to go next

- **Chart usage / values reference** → [`flexprice/README.md`](flexprice/README.md)
- **Pre-ship checklist** → [`PRODUCTION_READINESS.md`](PRODUCTION_READINESS.md)
- **Tuning specific knobs** → inline comments in [`flexprice/values.yaml`](flexprice/values.yaml)
- **Reading the chart source** → start at [`flexprice/templates/api/deployment.yaml`](flexprice/templates/api/deployment.yaml)
