# SDK and MCP setup

This directory configures SDK code generation for flexprice: **api/go**, **api/typescript**, **api/python**, and **api/mcp** from [docs/swagger/swagger-3-0.json](../docs/swagger/swagger-3-0.json).

## Central config (gen.yaml)

- **Source of truth:** `.speakeasy/gen/go.yaml`, `.speakeasy/gen/typescript.yaml`, `.speakeasy/gen/python.yaml`, `.speakeasy/gen/mcp.yaml`. Edit these; do not edit `api/<lang>/.speakeasy/gen.yaml` (it is copied from here at build time and may be gitignored).
- **Before generate:** `make speakeasy-generate` (and thus `make sdk-all`) runs `./scripts/sync-gen-to-output.sh`, which copies `.speakeasy/gen/<lang>.yaml` into `api/<lang>/.speakeasy/gen.yaml` so the Speakeasy CLI finds config in each target output dir.

## SDK version (unique every run so publish never fails)

- **When the generator bumps on its own**: Automatic versioning only bumps when the generator changes, **gen.yaml** (or checksum) changes, or **OpenAPI** (e.g. `info.version`) changes. It does **not** bump on every run, so re-running without changes can produce the same version and cause npm/PyPI publish to fail.
- **Our behavior**: Every `make sdk-all` and every CI run uses a **unique** version so publish never fails:
  - **Local**: If you don’t pass `VERSION`, the Makefile uses `./scripts/next-sdk-version.sh patch` (reads `.speakeasy/sdk-version.json`), then `./scripts/sync-sdk-version-to-gen.sh <next>` to write that version into **`.speakeasy/gen/*.yaml`** and `.speakeasy/sdk-version.json`, then generates.
  - **CI**: Uses version from version-check (sdk-version.json / .speakeasy/gen/go.yaml). Runs `make sdk-all VERSION=<version>`. After generate, CI only runs `force-sdk-version.sh` to overwrite version in artifacts; central gen is already updated before generate.
- **Scripts**:
  - `./scripts/next-sdk-version.sh [major|minor|patch] [baseVersion]` – Prints the next version (no write). Used by CI and by `make sdk-all` when `VERSION` is not set.
  - `./scripts/sync-sdk-version-to-gen.sh <VERSION>` – Writes `<VERSION>` into **`.speakeasy/gen/*.yaml`** and `.speakeasy/sdk-version.json` (run before generate in Makefile).
  - `./scripts/sync-gen-to-output.sh` – Copies `.speakeasy/gen/<lang>.yaml` to `api/<lang>/.speakeasy/gen.yaml` (run automatically before `speakeasy run`).

## Workflow

- **workflow.yaml** – Sources (OpenAPI spec + overlays) and targets (one per SDK/MCP). Add targets via the generator CLI with output paths: `api/go`, `api/typescript`, `api/python`, `api/mcp`.
- **overlays/flexprice-sdk.yaml** – OpenAPI overlay for MCP (scopes, descriptions, hints), improve operation summaries, or schema docs without editing the main spec.
- **mcp/allowed-tags.yaml** – Tags from OpenAPI that are exposed as MCP tools. Edit this file, then run `make filter-mcp-spec` and `make sdk-all` to regenerate the MCP spec and SDK.

## Recommended gen.yaml (per target)

The canonical gen config lives in `.speakeasy/gen/<lang>.yaml`. Apply these for best quality:

### Generation (all targets)

- `sdkClassName: Flexprice`
- `maintainOpenAPIOrder: true`
- `usageSnippets.optionalPropertyRendering: withExample`
- `fixes.securityFeb2025: true`, `requestResponseComponentNamesFeb2024: true`, `parameterOrderingFeb2024: true`, `nameResolutionDec2023: true`
- `repoUrl` and `repoSubDirectory` (e.g. `api/go`) for package metadata

### Language-specific

- **Go:** `maxMethodParams: 4`, `methodArguments: "infer-optional-args"`, `modulePath` (e.g. `github.com/flexprice/flexprice-go`), `sdkPackageName: flexprice`
- **TypeScript:** `packageName: "@flexprice/sdk"`, `generateExamples: true`
- **Python:** `packageName: flexprice`, `moduleName: flexprice`, `packageManager: uv`
- **MCP:** `mcpbManifestOverlay.displayName: "flexprice"`, `validateResponse: false` for robustness; package `@flexprice/mcp`

### Retries (production)

Use retry support (e.g. in OpenAPI or generator options) with exponential backoff for 5xx and transient errors.

## Commands

- `make sdk-all` – Validate + generate all SDKs/MCP + merge custom (uses existing docs/swagger/swagger-3-0.json).
- `make swagger` – Regenerate OpenAPI spec from code; run this when the API changes, then `make sdk-all`.
- `make speakeasy-validate` – Validate OpenAPI spec.
- `make speakeasy-generate` – Validate + lint + run generator (all targets).
- `make go-sdk` – Clean, validate, sync gen to output, generate Go SDK, merge-custom, build.
- `make merge-custom` – Merge `api/custom/<lang>/` into `api/<lang>/`.

## Custom code

See [api/custom/README.md](../api/custom/README.md). Custom files live under `api/custom/<lang>/` and are merged into `api/<lang>/` after every generation.
