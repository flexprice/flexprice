# White-Label SDK Configuration

Every Flexprice release automatically generates re-branded SDKs for white-label clients.
All client configuration lives in a single GitHub Actions secret — nothing is committed to this repo.

## How It Works

1. You publish a GitHub release (e.g. `v1.2.3`)
2. Both `generate-sdks.yml` (Flexprice) and `generate-wl-sdks.yml` (white-label) fire
3. The WL pipeline reads `WL_SDK_CONFIG`, builds a matrix by client index, and for each client:
   - Validates config and repo access
   - Substitutes branding into Speakeasy gen configs
   - Generates all four SDKs (Go, TypeScript, Python, MCP)
   - Verifies they compile
   - Pushes to client GitHub repos and optionally publishes to npm/PyPI

If `WL_SDK_CONFIG` is not set, the WL pipeline skips gracefully — Flexprice publishes normally.

## Setup

1. Go to: **Settings → Secrets and variables → Actions → Repository secrets**
2. Create secret named **`WL_SDK_CONFIG`**
3. Value is a JSON array — one object per white-label client

## Full Example

```json
[
  {
    "WL_SDK_CLASS_NAME":       "Acme",
    "WL_GO_MODULE_PATH":       "github.com/acmecorp/go-sdk",
    "WL_GO_PACKAGE_NAME":      "acme",
    "WL_TS_PACKAGE_NAME":      "@acmecorp/sdk",
    "WL_PYTHON_PACKAGE_NAME":  "acme-sdk",
    "WL_MCP_PACKAGE_NAME":     "@acmecorp/mcp-server",
    "WL_AUTHOR_NAME":          "Acme Corp",
    "WL_API_BASE_URL":         "https://api.acmecorp.io",
    "WL_GO_REPO":              "acmecorp/go-sdk",
    "WL_PYTHON_REPO":          "acmecorp/python-sdk",
    "WL_TS_REPO":              "acmecorp/javascript-sdk",
    "WL_MCP_REPO":             "acmecorp/mcp-server",
    "WL_SDK_DEPLOY_GIT_TOKEN": "github_pat_xxxx",
    "WL_PUBLISH_NPM":          "true",
    "WL_PUBLISH_PYPI":         "true",
    "WL_NPM_TOKEN":            "npm_xxxx",
    "WL_PYPI_TOKEN":           "pypi-xxxx",
    "WL_NPM_REGISTRY":         "https://registry.npmjs.org"
  }
]
```

## Field Reference

| Field | Required | Format | Description |
|---|---|---|---|
| `WL_SDK_CLASS_NAME` | Yes | `^[A-Z][a-zA-Z0-9]+$` | SDK root class — `new Acme(...)` |
| `WL_GO_MODULE_PATH` | Yes | `^github\.com/.+/.+` | Go module path used in `import` statements |
| `WL_GO_PACKAGE_NAME` | Yes | `^[a-z][a-z0-9_]+$` | Go package name — no hyphens (Go rule) |
| `WL_TS_PACKAGE_NAME` | Yes | `^@[a-z0-9-]+/[a-z0-9-]+$` | npm package — must include `@scope/` prefix |
| `WL_PYTHON_PACKAGE_NAME` | Yes | `^[a-z][a-z0-9_-]+$` | PyPI package name |
| `WL_MCP_PACKAGE_NAME` | Yes | `^@[a-z0-9-]+/[a-z0-9-]+$` | npm package for MCP server |
| `WL_AUTHOR_NAME` | Yes | Non-empty string | Author field in all package manifests |
| `WL_API_BASE_URL` | Yes | `^https?://.+` | Default API server baked into the SDK |
| `WL_GO_REPO` | Yes | `^[^/]+/[^/]+` | `org/repo` on GitHub — must exist |
| `WL_PYTHON_REPO` | Yes | `^[^/]+/[^/]+` | `org/repo` on GitHub — must exist |
| `WL_TS_REPO` | Yes | `^[^/]+/[^/]+` | `org/repo` on GitHub — must exist |
| `WL_MCP_REPO` | Yes | `^[^/]+/[^/]+` | `org/repo` on GitHub — must exist |
| `WL_SDK_DEPLOY_GIT_TOKEN` | Yes | GitHub PAT | Needs Contents R/W + Metadata Read on all 4 repos |
| `WL_PUBLISH_NPM` | No | `"true"` or omit | Publish TypeScript + MCP to npm (GitHub push always happens) |
| `WL_PUBLISH_PYPI` | No | `"true"` or omit | Publish Python to PyPI (GitHub push always happens) |
| `WL_NPM_TOKEN` | If `WL_PUBLISH_NPM=true` | npm token | Required when publishing to npm |
| `WL_PYPI_TOKEN` | If `WL_PUBLISH_PYPI=true` | PyPI token | Required when publishing to PyPI |
| `WL_NPM_REGISTRY` | No | URL | Defaults to `https://registry.npmjs.org` |

## Common Format Mistakes

| Field | Valid | Invalid |
|---|---|---|
| `WL_SDK_CLASS_NAME` | `Acme`, `AcmeCorp` | `acme` (lowercase start), `Acme Corp` (space) |
| `WL_GO_PACKAGE_NAME` | `acme`, `acme_sdk` | `acme-sdk` (hyphen invalid in Go) |
| `WL_TS_PACKAGE_NAME` | `@acme/sdk` | `acme-sdk` (missing `@scope/`) |
| `WL_API_BASE_URL` | `https://api.acme.io` | `api.acme.io` (missing scheme) |
| `WL_GO_REPO` | `acme/go-sdk` | `https://github.com/acme/go-sdk` |

## PAT Permissions for `WL_SDK_DEPLOY_GIT_TOKEN`

Create a **fine-grained personal access token** scoped to each of the 4 target repos with:
- **Contents**: Read and Write
- **Metadata**: Read (required automatically)

The PAT does NOT need access to the Flexprice repo itself.

## Adding a New Client

1. Open **Settings → Secrets → Actions → `WL_SDK_CONFIG`**
2. Append a new JSON object to the array
3. Save. Next release will include the new client automatically.

No workflow file changes required. Client names never appear in this repo.

## What Happens If `WL_SDK_CONFIG` Is Not Set

The `generate-wl-sdks.yml` pipeline checks for the secret at the start:
- Not set or empty string → skips gracefully (green ✅, no failure)
- Invalid JSON or non-array → fails with a clear error message
- Empty array `[]` → skips gracefully (green ✅)

The standard Flexprice SDK generation (`generate-sdks.yml`) is completely unaffected.

## Template Sync

When Speakeasy CLI auto-updates `.speakeasy/gen/*.yaml` (e.g., after a Speakeasy version bump), run:

```bash
make check-wl-templates
```

If drift is detected, update the corresponding `configs/white-label/templates/*.yaml.tmpl` file to match the new gen config while preserving the `${WL_*}` placeholders.
