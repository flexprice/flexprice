# Smoke-testing GHCR publish on `test/ghcr`

Workflow: [`.github/workflows/test-ghcr-publish.yml`](test-ghcr-publish.yml)

## What it does

On push to the `test/ghcr` branch (or `workflow_dispatch`), it:

1. Builds the FlexPrice app image **linux/amd64 only** and pushes
   `ghcr.io/<owner>/flexprice:test-<short-sha>` (and a `latest-test` floater).
2. Packages the helm chart with version `0.0.0-test-<short-sha>` and pushes it
   to `oci://ghcr.io/<owner>/charts/flexprice`.

It hits the **same GHCR repos as production** but with versions that sort
below any real release (`0.0.0-…` pre-release, plus a `test-…` image tag).

## One-time GitHub configuration

Do these once on the repo before the workflow can succeed.

### 1. Verify default `GITHUB_TOKEN` has the right permissions

Settings → **Actions → General → Workflow permissions**:

- ☑ **Read and write permissions**

If you don't want repo-wide write, leave it on "Read repository contents" and
make sure the workflow's `permissions:` block (which we set per-job) keeps
`packages: write`. The workflow does. So either setting works.

### 2. Create the GHCR package(s) by running the workflow once

GHCR packages don't exist until the first push. After the first successful
run, `ghcr.io/<owner>/flexprice` and `ghcr.io/<owner>/charts/flexprice` show
up under the **repo → Packages** sidebar.

### 3. Link packages to this repo so the default `GITHUB_TOKEN` can push later

GitHub auto-links a package to the repo that created it on first push. If
you ever rename the repo or move ownership, re-link via:

- `https://github.com/users/<owner>/packages/container/<package>/settings`
- **Manage Actions access** → **Add Repository** → pick this repo →
  **Role: Write**.

This is a no-op for new repos. Only matters if the package pre-existed.

### 4. (Optional) Make the GHCR packages public

By default GHCR packages are **private**, which means `helm pull` and
`docker pull` require auth. To make the OSS chart freely pullable:

- `https://github.com/users/<owner>/packages/container/<package>/settings`
- **Danger Zone** → **Change visibility** → Public

Do this for **both**:
- `ghcr.io/<owner>/flexprice` (the app image)
- `ghcr.io/<owner>/charts/flexprice` (the chart)

If you skip this, OSS users will need a personal access token (PAT) with
`read:packages` scope to pull, and the chart's `imagePullSecrets`
documentation in `values-prod.example.yaml` applies.

### 5. Branch protection (optional)

If `test/ghcr` becomes a long-lived branch, add a branch protection rule:

- Settings → **Branches → Add rule** for `test/ghcr`
- ☑ Require pull request reviews before merging — **off**, this is a sandbox
- ☑ Restrict who can push — only people you trust (workflow runs use their
  identity for the GHCR push)

## How to run

### From the CLI

```bash
git checkout test/ghcr
git push origin test/ghcr
```

The push triggers the workflow. Watch it under **Actions → Test GHCR Publish**.

### From the GitHub UI

- **Actions → Test GHCR Publish → Run workflow** → pick the `test/ghcr`
  branch → optionally enter a `tag_suffix` to override the default short-SHA
  tagging.

## After a successful run

```bash
# Pull the test image
docker pull ghcr.io/<owner>/flexprice:test-<sha>

# Pull the test chart
helm pull oci://ghcr.io/<owner>/charts/flexprice --version 0.0.0-test-<sha>

# Dry-install
helm install flexprice-rc \
  oci://ghcr.io/<owner>/charts/flexprice \
  --version 0.0.0-test-<sha> \
  -n flexprice-rc --create-namespace \
  --set secrets.existingSecret=flexprice-secrets \
  --dry-run --debug
```

## Cleanup

Test versions accumulate fast. Periodically delete them:

```bash
# Image: GHCR UI → Package → "Manage versions" → delete tagged versions
# Chart: same, under the charts/flexprice package
```

Or use the GitHub CLI (requires `gh auth login` with `delete:packages`):

```bash
gh api -X DELETE "/users/<owner>/packages/container/flexprice/versions/<version-id>"
gh api -X DELETE "/users/<owner>/packages/container/charts%2Fflexprice/versions/<version-id>"
```

## When this workflow gets removed

Once production publish is proven (`publish-app-image.yml` and
`publish-helm-chart.yml` succeed on a real tag), delete this workflow and
the `test/ghcr` branch — there's no reason to keep it once the prod path
works.
