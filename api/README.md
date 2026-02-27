# flexprice API SDKs

Generated SDKs and MCP server for the flexprice API. Source: OpenAPI spec at `docs/swagger/swagger-3-0.json`.

## Layout

- **api/go** – Go SDK
- **api/typescript** – TypeScript SDK
- **api/python** – Python SDK
- **api/mcp** – MCP server
- **api/tests** – SDK integration tests (published SDKs only); see [api/tests/README.md](tests/README.md). Run `make test-sdk` or `make test-sdks` from repo root.

## Generation

```bash
# Validate, generate all SDKs + MCP, merge custom files
make sdk-all
```

When the API changes, regenerate the spec first:

```bash
make swagger
make sdk-all
```

See [AGENTS.md](../AGENTS.md) and [.speakeasy/README.md](../.speakeasy/README.md) for details. SDK generation config (gen.yaml) lives in `.speakeasy/gen/` and is copied to `api/<lang>/.speakeasy/` before each generate. Custom code lives under `api/custom/<lang>/` and is merged into `api/<lang>/` after each run. READMEs are maintained in `api/custom/<lang>/README.md` and overwrite the generated README on merge; `.genignore` in each SDK output dir prevents the generator from overwriting README if you run generate without merge-custom.

## Usage (high level)

- **API base URL:** Use `https://us.api.flexprice.io/v1` for all SDKs and the MCP server. Always include `/v1` in the base URL; no trailing space or trailing slash.
- **Go:** `flexprice.New(serverURL, flexprice.WithSecurity(apiKey))` – see `api/go/README.md` and `api/go/examples/`.
- **TypeScript:** `npm i @flexprice/sdk` then import `Flexprice` / `FlexPrice`; optional custom `CustomerPortal` in `src/sdk/customer-portal.ts`.
- **Python:** `pip install flexprice` then `from flexprice import Flexprice`; examples in `api/python/examples/`.
- **MCP:** `npm i @flexprice/mcp-server`; run with `npx @flexprice/mcp-server start` (or from `api/mcp`); set `FLEXPRICE_API_KEY` or per-README auth.

## CI/CD

Use the **Generate SDKs** workflow (`.github/workflows/generate-sdks.yml`) for SDK generation (`make sdk-all`), merge-custom, and publish. See **Publishing** below.

## Publishing

The **Generate SDKs** workflow (`.github/workflows/generate-sdks.yml`) is the single pipeline: (1) generate SDKs, (2) push to GitHub repos, (3) publish to npm (TypeScript) and PyPI (Python). Go is published by the repo push in step 2.

- **Trigger:** Push to `main` (when `docs/swagger/**`, generator config, `api/custom/**`, `cmd/**`, `internal/api/**`, or `Makefile` change) or manual run via **workflow_dispatch**. For manual runs, check "Push generated SDKs to GitHub repos" to run steps 2 and 3; leave unchecked to only generate.
- **Variables (optional):** `SDK_GO_REPO`, `SDK_PYTHON_REPO`, `SDK_TYPESCRIPT_REPO`, `SDK_MCP_REPO` (defaults: flexprice/go-sdk, flexprice/python-sdk, flexprice/javascript-sdk, flexprice/mcp-server).

**Secrets (Settings → Secrets and variables → Actions):**

| Secret                 | Used for                                                                                              |
| ---------------------- | ----------------------------------------------------------------------------------------------------- |
| `SPEAKEASY_API_KEY`    | SDK generator CLI (generate step)                                                                     |
| `SDK_DEPLOY_GIT_TOKEN` | Push to GitHub repos (fine-grained PAT: Contents Read and write, Metadata Read on selected SDK repos) |
| `NPM_TOKEN`            | Publish TypeScript SDK and MCP to npm (granular token, read/write)                                    |
| `PYPI_TOKEN`           | Publish Python SDK to PyPI                                                                            |

