# FlexPrice SDK Tests (Published)

Integration tests for the **published** FlexPrice SDKs. See [SDK PR #1288](https://github.com/flexprice/flexprice/pull/1288).

Install the SDK from the registry, set credentials, and run the test for your language.

## Packages and repos (canonical)

| Language   | Install | Repo |
| ---------- | ------- | ----- |
| **Go**     | [go-sdk](https://github.com/flexprice/go-sdk) (GitHub) | [go-sdk](https://github.com/flexprice/go-sdk) |
| **TypeScript** | `npm i @flexprice/sdk` | [javascript-sdk](https://github.com/flexprice/javascript-sdk) |
| **MCP**    | `npm i @flexprice/mcp-server` | [mcp-server](https://github.com/flexprice/mcp-server) |
| **Python** | `pip install flexprice` | [python-sdk](https://github.com/flexprice/python-sdk) |

---

## Environment (required)

You must **export** base URL and API key so the tests can call the API. Set these before running any test (or `make test-sdk`):

| Variable             | Required | Description                                                                                                                                 |
| -------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `FLEXPRICE_API_KEY`  | **Yes**  | Your FlexPrice API key.                                                                                                                     |
| `FLEXPRICE_API_HOST` | **Yes**  | API host and version path (no `https://`). Must include `/v1` (e.g. `us.api.flexprice.io/v1` or `localhost:8080/v1`). No trailing space or slash. |

```bash
export FLEXPRICE_API_KEY="your-api-key"
export FLEXPRICE_API_HOST="us.api.flexprice.io/v1"
# For local server:
# export FLEXPRICE_API_HOST="localhost:8080/v1"
```

If you run `make test-sdk` without these set, the Makefile will exit with instructions to set them.

---

## Run tests

### Go

```bash
cd api/tests/go
go mod tidy
go run -tags published test_sdk.go
```

### Python

```bash
cd api/tests/python
.venv/bin/pip install -e ../../python
.venv/bin/python test_sdk.py
```

**Published SDK (pip, pinned to flexprice 2.0.1):**

```bash
cd api/tests/python
.venv/bin/pip install -r requirements.txt
.venv/bin/python test_sdk.py
```

### TypeScript

```bash
cd api/tests/ts
npm install
npm test
# or: npx ts-node test_sdk_js.ts
```

---

## Makefile (from repo root)

Run all SDK tests (Go, Python, TypeScript) in one command. Dependencies are installed automatically before each language’s tests:

```bash
make test-sdk
# or
make test-sdks
```

- **Go:** `go mod tidy` + `go mod download` then run tests (SDK is fetched from [go-sdk](https://github.com/flexprice/go-sdk) via a `replace` in `go.mod`).  
- **Python:** A `.venv` is created in `api/tests/python` and used so system Python is not modified (avoids “externally-managed-environment” on macOS/Homebrew).  
- **TypeScript:** `npm install` then run tests

---

## Test coverage

All variants run the same API flow: Customers, Features, Plans, Addons, Entitlements, Subscriptions, Invoices, Prices, Payments, Wallets, Credit Grants, Credit Notes, Integrations (connections), Events, plus cleanup.
