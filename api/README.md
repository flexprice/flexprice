# FlexPrice API SDKs

Generated SDKs and MCP server for the Flexprice API. Source: OpenAPI spec at `docs/swagger/swagger-3-0.json`.

## Layout

- **api/go** – Go SDK (Speakeasy)
- **api/typescript** – TypeScript SDK (Speakeasy)
- **api/python** – Python SDK (Speakeasy)
- **api/mcp** – MCP server (Speakeasy)
- **api/tests** – SDK integration tests (local vs published); see [api/tests/README.md](tests/README.md). Run `make test-sdk-local` or `make test-sdk-published` from repo root.

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

See [AGENTS.md](../AGENTS.md) and [.speakeasy/README.md](../.speakeasy/README.md) for details. Custom code lives under `api/custom/<lang>/` and is merged into `api/<lang>/` after each run. READMEs are maintained in `api/custom/<lang>/README.md` and overwrite the generated README on merge; `.genignore` in each SDK output dir prevents the generator from overwriting README if you run generate without merge-custom.

## Usage (high level)

- **Go:** `flexprice.New(serverURL, flexprice.WithSecurity(apiKey))` – see `api/go/README.md` and `api/go/examples/`.
- **TypeScript:** Import from the built package; optional custom `CustomerPortal` in `src/sdk/customer-portal.ts`.
- **Python:** Use the generated package; examples in `api/python/examples/` (may need updates for current SDK).
- **MCP:** Run from `api/mcp` (e.g. `npx . start`); set `FLEXPRICE_API_KEY` or per-README auth.

## CI/CD

Use the **Generate SDKs** workflow (`.github/workflows/generate-sdks.yml`) for Speakeasy generation (`make sdk-all`), merge-custom, and publish. See **Publishing** below.

## Publishing

The **Generate SDKs** workflow (`.github/workflows/generate-sdks.yml`) is the single pipeline: (1) generate SDKs, (2) push to GitHub repos, (3) publish to npm (TypeScript) and PyPI (Python). Go is published by the repo push in step 2.

- **Trigger:** Push to `main` (when `docs/swagger/**`, `.speakeasy/**`, `api/custom/**`, `cmd/**`, `internal/api/**`, or `Makefile` change) or manual run via **workflow_dispatch**. For manual runs, check "Push generated SDKs to GitHub repos" to run steps 2 and 3; leave unchecked to only generate.
- **Variables (optional):** `SDK_GO_REPO`, `SDK_PYTHON_REPO`, `SDK_TYPESCRIPT_REPO` (defaults: Random-test-v2/go-temp, py-temp, ts-temp).

**Secrets (Settings → Secrets and variables → Actions):**

| Secret                 | Used for                                                           |
| ---------------------- | ------------------------------------------------------------------ |
| `SPEAKEASY_API_KEY`    | Speakeasy CLI (generate step)                                      |
| `SDK_DEPLOY_GIT_TOKEN` | Push to GitHub repos (classic PAT with `repo` scope)               |
| `NPM_TOKEN`            | Publish TypeScript SDK and MCP to npm (granular token, read/write) |
| `PYPI_TOKEN`           | Publish Python SDK to PyPI                                         |

**Why GitHub publish can succeed but npm publish fails:** The GitHub step only copies files into a git repo; it does not care about `package.json` `name`. The npm step reads `package.json` and publishes under that name. If the name is reserved (e.g. `"mcp"`) or the token does not have publish permission for that package/scope, npm returns 403. The generate job runs `fix-mcp-package-name` (Makefile) so the MCP artifact has `"name": "@omkar273/mcp-temp"`. In the **publish-to-registries** job, the "Show package name" step logs the name from the downloaded artifact; if it still shows `mcp`, the generate run did not apply the fix (e.g. branch missing the Makefile change, or `jq` not available). For 403, also ensure `NPM_TOKEN` has **Publish** scope and access to the `@omkar273` scope (or the TypeScript package’s scope).

**When using act:** The publish-to-registries job runs three matrix jobs (TypeScript, Python, MCP) in parallel. If **one** of them fails (e.g. TypeScript npm publish), act may cancel the others with "context canceled", so MCP can show as failed even though the token works from CLI. In that case the **first** failure is the real one (e.g. TypeScript); fix that (check the TypeScript step log for `npm error` or 403/409) so the rest can complete. Python succeeded in your run; TypeScript failed first, then MCP was canceled during its build phase.

## Running with act (local)

You can run the Generate SDKs workflow locally with [act](https://github.com/nektos/act). Local runs often fail at **artifact handoff** between jobs (upload in `generate` → download in `publish-to-github` / `publish-to-registries`); the artifact server must be configured.

### Required secrets file (`.secrets`)

Create a `.secrets` file (gitignored) with **KEY=value** per line; keys must match exactly (case-sensitive):

```
SPEAKEASY_API_KEY=spk_...
SDK_DEPLOY_GIT_TOKEN=ghp_...
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
