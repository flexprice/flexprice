# Deploy workflow – GitHub Actions inputs

Configure these in the repo: **Settings → Secrets and variables → Actions**.

---

## Secrets (sensitive – use **Secrets**, not Variables)

| Name | Example value | Description |
|------|----------------|-------------|
| `AWS_ACCOUNT_ID` | `123456789012` | Your AWS account ID (12 digits). Used in OIDC role ARN. |
| `AWS_REGION` | `ap-south-1` | Default AWS region for build (ECR login). Deploy uses region from targets. |

---

## Variables (non-sensitive – use **Variables**)

| Name | Example value | Description |
|------|----------------|-------------|
| `ECR_REGISTRY` | `123456789012.dkr.ecr.ap-south-1.amazonaws.com` | ECR registry host (no `https://`, no trailing slash). |
| `ECR_REPOSITORY` | `my-app` | ECR repository name. Image tag is `${{ github.sha }}`. |
| `STAGING_DEPLOY_TARGETS` | `[]` | **Retired.** There is no AWS staging — the `fp-staging-backend` cluster is gone and staging runs on GCP via ArgoCD. Keep this `[]`; see below. |
| `PROD_DEPLOY_TARGETS` | See JSON below | JSON array of ECS targets for **production** (used on `main` and when manual run chooses production). |
| `DB_MIGRATIONS_ENABLED` | `true` / unset | Whether the `migrate` job runs database migrations before a rollout. **Off unless set to exactly `true`.** Turn it on for an environment only after that database has been adopted (`make migrate-adopt`) — an unadopted database fails the job on purpose and blocks the deploy. A single run can also be exempted with the `skip_migrations` input on a manual dispatch. When migrations do not run, the workflow summary says so. |

---

## Deploy targets JSON format

One object per region. Each object must have: `region`, `cluster`, `api_service`, `consumer_service`, `worker_service`.

**Single region (staging):**

```json
[
  {
    "region": "ap-south-1",
    "cluster": "my-staging-cluster",
    "api_service": "my-api",
    "consumer_service": "my-consumer",
    "worker_service": "my-worker"
  }
]
```

**Single region (production):**

```json
[
  {
    "region": "ap-south-1",
    "cluster": "my-prod-cluster",
    "api_service": "my-api",
    "consumer_service": "my-consumer",
    "worker_service": "my-worker"
  }
]
```

**Multiple regions:** add more objects to the array:

```json
[
  {
    "region": "ap-south-1",
    "cluster": "prod-cluster-1",
    "api_service": "api",
    "consumer_service": "consumer",
    "worker_service": "worker"
  },
  {
    "region": "us-west-2",
    "cluster": "prod-cluster-2",
    "api_service": "api",
    "consumer_service": "consumer",
    "worker_service": "worker"
  }
]
```

Paste the full JSON (no comments, valid JSON) into the Variable value for `PROD_DEPLOY_TARGETS`.

### Staging is not deployed by this workflow

`STAGING_DEPLOY_TARGETS` is `[]` and should stay that way. The AWS staging
cluster (`fp-staging-backend`) was decommissioned; staging now runs on GCP and is
deployed by ArgoCD from the Helm chart in
`internal/ee/infrastructure/helm/flexprice`.

A push to `develop` still runs this workflow, and that is intentional — the
`build` job publishes `ghcr.io/flexprice/flexprice:develop`, which GCP staging's
`argocd-image-updater` watches for new digests. Everything after `build` skips,
because the deploy and migration matrices expand to zero entries.

Use `[]` rather than clearing the variable: `resolve-config` leaves `targets` as
an empty **string** when the variable is unset, and `fromJson('')` fails the run.

---

## Checklist

- [ ] **Secrets:** `AWS_ACCOUNT_ID`, `AWS_REGION`
- [ ] **Variables:** `ECR_REGISTRY`, `ECR_REPOSITORY`
- [ ] **Variables:** `STAGING_DEPLOY_TARGETS`, `PROD_DEPLOY_TARGETS` (valid JSON)
- [ ] **AWS:** IAM role `github-cicd` with OIDC trust for your repo; permissions for ECR push and ECS `DescribeServices`, `DescribeTaskDefinition`, `RegisterTaskDefinition`, `UpdateService`
- [ ] **AWS:** the same role also needs `ecs:RunTask` and `ecs:DescribeTasks` for the database-migration task, plus `iam:PassRole` for the task and execution roles it inherits. `RunTask` is scoped to task definitions matching `*-migrate:*` in the backend cluster — see the `ECSRunMigrationTask` / `ECSPollMigrationTask` statements on `GitHubActionsECRDeployPolicy`
- [ ] **Runners:** Workflow uses `runs-on: self-hosted`; ensure a self-hosted runner is registered
