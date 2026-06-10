# Preprod canary + production approval gate — setup & operations

The production deploy flow (`.github/workflows/deploy.yml`) is:

    build image → deploy preprod service(s) → ⏸ manual approval → full prod rollout
                                              (GitHub Environment: prod-approval)

Staging (`develop`) is unaffected: no preprod stage, no gate.

## One-time setup (repo admin)

### 1. Create the `prod-approval` environment

1. GitHub → repo → **Settings → Environments → New environment**.
2. Name it exactly `prod-approval` (must match `environment:` in deploy.yml).
3. Enable **Required reviewers** and add the people/teams allowed to approve
   production rollouts (max 6 entries; only ONE of them needs to approve).
4. Recommended: enable **Prevent self-review** so the merger can't approve
   their own rollout. Optional, decide per team policy.
5. Under **Deployment branches and tags**, choose **Selected branches and tags**
   and add `main` — prevents the gate (and thus prod deploys) from being
   reachable from other refs.

### 2. Add `preprod_service` to the deploy targets variable

GitHub → repo → **Settings → Secrets and variables → Actions → Variables** →
edit `PROD_DEPLOY_TARGETS`:

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

`preprod_service` is optional per target and names an ECS service in the SAME
cluster/region. Targets without it (us-west-2 today) have no preprod stage but
still wait behind the same approval gate.

The preprod ECS service must already exist (today:
`preprod-flexprice-api-service` in `fp-prod-backend`, ap-south-1). The workflow
reuses that service's current task definition and only swaps the image, so any
preprod-specific config (e.g. a future distinct `OTEL_SERVICE_NAME`) lives in
the service's task definition, not in CI.

### 3. Verify with a dry run

Actions → "Build, Push & Deploy to AWS ECS" → **Run workflow** →
environment `production`, **dry_run = true**. Expected: preprod + prod jobs all
print `[DRY RUN]` plans, and the approval gate is bypassed (dry runs don't pause).

Then merge a trivial change to `main` and confirm the run pauses at
**Approve production rollout** after the preprod deploy succeeds.

## Day-to-day: approving a rollout

1. Merge to `main`. The workflow builds, deploys preprod
   (`preprod-flexprice-api-service`, Mumbai), then pauses.
2. Validate the change on the preprod API service.
3. Open the workflow run (Actions tab; reviewers also get a notification) →
   **Review deployments** → check `prod-approval` → **Approve and deploy**.
4. The api/consumer/worker services deploy to ALL production targets
   (ap-south-1 + us-west-2).

To abort: **Reject** the review. Prod jobs are skipped; preprod keeps the new
image until the next deploy (push another commit/revert to roll preprod back).

Unactioned gates auto-expire after 30 days (run fails harmlessly; nothing
deploys).

## Failure modes

| Situation | Result |
|---|---|
| Preprod deploy fails | Gate never offered; prod jobs skipped |
| Reviewer rejects | Prod jobs skipped |
| Gate ignored 30 days | Run times out; prod jobs skipped |
| `prod-approval` env missing/unprotected | Gate job runs WITHOUT pausing — prod deploys immediately (the pause lives in the environment protection rule, not the YAML). Keep the environment configured. |
