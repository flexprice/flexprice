# Canary Preprod + Gated Prod Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On `main` merges, deploy preprod ECS service(s) first, pause for manual approval (GitHub Environment), then roll out the full production deploy — per the approved spec at `docs/superpowers/specs/2026-06-11-canary-preprod-gated-deploy-design.md`.

**Architecture:** Single-workflow change to `.github/workflows/deploy.yml`: `resolve-config` gains `environment` + `preprod_targets` outputs; a new `deploy-preprod` job (matrix over preprod-enabled targets) reuses the existing `deploy-ecs-service` composite action; a new `approval-gate` job bound to GitHub Environment `prod-approval` pauses the run; the three existing prod deploy jobs are gated on it for production only. Staging (`develop`) behavior is unchanged.

**Tech Stack:** GitHub Actions (self-hosted runners), AWS ECS via existing composite action, python3 for JSON filtering (already used by the composite action).

**Two deliberate refinements beyond the spec text** (both preserve spec intent, flag to user at review):
1. `approval-gate` also treats `deploy-preprod.result == 'skipped'` as passable, so a production config with *zero* preprod services still gets a gate then deploys (instead of silently never deploying prod).
2. Dry-run dispatches **bypass** the gate (`dry_run == 'true'` short-circuits both the gate job and the prod-job guards), preserving today's "validate config & print plan" UX without a human pause. Nothing is registered/deployed on dry runs anyway.

---

## File Structure

- **Modify:** `.github/workflows/deploy.yml`
  - `resolve-config` job: restructured config step + 2 new outputs
  - New job `deploy-preprod` (after `resolve-config`, before the deploy jobs)
  - New job `approval-gate`
  - `deploy-api` / `deploy-consumer` / `deploy-worker`: `needs` + `if` changes only
  - Comment block updates documenting `preprod_service` and the flow
- **Create:** `docs/deployment/preprod-approval-gate.md` — one-time GitHub setup runbook (environment, reviewers, variable change, approval how-to)
- **No change:** `.github/actions/deploy-ecs-service/action.yml`

Note: `docs/superpowers/*` is gitignored — commit plan/spec files with `git add -f`. The runbook lives in `docs/deployment/` which is tracked normally.

---

### Task 1: `resolve-config` — emit `environment` and `preprod_targets`

**Files:**
- Modify: `.github/workflows/deploy.yml:164-205` (the `resolve-config` job)

- [ ] **Step 1: Test the preprod filter snippet locally (the "failing test" for config parsing)**

Run:
```bash
echo '[{"region":"ap-south-1","cluster":"fp-prod-backend","api_service":"flexprice-api-v4","consumer_service":"flexprice-consumer-v1","worker_service":"temporal-worker","preprod_service":"preprod-flexprice-api-service"},{"region":"us-west-2","cluster":"prod-flexprice-v1","api_service":"prod-flexprice-v1","consumer_service":"flexprice-consumer-v1","worker_service":"flexprice-temporal-worker-v1"}]' | python3 -c "
import json, sys
targets = json.load(sys.stdin)
print(json.dumps([t for t in targets if t.get('preprod_service')]))
"
```
Expected: a one-element JSON array containing only the `ap-south-1` object (the one with `preprod_service`).

Also verify the empty case:
```bash
echo '[{"region":"us-west-2","cluster":"c","api_service":"a","consumer_service":"b","worker_service":"w"}]' | python3 -c "
import json, sys
targets = json.load(sys.stdin)
print(json.dumps([t for t in targets if t.get('preprod_service')]))
"
```
Expected: `[]`

- [ ] **Step 2: Update the `resolve-config` outputs block**

Replace (current lines 167-169):
```yaml
    outputs:
      targets: ${{ steps.config.outputs.targets }}
      dry_run: ${{ steps.config.outputs.dry_run }}
```
With:
```yaml
    outputs:
      targets: ${{ steps.config.outputs.targets }}
      preprod_targets: ${{ steps.config.outputs.preprod_targets }}
      environment: ${{ steps.config.outputs.environment }}
      dry_run: ${{ steps.config.outputs.dry_run }}
```

- [ ] **Step 3: Replace the `run:` block of the "Select targets based on branch / input" step**

