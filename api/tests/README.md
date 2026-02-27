# FlexPrice SDK Tests (Published)

Integration tests for the **published** FlexPrice SDKs. See [SDK PR #1288](https://github.com/flexprice/flexprice/pull/1288).

Install the SDK from the registry, set credentials, and run the test for your language.

## Packages and repos (canonical)

| Language   | Package / registry | Repo |
| ---------- | ------------------ | ----- |
| **Go**     | [go-sdk-temp](https://github.com/flexprice/go-sdk-temp) (GitHub) | [go-sdk-temp](https://github.com/flexprice/go-sdk-temp) |
| **TypeScript** | [flexprice-ts-temp](https://www.npmjs.com/package/flexprice-ts-temp) (npm) | [js-sdk-temp](https://github.com/flexprice/js-sdk-temp) |
| **MCP**    | [@omkar273/mcp-temp](https://www.npmjs.com/package/@omkar273/mcp-temp) (npm) | [mcp-temp](https://github.com/flexprice/mcp-temp) |
| **Python** | [flexprice-temp](https://pypi.org/project/flexprice-temp/) (PyPI) | [python-sdk-temp](https://github.com/flexprice/python-sdk-temp) |

---

## Environment (required)

You must **export** base URL and API key so the tests can call the API. Set these before running any test (or `make test-sdk`):

| Variable             | Required | Description                                                                                                 |
| -------------------- | -------- | ----------------------------------------------------------------------------------------------------------- |
| `FLEXPRICE_API_KEY`  | **Yes**  | Your FlexPrice API key.                                                                                     |
| `FLEXPRICE_API_HOST` | **Yes**  | API host only (no `https://`). Use `us.api.flexprice.io` for cloud, or `localhost:8080` for a local server. |

```bash
export FLEXPRICE_API_KEY="your-api-key"
export FLEXPRICE_API_HOST="us.api.flexprice.io"
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
python3 -m pip install flexprice-temp
cd api/tests/python
python3 test_sdk.py
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

- **Go:** `go mod tidy` + `go mod download` then run tests (SDK is fetched from [go-sdk-temp](https://github.com/flexprice/go-sdk-temp) via a `replace` in `go.mod`).  
- **Python:** A `.venv` is created in `api/tests/python` and used so system Python is not modified (avoids “externally-managed-environment” on macOS/Homebrew).  
- **TypeScript:** `npm install` then run tests

---

## Test coverage

All variants run the same API flow: Customers, Features, Plans, Addons, Entitlements, Subscriptions, Invoices, Prices, Payments, Wallets, Credit Grants, Credit Notes, Integrations (connections), Events, plus cleanup.
