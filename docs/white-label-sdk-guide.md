# White-Label SDK Guide

This guide covers everything from first-time setup to handing SDKs off to a client — including where to get each token, what every configuration field controls, how end users install and import the SDK, and how to test and verify locally before a release.

---

## Table of Contents

1. [What Gets Generated](#1-what-gets-generated)
2. [How the Pipeline Works](#2-how-the-pipeline-works)
3. [Tokens & Secrets You Need](#3-tokens--secrets-you-need)
4. [Configuration Field Reference](#4-configuration-field-reference)
5. [Setting Up WL_SDK_CONFIG](#5-setting-up-wl_sdk_config)
6. [Local Testing](#6-local-testing)
7. [Verifying the Generated SDKs](#7-verifying-the-generated-sdks)
8. [Triggering a Release](#8-triggering-a-release)
9. [What the Client Gets](#9-what-the-client-gets)

---

## 1. What Gets Generated

For each white-label client, the pipeline produces **four fully re-branded SDKs**:

| SDK | Language | Pushed To |
|-----|----------|-----------|
| Go SDK | Go | Client's GitHub repo |
| TypeScript SDK | TypeScript/JavaScript | Client's GitHub repo + npm (optional) |
| Python SDK | Python | Client's GitHub repo + PyPI (optional) |
| MCP Server | TypeScript | Client's GitHub repo + npm (optional) |

Every Flexprice reference — class names, package names, import paths, log strings, API base URL — is replaced with the client's brand.

**Example: what a developer at the client's customer sees**

```bash
# Go
go get github.com/acmecorp/go-sdk/v2

# TypeScript / JavaScript
npm install @acmecorp/sdk

# Python
pip install acme-sdk

# MCP (AI tool integrations)
npx @acmecorp/mcp-server
```

```go
// Go import
import acme "github.com/acmecorp/go-sdk/v2"

client := acme.NewAcme(acme.WithSecurity("API_KEY"))
```

```typescript
// TypeScript import
import { Acme } from "@acmecorp/sdk";

const client = new Acme({ apiKey: "API_KEY" });
```

```python
# Python import
from acme_sdk import Acme

client = Acme(api_key="API_KEY")
```

---

## 2. How the Pipeline Works

```
GitHub Release (v2.x.x)
        │
        ▼
generate-wl-sdks.yml fires
        │
        ├── matrix-setup job
        │     Reads WL_SDK_CONFIG secret → builds [0, 1, 2, ...] matrix
        │
        └── generate-wl job (one parallel run per client)
              │
              ├── Parse + validate client config
              ├── Check client repos are accessible
              ├── make swagger          (generate OpenAPI spec)
              ├── generate-wl-configs.sh  (brand .speakeasy/ configs)
              ├── speakeasy run         (generate all 4 SDKs)
              ├── apply-wl-custom-branding.sh  (fix custom files)
              ├── verify-sdk-builds.sh  (compile check + branding check)
              ├── Push Go / Python / TypeScript / MCP → client GitHub repos
              ├── Create GitHub release in client's Go repo
              └── Publish to npm / PyPI (if enabled)
```

Each client runs **in parallel** and **independently** — one client failing does not cancel others (`fail-fast: false`).

---

## 3. Tokens & Secrets You Need

### Required always

#### `SPEAKEASY_API_KEY`
**What it's for:** Speakeasy CLI authentication — required to generate SDKs.

**Where to get it:**
1. Go to [app.speakeasy.com](https://app.speakeasy.com)
2. Log in → navigate to **API Keys** (top-right profile menu)
3. Create a new key or copy an existing one
4. Add to GitHub: **Settings → Secrets → Actions → `SPEAKEASY_API_KEY`**

---

#### `WL_SDK_DEPLOY_GIT_TOKEN` (per client, inside `WL_SDK_CONFIG`)
**What it's for:** Clones and pushes to the client's four GitHub repos (Go, TypeScript, Python, MCP).

**Where to get it:**
1. The token must belong to a GitHub account (or bot account) that has **write access** to all four client repos
2. Go to: [github.com/settings/tokens](https://github.com/settings/tokens)
3. Click **"Generate new token (classic)"**
4. Scopes needed: `repo` (full control of private repositories)
5. Or use a **Fine-grained personal access token** (recommended):
   - Resource owner: the org that owns the client repos
   - Repository access: select the 4 client repos
   - Permissions: **Contents → Read and Write**, **Metadata → Read**

The token goes **inside the `WL_SDK_CONFIG` JSON** (not as a top-level secret), so it stays client-specific and masked:

```json
{
  "WL_SDK_DEPLOY_GIT_TOKEN": "github_pat_11AAAA..."
}
```

---

#### `WL_NPM_TOKEN` (per client, only if `WL_PUBLISH_NPM: "true"`)
**What it's for:** Publishes TypeScript SDK and MCP server to npm.

**Where to get it:**
1. Go to [npmjs.com](https://www.npmjs.com) → log in as the client's npm account
2. Click profile → **Access Tokens** → **Generate New Token**
3. Type: **Automation** (works in CI without 2FA)
4. Scope: the token needs publish access to the package namespace (e.g. `@acmecorp/`)
5. If publishing to a private registry (`WL_NPM_REGISTRY`), get the token from that registry's dashboard

Embed in `WL_SDK_CONFIG`:
```json
{
  "WL_NPM_TOKEN": "npm_xxxx..."
}
```

---

#### `WL_PYPI_TOKEN` (per client, only if `WL_PUBLISH_PYPI: "true"`)
**What it's for:** Publishes Python SDK to PyPI.

**Where to get it:**
1. Go to [pypi.org](https://pypi.org) → log in as the client's PyPI account
2. Click your username → **Account settings** → scroll to **API tokens**
3. Click **Add API token**
4. Scope: **Entire account** (for first publish) or restrict to the specific project once it exists

Embed in `WL_SDK_CONFIG`:
```json
{
  "WL_PYPI_TOKEN": "pypi-xxxx..."
}
```

---

### Secret precedence at runtime

```
SPEAKEASY_API_KEY  →  top-level GitHub secret (shared across all clients)
WL_SDK_CONFIG      →  top-level GitHub secret (contains per-client secrets)
  └── WL_SDK_DEPLOY_GIT_TOKEN  (per client)
  └── WL_NPM_TOKEN             (per client, optional)
  └── WL_PYPI_TOKEN            (per client, optional)
```

All three per-client sensitive values are **masked** with `::add-mask::` before they ever touch `$GITHUB_ENV`, so they won't appear in any workflow log.

---

## 4. Configuration Field Reference

This table maps every field in `WL_SDK_CONFIG` to its effect on the generated SDKs and the end-user developer experience.

### Identity fields

| Field | Example value | What it controls |
|-------|--------------|------------------|
| `WL_SDK_CLASS_NAME` | `Acme` | The main SDK class name. End users write `new Acme()` / `acme.NewAcme()` / `Acme()`. Must be `PascalCase`, letters + digits only. |
| `WL_AUTHOR_NAME` | `"Acme Corp"` | Author/organization name in package metadata (`package.json`, `pyproject.toml`). Shown in `npm info` and `pip show`. |
| `WL_API_BASE_URL` | `https://api.acme.com` | The HTTP base URL all SDK requests target. End users do not set this — it's baked in. Must include protocol, no trailing slash. |

**Effect of `WL_SDK_CLASS_NAME = "Acme"`:**

```go
// Go — every method and type is prefixed with this name
client := acme.NewAcme(acme.WithSecurity(token))
//                ^^^   ^^^
```
```typescript
const client = new Acme({ apiKey: token });
//                  ^^^^
```
```python
client = Acme(api_key=token)
#        ^^^^
```

---

### Go SDK fields

| Field | Example value | What it controls |
|-------|--------------|------------------|
| `WL_GO_MODULE_PATH` | `github.com/acmecorp/go-sdk` | The Go module path (without `/v2`). End users `go get <this>/v2`. Appears in `go.mod` and every import. |
| `WL_GO_PACKAGE_NAME` | `acme` | The Go package name (lowercase, no dashes). End users write `import acme "<module>/v2"`. |
| `WL_GO_REPO` | `acmecorp/go-sdk` | The GitHub repository (`org/repo`) the SDK is pushed to. End users clone or `go get` from here. |

**End-user Go experience:**
```bash
go get github.com/acmecorp/go-sdk/v2
```
```go
import acme "github.com/acmecorp/go-sdk/v2"
//           ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
//           WL_GO_MODULE_PATH + /v2

client := acme.NewAcme(
//        ^^^^ ^^^^^^^^
//   WL_GO_PACKAGE_NAME  WL_SDK_CLASS_NAME
    acme.WithSecurity("sk_live_xxx"),
)
```

> **Note:** The `/v2` suffix is appended automatically. Set `WL_GO_MODULE_PATH` without it.

---

### TypeScript / JavaScript SDK fields

| Field | Example value | What it controls |
|-------|--------------|------------------|
| `WL_TS_PACKAGE_NAME` | `@acmecorp/sdk` | The npm package name. End users `npm install <this>`. Must follow `@scope/name` format. |
| `WL_TS_REPO` | `acmecorp/javascript-sdk` | GitHub repo the SDK is pushed to. End users can also install directly from GitHub. |

**End-user TypeScript experience:**
```bash
npm install @acmecorp/sdk
#            ^^^^^^^^^^^^
#            WL_TS_PACKAGE_NAME
```
```typescript
import { Acme } from "@acmecorp/sdk";
//       ^^^^          ^^^^^^^^^^^^
//  WL_SDK_CLASS_NAME  WL_TS_PACKAGE_NAME

const client = new Acme({ apiKey: "sk_live_xxx" });
```
```javascript
// CommonJS
const { Acme } = require("@acmecorp/sdk");
```

---

### Python SDK fields

| Field | Example value | What it controls |
|-------|--------------|------------------|
| `WL_PYTHON_PACKAGE_NAME` | `acme-sdk` | The PyPI distribution name. End users `pip install <this>`. Dashes allowed. |
| `WL_PYTHON_REPO` | `acmecorp/python-sdk` | GitHub repo the SDK is pushed to. |

> **Auto-derived:** The Python **module name** (used for `import`) is automatically derived from `WL_PYTHON_PACKAGE_NAME` by replacing dashes with underscores. `acme-sdk` → `acme_sdk`. You do **not** need to set this separately.

**End-user Python experience:**
```bash
pip install acme-sdk
#           ^^^^^^^^
#           WL_PYTHON_PACKAGE_NAME
```
```python
from acme_sdk import Acme
#    ^^^^^^^^        ^^^^
#  WL_PYTHON_PACKAGE_NAME  WL_SDK_CLASS_NAME
# (dashes → underscores)

client = Acme(api_key="sk_live_xxx")

# Async
import asyncio
from acme_sdk import AsyncAcme

async def main():
    client = AsyncAcme(api_key="sk_live_xxx")
```

---

### MCP Server fields

| Field | Example value | What it controls |
|-------|--------------|------------------|
| `WL_MCP_PACKAGE_NAME` | `@acmecorp/mcp-server` | The npm package name for the MCP server. End users run it with `npx`. Must follow `@scope/name`. |
| `WL_MCP_REPO` | `acmecorp/mcp-server` | GitHub repo the MCP server is pushed to. |

**End-user MCP experience:**

The MCP server lets AI assistants (Claude, Cursor, Copilot) call the client's API directly.

```json
// Claude Desktop: ~/Library/Application Support/Claude/claude_desktop_config.json
{
  "mcpServers": {
    "acmecorp": {
      "command": "npx",
      "args": ["-y", "@acmecorp/mcp-server"],
      "env": { "ACME_API_KEY": "sk_live_xxx" }
    }
  }
}
```
```json
// Cursor: .cursor/mcp.json
{
  "mcpServers": {
    "acmecorp": {
      "command": "npx",
      "args": ["-y", "@acmecorp/mcp-server"],
      "env": { "ACME_API_KEY": "sk_live_xxx" }
    }
  }
}
```

---

### Publishing fields (optional)

| Field | Values | What it controls |
|-------|--------|------------------|
| `WL_PUBLISH_NPM` | `"true"` / `"false"` | Whether to publish TypeScript SDK and MCP to npm after pushing to GitHub. Default: `"false"`. |
| `WL_PUBLISH_PYPI` | `"true"` / `"false"` | Whether to publish Python SDK to PyPI after pushing to GitHub. Default: `"false"`. |
| `WL_NPM_REGISTRY` | `https://registry.npmjs.org` | npm registry URL. Override for private registries (GitHub Packages, Verdaccio, etc.). Default: `https://registry.npmjs.org`. |

> If `WL_PUBLISH_NPM = "false"`, the SDK is still pushed to GitHub. End users can install directly:
> ```bash
> npm install acmecorp/javascript-sdk
> ```

---

## 5. Setting Up WL_SDK_CONFIG

### Step 1: Create the client repos on GitHub

Create four empty GitHub repositories (with a README so they're non-empty):

| Language | Repo name (example) |
|----------|-------------------|
| Go | `acmecorp/go-sdk` |
| TypeScript | `acmecorp/javascript-sdk` |
| Python | `acmecorp/python-sdk` |
| MCP | `acmecorp/mcp-server` |

### Step 2: Build the JSON config

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
    "WL_API_BASE_URL":         "https://api.acme.com",
    "WL_GO_REPO":              "acmecorp/go-sdk",
    "WL_PYTHON_REPO":          "acmecorp/python-sdk",
    "WL_TS_REPO":              "acmecorp/javascript-sdk",
    "WL_MCP_REPO":             "acmecorp/mcp-server",
    "WL_SDK_DEPLOY_GIT_TOKEN": "github_pat_11AAAA...",
    "WL_PUBLISH_NPM":          "true",
    "WL_PUBLISH_PYPI":         "true",
    "WL_NPM_TOKEN":            "npm_xxxx...",
    "WL_PYPI_TOKEN":           "pypi-xxxx..."
  }
]
```

For **multiple clients**, add more objects to the array:
```json
[
  { ... client A ... },
  { ... client B ... }
]
```

Each runs in parallel. One failure does not cancel the other.

### Step 3: Add to GitHub

1. Go to your Flexprice repo → **Settings → Secrets and variables → Actions**
2. Click **New repository secret**
3. Name: **`WL_SDK_CONFIG`**
4. Value: paste the full JSON array
5. Also ensure **`SPEAKEASY_API_KEY`** is set as a separate secret

---

## 6. Local Testing

Use the test harness script to run the full pipeline locally before pushing a release.

### Prerequisites

```bash
go version      # 1.23+
node --version  # 20+
python3 --version # 3.12+
jq --version
speakeasy --version  # install: curl -fsSL https://raw.githubusercontent.com/speakeasy-api/speakeasy/main/install.sh | sh
```

### One-time setup: set env vars

Create a local env file (do **not** commit this):

```bash
# .env.wl-test  (add to .gitignore)
export SPEAKEASY_API_KEY=your_speakeasy_key

export WL_SDK_CLASS_NAME=Acme
export WL_GO_MODULE_PATH=github.com/acmecorp/go-sdk
export WL_GO_PACKAGE_NAME=acme
export WL_TS_PACKAGE_NAME=@acmecorp/sdk
export WL_PYTHON_PACKAGE_NAME=acme-sdk
export WL_MCP_PACKAGE_NAME=@acmecorp/mcp-server
export WL_AUTHOR_NAME="Acme Corp"
export WL_API_BASE_URL=https://api.acme.com
export WL_GO_REPO=acmecorp/go-sdk
export WL_PYTHON_REPO=acmecorp/python-sdk
export WL_TS_REPO=acmecorp/javascript-sdk
export WL_MCP_REPO=acmecorp/mcp-server
# WL_SDK_DEPLOY_GIT_TOKEN not needed locally (push step is skipped)
```

### Run the harness

```bash
source .env.wl-test
bash scripts/test-wl-local.sh
```

The script runs all 8 pipeline steps, prints a spot-check table, and **automatically restores `.speakeasy/`** when done (whether it passes or fails).

### Override the version

```bash
VERSION=2.1.0 bash scripts/test-wl-local.sh
```

> **Why VERSION must be 2.x:** The Go SDK module path gets a `/v2` suffix (e.g. `github.com/acmecorp/go-sdk/v2`) only when the version is 2.x or higher. Using `0.x` produces a `/v0` module path, which doesn't match the internal imports in the custom Go files.

---

## 7. Verifying the Generated SDKs

After running `test-wl-local.sh` or inspecting a CI run, check these files:

### Go SDK — `api/go/`

```bash
# Module path (should end in /v2)
head -1 api/go/go.mod
# Expected: module github.com/acmecorp/go-sdk/v2

# Package declaration in SDK entry point
grep '^package' api/go/sdk.go
# Expected: package acme

# No residual Flexprice references
grep -r 'flexprice\|Flexprice\|FlexPrice' api/go/ --include="*.go" | grep -v '_test.go'
# Expected: no output
```

### TypeScript SDK — `api/typescript/`

```bash
# Package name and version
jq '{name, version, author}' api/typescript/package.json
# Expected: {"name": "@acmecorp/sdk", "version": "2.x.x", "author": "Acme Corp"}

# No residual Flexprice references
grep -r 'flexprice\|Flexprice\|@flexprice' api/typescript/src/ --include="*.ts"
# Expected: no output
```

### Python SDK — `api/python/`

```bash
# Package name and module name
grep -E '^name|^version|^description' api/python/pyproject.toml
# Expected: name = "acme-sdk", description = "...for the Acme API."

# Import structure
ls api/python/src/
# Expected: acme_sdk/   (underscores, derived from acme-sdk)
```

### MCP Server — `api/mcp/`

```bash
# Package name
jq '{name, version}' api/mcp/package.json
# Expected: {"name": "@acmecorp/mcp-server", "version": "2.x.x"}

# Example configs updated
grep 'mcp-server' api/mcp/examples/claude-desktop-config.example.json
# Expected: "@acmecorp/mcp-server"  (not "@flexprice/mcp-server")
```

### Branding residue check (automated)

`verify-sdk-builds.sh` runs this automatically. To run it manually:

```bash
WL_SDK_CLASS_NAME=Acme bash scripts/verify-sdk-builds.sh
```

It fails if any of these are found in the generated output:
- `package flexprice` (Go package declaration)
- `*Flexprice` (Go type reference)
- `"@flexprice/mcp-server"` (MCP package name)
- `for the FlexPrice API` (doc string)
- `github.com/flexprice/go-sdk` (Go import path)

---

## 8. Triggering a Release

The WL pipeline fires on every **published, non-pre-release, non-draft** GitHub release.

### Steps

1. Make sure `WL_SDK_CONFIG` and `SPEAKEASY_API_KEY` are set in the repo secrets
2. Merge your changes to `main`
3. Go to **Releases → Draft a new release**
4. Tag: `v2.x.x` (must be a 2.x.x version)
5. Set title, write release notes
6. Click **Publish release** (not "Save draft" or "Pre-release")

Both `generate-sdks.yml` (Flexprice SDKs) and `generate-wl-sdks.yml` (WL SDKs) fire simultaneously from the same release event.

### Monitoring

Go to **Actions → Generate White-Label SDKs** to see:
- `matrix-setup` — parses `WL_SDK_CONFIG`, builds client index list
- `generate-wl (index: 0)`, `generate-wl (index: 1)` — one job per client, all parallel
- `wl-summary` — overall result

Each job logs which repos it pushed to and whether npm/PyPI publish succeeded.

### If a client job fails

The job log will show exactly which step failed. Common causes:

| Failure step | Likely cause | Fix |
|---|---|---|
| Validate white-label config | Missing or malformed field | Check the field reference in §4 |
| Verify client repo accessibility | Token lacks write access, or repo doesn't exist | Re-check PAT permissions |
| Generate SDKs | Speakeasy error | Check Speakeasy dashboard for the API key's quota |
| Push Go/Python/TypeScript/MCP | Token expired or revoked | Rotate `WL_SDK_DEPLOY_GIT_TOKEN` in `WL_SDK_CONFIG` |
| Publish to npm | Token invalid or package name taken | Re-generate npm token; check package name availability |
| Publish to PyPI | Token invalid or version already published | Re-generate PyPI token; use a new version |

---

## 9. What the Client Gets

Once the pipeline runs, deliver the following to the client:

### 1. SDK documentation

Point them to their GitHub repos. Each repo has a `README.md` with:
- Installation instructions
- Quickstart code snippet
- Link to full API reference

### 2. Quick-start guide (per language)

**Go**
```bash
go get github.com/acmecorp/go-sdk/v2
```
```go
package main

import (
    acme "github.com/acmecorp/go-sdk/v2"
    "context"
)

func main() {
    client := acme.NewAcme(acme.WithSecurity("sk_live_xxx"))
    customers, err := client.Customers.List(context.Background(), nil)
    _ = customers
    _ = err
}
```

**TypeScript**
```bash
npm install @acmecorp/sdk
```
```typescript
import { Acme } from "@acmecorp/sdk";

const client = new Acme({ apiKey: "sk_live_xxx" });

const customers = await client.customers.list({});
```

**Python**
```bash
pip install acme-sdk
```
```python
from acme_sdk import Acme

client = Acme(api_key="sk_live_xxx")
customers = client.customers.list()
```

**MCP (AI assistant integration)**
```bash
# Add to Claude Desktop config:
# ~/Library/Application Support/Claude/claude_desktop_config.json
```
```json
{
  "mcpServers": {
    "acmecorp": {
      "command": "npx",
      "args": ["-y", "@acmecorp/mcp-server"],
      "env": { "ACME_API_KEY": "sk_live_xxx" }
    }
  }
}
```

### 3. SDK versioning

SDK versions match the Flexprice release tag (`v2.1.0` release → SDK version `2.1.0`). The client can rely on semver — patch releases are backward-compatible, minor releases may add new endpoints.

### 4. Updates

Every subsequent Flexprice release automatically pushes updated SDKs to the client's repos and publishes new versions to npm/PyPI. No manual action needed from the client.