Replace the entire `run: |` script (current lines 177-205) with:
```bash
          # Determine environment
          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            ENV="${{ github.event.inputs.environment }}"
            # Default dry_run to false when not explicitly set
            DRY_RUN="${{ github.event.inputs.dry_run }}"
            if [ -z "$DRY_RUN" ]; then DRY_RUN="false"; fi
          elif [ "${{ github.ref_name }}" = "main" ]; then
            ENV="production"
            DRY_RUN="false"
          else
            ENV="staging"
            DRY_RUN="false"
          fi

          echo "Environment and dry_run configured (values redacted for public logs)"

          if [ "$ENV" = "production" ]; then
            TARGETS_JSON="$PROD_TARGETS"
          else
            TARGETS_JSON="$STAGING_TARGETS"
          fi

          # Targets that also define a preprod_service get a canary deploy +
          # manual approval gate before the full production rollout.
          if [ -z "$TARGETS_JSON" ]; then
            PREPROD_TARGETS_JSON="[]"
          else
            PREPROD_TARGETS_JSON=$(echo "$TARGETS_JSON" | python3 -c "
          import json, sys
          targets = json.load(sys.stdin)
          print(json.dumps([t for t in targets if t.get('preprod_service')]))
          ")
          fi

          # Use delimiter format so JSON (newlines, spaces) is written correctly to GITHUB_OUTPUT
          echo "targets<<TARGETS_EOF" >> $GITHUB_OUTPUT
          echo "$TARGETS_JSON" >> $GITHUB_OUTPUT
          echo "TARGETS_EOF" >> $GITHUB_OUTPUT

          echo "preprod_targets<<PREPROD_EOF" >> $GITHUB_OUTPUT
          echo "$PREPROD_TARGETS_JSON" >> $GITHUB_OUTPUT
          echo "PREPROD_EOF" >> $GITHUB_OUTPUT

          echo "environment=$ENV" >> $GITHUB_OUTPUT
          echo "dry_run=$DRY_RUN" >> $GITHUB_OUTPUT
```

