# FlexPrice SDK Tests (Local vs Published)

Tests are organized by language:

- **`api/tests/go/`** – Go tests (local: `test_local_sdk.go`; published: `test_sdk.go`), plus `go.mod`/`go.sum` for local SDK.
- **`api/tests/python/`** – Python tests (`test_local_sdk.py`, `test_sdk.py`).
- **`api/tests/ts/`** – TypeScript/JavaScript tests (`test_local_sdk_js.ts`, `test_sdk_js.ts`) and `package.json`.

Two variants of integration tests for the FlexPrice SDKs:

- **Local** (`test_local_sdk_*`) – Exercises the SDK built from this repo (`api/go`, `api/python`, `api/javascript` or `api/typescript`). Use after `make sdk-all` (or equivalent) to verify the generated SDK before publishing.
- **Published** (`test_sdk_*`) – Exercises the published packages (Go module proxy, PyPI, npm). Use to verify the currently released SDK.

Both variants run the same API test flow (Customers, Features, Plans, Addons, Entitlements, Subscriptions, Invoices, Prices, Payments, Wallets, Credit Grants, Credit Notes, Connections, Events, plus cleanup).

---

## Environment (required for all tests)

Set these **before** running any variant:

| Variable | Required | Description |
|----------|----------|-------------|
| `FLEXPRICE_API_KEY` | **Yes** | Your FlexPrice API key. If missing, tests exit with an error. |
| `FLEXPRICE_API_HOST` | **Yes** (for most tests) | API host only (no `https://`). Use `us.api.flexprice.io` for cloud; or `localhost:8080` for a local server. Scripts typically prepend `https://`; for local HTTP you may need to pass a full URL in code. |

**Step 1 – Set environment (examples):**

```bash
# Cloud / staging
export FLEXPRICE_API_KEY="your-api-key"
export FLEXPRICE_API_HOST="us.api.flexprice.io"

# Local API (e.g. go run cmd/server/main.go)
export FLEXPRICE_API_KEY="your-dev-key"
export FLEXPRICE_API_HOST="localhost:8080"
```

**Step 2 – Run tests** (see sections below for local vs published and per-language steps).

---

## Local SDK tests (step-by-step)

Local tests use the SDKs generated in this repo (`api/go`, `api/python`, `api/typescript`). Use them to verify the SDK **before** publishing.

### Step 1: Build the SDKs

From the **repository root**:

```bash
make sdk-all
```

This validates the OpenAPI spec, generates all SDKs (and MCP), and runs `merge-custom`. Ensure this completes without errors.

### Step 2: Set environment

```bash
export FLEXPRICE_API_KEY="your-api-key"
export FLEXPRICE_API_HOST="us.api.flexprice.io"   # or your host / localhost:8080
```

### Step 3: Run the local test for your language

Tests are organized by language under `api/tests/go/`, `api/tests/python/`, and `api/tests/ts/`.

| Language   | Steps | Command |
|-----------|--------|---------|
| **Go**    | 1. `cd api/tests/go`<br>2. Ensure env is set, then run | `go run test_local_sdk.go` |
| **Python**| 1. `cd api/tests/python`<br>2. Ensure env is set, then run | `python test_local_sdk.py` |
| **TypeScript** | 1. `cd api/tests/ts`<br>2. Ensure env is set, then run | `npx ts-node test_local_sdk_js.ts` |

**Go in one line (from repo root):**

```bash
cd api/tests/go && go run test_local_sdk.go
```

**Go with inline env (run from `api/tests/go` so the local SDK is used):**

```bash
cd api/tests/go
FLEXPRICE_API_KEY=your-key FLEXPRICE_API_HOST=us.api.flexprice.io go run test_local_sdk.go
```

**Notes:**

- **Go**: Uses `api/tests/go/go.mod` with `replace github.com/flexprice/flexprice-go => ../../go` so the test uses the local SDK from `api/go`.
- **Python**: Adds the local SDK path (`api/python`) to `sys.path` and imports the local package.
- **TypeScript**: Imports from the local build (`api/javascript/dist`).

---

## Published SDK tests (step-by-step)

Published tests use the SDKs installed from the registry (Go module proxy, PyPI, npm). Use them to verify the **released** SDK.

### Step 1: Install the published SDK

| Language   | Install command |
|-----------|------------------|
| **Go**    | No extra install; use repo root `go.mod` which depends on `github.com/flexprice/go-sdk` (or the published module name). Run from repo root. |
| **Python**| `pip install flexprice-sdk-test` (or the published package name). |
| **TypeScript** | `cd api/tests/ts && npm install` or `npm install flexprice-sdk-test`. |

### Step 2: Set environment

Same as local:

```bash
export FLEXPRICE_API_KEY="your-api-key"
export FLEXPRICE_API_HOST="us.api.flexprice.io"
```

### Step 3: Run the published test

| Language   | Run from | Command |
|-----------|----------|---------|
| **Go**    | **Repository root** | `go run -tags published ./api/tests/go/test_sdk.go` |
| **Python**| `api/tests/python` | `python test_sdk.py` |
| **TypeScript** | `api/tests/ts` | `npx ts-node test_sdk_js.ts` |

**Important:** Go published tests must be run from the **repository root** with `-tags published` so the root `go.mod` (and its dependency on the published Go SDK) is used. In `api/tests/go`, the local test is built by default; the published test file is built only with `-tags published` to avoid two `main` packages in one directory.

---

## Makefile targets (from repo root)

Convenience targets that run **all** local or **all** published tests:

| Target | What it runs |
|--------|----------------|
| `make test-sdk-local` | All local SDK tests (Go, Python, TypeScript). |
| `make test-sdk-published` | All published SDK tests. |

**Prerequisites:** For local, run `make sdk-all` first. For published, ensure the published packages are installed for each language.
