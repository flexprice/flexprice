# Canary preprod + manually-gated prod deploy

**Date:** 2026-06-11
**Status:** Approved (design)
**Author:** ola@flexprice.io (with Claude)

## Goal

Introduce a canary-style preprod stage and a manual approval gate into the
production deploy flow, so that on a `main` merge the pipeline first deploys to
preprod ECS service(s), pauses for human validation, and only then rolls out the
full production deploy.

Today this matters for **one** preprod service — the Mumbai API
(`preprod-flexprice-api-service` in cluster `fp-prod-backend`, region
`ap-south-1`) — but the design is generic across regions/targets.

## Current state (before)

`.github/workflows/deploy.yml`:

- Triggers on push to `develop`, `main`, and `v*` tags; plus `workflow_dispatch`
  (`environment` choice, `dry_run` bool).
- `build` job: builds the Docker image once, pushes to ECR tagged by `${github.sha}`
  (and GHCR best-effort). Skipped on dry-run dispatches.
- `resolve-config` job: selects `PROD_DEPLOY_TARGETS` or `STAGING_DEPLOY_TARGETS`
  (JSON array, one object per region) based on branch/input, emits `targets` + `dry_run`.
- `deploy-api`, `deploy-consumer`, `deploy-worker` jobs: each a matrix over
  `targets`, each calling the reusable composite action
  `./.github/actions/deploy-ecs-service` with the target's cluster/region/service.
  All three run in parallel immediately after build, for **every** target.

`.github/actions/deploy-ecs-service/action.yml` (composite, **unchanged** by this work):
- Describes the ECS service → grabs its current task definition → swaps any
  container whose image starts with the ECR prefix to the new image → registers
  a new task-def revision → `update-service --force-new-deployment`.
- Honors `dry_run` (prints plan only).

## Decisions

- **Platform:** GitHub Actions + a GitHub **Environment** for the manual gate.
  No AWS CodePipeline. Reuses existing OIDC role (`github-cicd`), self-hosted
  runners, ECR flow, and the `deploy-ecs-service` composite action as-is.
- **Single workflow:** modify `deploy.yml`; do **not** add a parallel workflow.
- **Scope of gate:** production (`main`) only. `develop`→staging stays byte-for-byte
  unchanged: no preprod stage, no gate.
- **Post-approval scope:** the full existing prod rollout — `api` + `consumer` +
  `worker` across **all** prod targets (Mumbai + us-west-2).
- **Image:** built once, reused (same SHA) across preprod and prod stages.

## Target flow (after)

```
build image
   │
   ├─► deploy-preprod   (matrix over targets WITH a preprod_service; today: Mumbai preprod-api)
   │        │
   │        ▼
   │   approval-gate    (production only; GitHub Environment `prod-approval`, required reviewers)
   │        │  ⏸ run pauses here until a reviewer approves
   │        ▼
   └─► deploy-api / deploy-consumer / deploy-worker   (all targets, all regions)
```

- **main / production:** preprods deploy → workflow pauses at `approval-gate` →
  on approval, all prod api/consumer/worker across both regions deploy.
- **develop / staging:** `preprod_targets` is empty → `deploy-preprod` skips;
  `approval-gate` skips (not production); prod-deploy jobs run straight through
  exactly as today.

## Changes

### 1. Config: add `preprod_service` to `PROD_DEPLOY_TARGETS`

Optional per-target field. The preprod service is assumed to live in the **same
cluster and region** as its target, so no extra cluster/region fields are needed.

```json
[
  {
    "region": "ap-south-1",
    "cluster": "fp-prod-backend",
    "api_service": "flexprice-api-v4",
    "consumer_service": "flexprice-consumer-v1",
    "worker_service": "temporal-worker",
    "preprod_service": "preprod-flexprice-api-service"
  },
  {
    "region": "us-west-2",
    "cluster": "prod-flexprice-v1",
    "api_service": "prod-flexprice-v1",
    "consumer_service": "flexprice-consumer-v1",
    "worker_service": "flexprice-temporal-worker-v1"
  }
]
```

(GitHub repo-settings change, not a code change.)

### 2. `resolve-config` job — new outputs

In addition to `targets` and `dry_run`, emit:
- `environment` — `production` | `staging` (already computed internally as `ENV`).
- `preprod_targets` — the `targets` array filtered to objects with a non-empty
  `preprod_service`. Produced with a `python3`/`jq` step. Empty array for staging.

### 3. New `deploy-preprod` job

- `needs: [build, resolve-config]`, `if: always() && needs.resolve-config.result == 'success'`.
- `strategy.matrix.target: ${{ fromJson(needs.resolve-config.outputs.preprod_targets) }}`,
  `fail-fast: false`, `max-parallel: 3`. Empty matrix → job is skipped.
- Configures AWS creds (OIDC) for `${{ matrix.target.region }}`.
- Calls `./.github/actions/deploy-ecs-service` with:
  `cluster=${{ matrix.target.cluster }}`, `region=${{ matrix.target.region }}`,
  `service_name=${{ matrix.target.preprod_service }}`, the built image (with the
  same fallback-image expression the existing deploy jobs use), and `dry_run`.

### 4. New `approval-gate` job

- `needs: [resolve-config, deploy-preprod]`.
- `if: needs.resolve-config.outputs.environment == 'production'` — so it only runs
  for production, and (via `needs`, no `always()`) only after `deploy-preprod`
  succeeds. If preprod fails, the gate is skipped and prod never deploys.
- `environment: prod-approval` — the required-reviewers protection rule on this
  GitHub Environment is what pauses the run.
- Body is a single informational `echo` step; the gate is the environment, not the step.

### 5. Gate the existing prod-deploy jobs

For `deploy-api`, `deploy-consumer`, `deploy-worker`:
- Add `approval-gate` to `needs`: `needs: [build, resolve-config, approval-gate]`.
- Change the guard to:
  ```yaml
  if: >-
    always() && needs.resolve-config.result == 'success' &&
    ( needs.resolve-config.outputs.environment != 'production' ||
      needs.approval-gate.result == 'success' )
  ```
  - **staging:** `environment != 'production'` → runs (gate skipped, irrelevant).
  - **production:** requires `approval-gate.result == 'success'` → only runs after
    approval; if preprod failed or approval was rejected, these skip.
- Matrix, composite-action calls, and image expressions are otherwise unchanged.

### 6. One-time GitHub repo setup (documented, not in YAML)

- Create a GitHub **Environment** named `prod-approval`.
- Add a **Required reviewers** protection rule listing the approvers.
- (Optional) restrict the environment to the `main` branch.
- Add `preprod_service` to the Mumbai entry of the `PROD_DEPLOY_TARGETS` variable.

## Non-goals

- No change to the `deploy-ecs-service` composite action.
- No change to staging/`develop` behavior.
- No change to how the preprod task definition is shaped (it currently mirrors
  `web_api`; `otel_service_name` differentiation is deferred — the composite
  action just reuses the preprod service's existing task def and swaps the image).
- No AWS CodePipeline / CodeBuild infrastructure.

## Risks / notes

- The manual gate depends on the `prod-approval` Environment + reviewers being
  configured in repo settings; without it the "pause" won't happen (the job would
  just run through). This is the one out-of-band setup step.
- `develop` path is protected by the `environment != 'production'` branch of the
  guard — verify staging still deploys without interruption after the change.
- If `preprod_service` is added to a target but that ECS service doesn't exist,
  the composite action exits non-zero (service-not-found), failing the preprod
  stage and correctly blocking the gate.
