# Speakeasy SDK and MCP setup

This directory configures Speakeasy code generation for Flexprice: **api/go**, **api/typescript**, **api/python**, and **api/mcp** from [docs/swagger/swagger-3-0.json](../docs/swagger/swagger-3-0.json).

## SDK version (unique every run so publish never fails)

- **When Speakeasy bumps on its own**: Speakeasy’s automatic versioning only bumps when the **generator** changes, **gen.yaml** (or checksum) changes, or **OpenAPI** (e.g. `info.version`) changes. It does **not** bump on every run, so re-running without changes can produce the same version and cause npm/PyPI publish to fail.
- **Our behavior**: Every `make sdk-all` and every CI run uses a **unique** version so publish never fails:
  - **Local**: If you don’t pass `VERSION`, the Makefile uses `./scripts/next-sdk-version.sh patch` (reads `.speakeasy/sdk-version.json`), then `./scripts/sync-sdk-version-to-gen.sh <next>` to write that version into all gen.yaml and sdk-version.json, then generates.
  - **CI**: Resolves the next version with `./scripts/next-sdk-version.sh patch "$BASE"` where BASE is max(npm `flexprice-ts` version, `.speakeasy/sdk-version.json`). Then runs `make sdk-all VERSION=<resolved>`. After generate, CI runs `./scripts/sync-sdk-version-to-gen.sh <version>` so gen.yaml and sdk-version.json match the generated version.
- **Scripts**:
  - `./scripts/next-sdk-version.sh [major|minor|patch] [baseVersion]` – Prints the next version (no write). Used by CI and by `make sdk-all` when `VERSION` is not set. Omit `baseVersion` to use `.speakeasy/sdk-version.json`.
  - `./scripts/sync-sdk-version-to-gen.sh <VERSION>` – Writes `<VERSION>` into all `api/*/.speakeasy/gen.yaml` and `.speakeasy/sdk-version.json` (run before generate in Makefile; run after generate in CI).

## Workflow

- **workflow.yaml** – Sources (OpenAPI spec + overlays) and targets (one per SDK/MCP). Add targets via `speakeasy configure targets` with output paths: `api/go`, `api/typescript`, `api/python`, `api/mcp`.
- **overlays/flexprice-sdk.yaml** – OpenAPI overlay to add `x-speakeasy-mcp` (scopes, descriptions, hints), improve operation summaries, or schema docs without editing the main spec.

## Recommended gen.yaml (per target)

When each target is created, Speakeasy generates a `gen.yaml` in that target’s output directory. Apply these for best quality:

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
- **MCP:** `mcpbManifestOverlay.displayName: "Flexprice"`, `validateResponse: false` for robustness; package `@flexprice/mcp`

### Retries (production)

Use Speakeasy retry support (e.g. `x-speakeasy-retries` in OpenAPI or generator options) with exponential backoff for 5xx and transient errors.

## Commands

- `make sdk-all` – Validate + generate all SDKs/MCP + merge custom (uses existing docs/swagger/swagger-3-0.json).
- `make swagger` – Regenerate OpenAPI spec from code; run this when the API changes, then `make sdk-all`.
- `make speakeasy-validate` – Validate OpenAPI spec.
- `make speakeasy-generate` – Validate + lint + run Speakeasy (all targets).
- `make go-sdk` – Clean, swagger, validate, lint, generate Go SDK, copy custom, merge-custom, build.
- `make merge-custom` – Merge `api/custom/<lang>/` into `api/<lang>/`.

## Custom code

See [api/custom/README.md](../api/custom/README.md). Custom files live under `api/custom/<lang>/` and are merged into `api/<lang>/` after every generation.