(Behavior-preserving restructure: the production/staging if/else now selects `TARGETS_JSON` once instead of duplicating the heredoc write. Indentation of the embedded python: GitHub Actions strips nothing — the python here-string lines must stay flush with the surrounding script indentation; python tolerates the leading whitespace because the `-c` string dedents consistently. Match the composite action's existing style at `.github/actions/deploy-ecs-service/action.yml:70-85`, which embeds python the same way.)

- [ ] **Step 4: Update the comment block above `resolve-config` (lines ~132-163) to document `preprod_service`**

Inside the example JSON in the comment, change the first (ap-south-1) object to include the optional field, and add one explanatory line. The comment's example becomes:
```yaml
  #   Format (one object per region):
  #   [
  #     {
  #       "region":           "ap-south-1",
  #       "cluster":          "fp-prod-backend",
  #       "api_service":      "flexprice-api-v4",
  #       "consumer_service": "flexprice-consumer-v1",
  #       "worker_service":   "temporal-worker",
  #       "preprod_service":  "preprod-flexprice-api-service"
  #     },
  #     {
  #       "region":           "us-west-2",
  #       "cluster":          "prod-flexprice-v1",
  #       "api_service":      "prod-flexprice-v1",
  #       "consumer_service": "flexprice-consumer-v1",
  #       "worker_service":   "flexprice-temporal-worker-v1"
  #     }
  #   ]
  #
  #   preprod_service (optional): canary ECS service in the SAME cluster/region.
  #   Targets that define it are deployed to preprod first; production rollout
  #   then waits for manual approval (GitHub Environment: prod-approval).
```

- [ ] **Step 5: Validate YAML parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy.yml')); print('OK')"`
Expected: `OK`

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/deploy.yml
git commit -m "ci(deploy): resolve-config emits environment + preprod_targets"
```

---

### Task 2: Add the `deploy-preprod` job

**Files:**
- Modify: `.github/workflows/deploy.yml` — insert new job between `resolve-config` and `deploy-api`

- [ ] **Step 1: Insert the job**

Add directly after the `resolve-config` job, before the `deploy-api` comment banner:
```yaml
  # ──────────────────────────────────────────────────────────────────────────
  # Job 3: Canary — deploy preprod service(s) for targets that define one.
  # Runs BEFORE the approval gate; production rollout only proceeds after a
  # human validates these preprod deployments and approves the gate.
  # Skipped entirely when no target defines preprod_service (e.g. staging).
  # ──────────────────────────────────────────────────────────────────────────
  deploy-preprod:
    name: Deploy Preprod (${{ strategy.job-index }})
    needs: [build, resolve-config]
    if: ${{ always() && needs.resolve-config.result == 'success' && needs.resolve-config.outputs.preprod_targets != '[]' }}
    runs-on: self-hosted
    strategy:
      matrix:
        target: ${{ fromJson(needs.resolve-config.outputs.preprod_targets) }}
      fail-fast: false
      max-parallel: 3

    steps:
      - name: Configure AWS credentials (OIDC)
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/github-cicd
          aws-region: ${{ matrix.target.region }}

      - name: Deploy ECS Preprod service
        uses: ./.github/actions/deploy-ecs-service
        with:
          cluster: ${{ matrix.target.cluster }}
          region: ${{ matrix.target.region }}
          service_name: ${{ matrix.target.preprod_service }}
          image: ${{ needs.build.outputs.image || format('{0}/{1}:{2}', env.ECR_REGISTRY, env.ECR_REPOSITORY, github.sha) }}
          ecr_repo_prefix: ${{ env.ECR_REGISTRY }}/${{ env.ECR_REPOSITORY }}
          dry_run: ${{ needs.resolve-config.outputs.dry_run }}
```

Notes for the implementer:
- The `preprod_targets != '[]'` guard is REQUIRED: a matrix built from an empty array fails the job ("Matrix vector 'target' does not contain any values"), so we must skip the job instead.
- No `actions/checkout` step — this matches the existing `deploy-api`/`deploy-consumer`/`deploy-worker` jobs, which rely on the self-hosted runner's persistent workspace for the local composite action. Do not "fix" this here; consistency with the working pattern matters more.
- The `image:` fallback expression is copied verbatim from `deploy-api` (line 240) so dry-run dispatches (build skipped) still resolve an image string.

- [ ] **Step 2: Validate YAML parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy.yml')); print('OK')"`
Expected: `OK`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/deploy.yml
git commit -m "ci(deploy): add preprod canary deploy job"
```

---

### Task 3: Add the `approval-gate` job

**Files:**
- Modify: `.github/workflows/deploy.yml` — insert new job after `deploy-preprod`

- [ ] **Step 1: Insert the job**

Add directly after `deploy-preprod`:
```yaml
  # ──────────────────────────────────────────────────────────────────────────
  # Job 4: Manual approval gate (production only).
  # Bound to the GitHub Environment `prod-approval` whose required-reviewers
  # protection rule pauses the run here until a reviewer approves.
  # - Runs only for production, after preprod deploys succeed.
  # - 'skipped' preprod is also accepted so a prod config with no preprod
  #   services still gates + deploys instead of silently doing nothing.
  # - Dry runs bypass the gate (nothing is registered/deployed anyway).
  # Setup runbook: docs/deployment/preprod-approval-gate.md
  # ──────────────────────────────────────────────────────────────────────────
  approval-gate:
    name: Approve production rollout
    needs: [resolve-config, deploy-preprod]
    if: >-
      ${{ always() &&
          needs.resolve-config.result == 'success' &&
          needs.resolve-config.outputs.environment == 'production' &&
          needs.resolve-config.outputs.dry_run != 'true' &&
          (needs.deploy-preprod.result == 'success' || needs.deploy-preprod.result == 'skipped') }}
    runs-on: self-hosted
    environment: prod-approval

    steps:
      - name: Production rollout approved
        run: |
          echo "Preprod canary validated and production rollout approved."
          echo "Proceeding to deploy api, consumer and worker services to all production targets."
```

- [ ] **Step 2: Validate YAML parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy.yml')); print('OK')"`
Expected: `OK`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/deploy.yml
git commit -m "ci(deploy): add manual approval gate before production rollout"
```

---

### Task 4: Gate `deploy-api`, `deploy-consumer`, `deploy-worker` on the approval

**Files:**
- Modify: `.github/workflows/deploy.yml` — `needs:`/`if:` lines of the three deploy jobs (currently lines 218-219, 246-247, 274-275; shifted down by the two new jobs)

- [ ] **Step 1: Update all three jobs' `needs` and `if`**

In each of `deploy-api`, `deploy-consumer`, `deploy-worker`, replace:
```yaml
    needs: [build, resolve-config]
    if: ${{ always() && needs.resolve-config.result == 'success' }}
```
With:
```yaml
    needs: [build, resolve-config, approval-gate]
    # staging: deploys immediately (gate is skipped / irrelevant)
    # production: requires the approval-gate to have been approved
    # dry-run: bypasses the gate; composite action prints plan only
    if: >-
      ${{ always() &&
          needs.resolve-config.result == 'success' &&
          (needs.resolve-config.outputs.environment != 'production' ||
           needs.resolve-config.outputs.dry_run == 'true' ||
           needs.approval-gate.result == 'success') }}
```
All other content of the three jobs (matrix, steps, image expression) is untouched.

- [ ] **Step 2: Verify the decision table by reading the final YAML**

Walk each scenario against the conditions and confirm:

| Scenario | deploy-preprod | approval-gate | deploy-api/consumer/worker |
|---|---|---|---|
| push `develop` (staging, no preprod svcs) | skipped (`[]`) | skipped (env != production) | **run** (env != production) |
| push `main` (production) | runs | pauses → approved | **run** after approval |
| push `main`, preprod deploy FAILS | fails | skipped (preprod not success/skipped) | **skipped** |
| push `main`, reviewer REJECTS | runs | fails (rejected) | **skipped** |
| dispatch production + dry_run | runs (prints plan) | skipped (dry_run) | **run** (dry_run bypass; plan only) |
| dispatch staging (no dry_run) | skipped | skipped | **run** |

- [ ] **Step 3: Validate YAML + actionlint (if available)**

Run:
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy.yml')); print('OK')"
command -v actionlint >/dev/null && actionlint .github/workflows/deploy.yml || echo "actionlint not installed — skipped"
```
Expected: `OK`; actionlint reports no errors for deploy.yml (or is skipped).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/deploy.yml
git commit -m "ci(deploy): gate production rollout behind preprod approval"
```

---

### Task 5: Write the GitHub setup runbook

**Files:**
- Create: `docs/deployment/preprod-approval-gate.md`

- [ ] **Step 1: Write the runbook**

Full content:
```markdown
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
```

- [ ] **Step 2: Commit**

```bash
git add docs/deployment/preprod-approval-gate.md
git commit -m "docs(deploy): runbook for prod-approval environment setup"
```

---

### Task 6: Final verification

- [ ] **Step 1: Full-file review**

Read the final `.github/workflows/deploy.yml` end-to-end and confirm:
- Job order/`needs` graph: `build`, `resolve-config` → `deploy-preprod` → `approval-gate` → `deploy-api`/`deploy-consumer`/`deploy-worker`.
- No job other than the three deploy jobs + `deploy-preprod` references `needs.build.outputs.image`.
- `environment: prod-approval` appears exactly once (the gate job).
- The staging path has no reference to preprod/gate other than skip conditions.

- [ ] **Step 2: Re-run validators**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/deploy.yml')); print('OK')"
command -v actionlint >/dev/null && actionlint .github/workflows/deploy.yml || echo "actionlint not installed — skipped"
```
Expected: `OK`, no actionlint errors.

- [ ] **Step 3: Commit the plan file (gitignored path, force-add)**

```bash
git add -f docs/superpowers/plans/2026-06-11-canary-preprod-gated-deploy.md
git commit -m "docs: implementation plan for canary preprod gated deploy"
```

---

## Out-of-band steps (cannot be done from this repo — user action)

1. Create the `prod-approval` GitHub Environment + required reviewers (runbook §1).
2. Update the `PROD_DEPLOY_TARGETS` Actions variable (runbook §2).
3. Dry-run verification + first real `main` merge watch (runbook §3).
```