**Fine-grained token setup (SDK_DEPLOY_GIT_TOKEN):** Create a [fine-grained personal access token](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens#creating-a-fine-grained-personal-access-token) under the account or org that owns the SDK repos. Use **Only select repositories** and add every repo the workflow pushes to (defaults: go-temp, py-temp, ts-temp; add any overridden via `SDK_GO_REPO` etc.). Under **Repository permissions** set **Contents** to **Read and write** and **Metadata** to **Read**. No other permissions are required.

**Published packages:** Python: `pip install flexprice` | TypeScript: `npm i @flexprice/sdk` | MCP: `npm i @flexprice/mcp-server`.

**Why GitHub publish can succeed but npm publish fails:** The GitHub step only copies files into a git repo; it does not care about `package.json` `name`. The npm step reads `package.json` and publishes under that name. If the name is reserved (e.g. `"mcp"`) or the token does not have publish permission for that package/scope, npm returns 403. The generate job runs `fix-mcp-package-name` (Makefile) so the MCP artifact has `"name": "@flexprice/mcp-server"`. In the **publish-to-registries** job, the "Show package name" step logs the name from the downloaded artifact. For 403, ensure `NPM_TOKEN` has **Publish** scope and access to the `@flexprice` scope.

**When using act:** The publish-to-registries job runs three matrix jobs (TypeScript, Python, MCP) in parallel. If **one** of them fails (e.g. TypeScript npm publish), act may cancel the others with "context canceled", so MCP can show as failed even though the token works from CLI. In that case the **first** failure is the real one (e.g. TypeScript); fix that (check the TypeScript step log for `npm error` or 403/409) so the rest can complete. Python succeeded in your run; TypeScript failed first, then MCP was canceled during its build phase.

## Running with act (local)

You can run the Generate SDKs workflow locally with [act](https://github.com/nektos/act). Local runs often fail at **artifact handoff** between jobs (upload in `generate` → download in `publish-to-github` / `publish-to-registries`); the artifact server must be configured.

### Required secrets file (`.secrets`)

Create a `.secrets` file (gitignored) with **KEY=value** per line; keys must match exactly (case-sensitive):

```
SPEAKEASY_API_KEY=spk_...
# Fine-grained PAT: Contents (Read and write) + Metadata (Read) on SDK repos
SDK_DEPLOY_GIT_TOKEN=github_pat_...
NPM_TOKEN=npm_...
PYPI_TOKEN=pypi-...
```

### Run the full pipeline (generate + push to GitHub + publish to registries)

```bash
act workflow_dispatch \
  -W .github/workflows/generate-sdks.yml \
  --secret-file .secrets \
  --artifact-server-path "$(pwd)/.artifacts" \
  -v
```

`-v` is verbose so you can see which step fails. Artifacts are stored under `.artifacts/` (gitignored).

### Isolate artifact issues (generate only, no push/publish)

If the failure is in **download-artifact** or in **publish-to-registries** (e.g. empty `sdk/`), the cause is usually artifact handoff in act. To run only the **generate** job and verify uploads:

```bash
act workflow_dispatch \
  -W .github/workflows/generate-sdks.yml \
  -e .github/workflows/event-generate-sdks-no-publish.json \
  --secret-file .secrets \
  --artifact-server-path "$(pwd)/.artifacts" \
  -v
```

Then inspect `.artifacts/` for the expected artifact names (`api-go`, `api-typescript`, `api-python`, `api-mcp`). If generate succeeds but a full run fails on download or npm publish, use a clean `.artifacts` dir, ensure the path exists and is writable, and try a recent act version.

### Verify token and build on the host (no act)

To confirm the same token and `make sdk-all` work as in CI:

```bash
set -a && . ./.secrets && set +a
make sdk-all
```

Then from `api/mcp`: `echo "//registry.npmjs.org/:_authToken=${NPM_TOKEN}" >> .npmrc`, `npm run build`, `npm publish --access public`. If that works, act failures are likely secrets injection or artifact transfer.
