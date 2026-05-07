# FlexPrice SDK integration tests

Integration tests for the FlexPrice SDKs shipped from this repo (`api/go`, `api/typescript`, `api/python`). See [SDK PR #1288](https://github.com/flexprice/flexprice/pull/1288).

**Default workflow (pre-release):** point tests at the **local** generated SDKs so you validate the tree before publish.

## What “async” means per language

| Language | Behavior exercised |
| -------- | -------------------- |
| **Go** | Custom `AsyncClient` on `*flexprice.Flexprice` (`NewAsyncClientWithConfig`, `Enqueue`, `Flush`, `Close`) in addition to sync `Events.IngestEvent`. |
| **TypeScript** | Promise-based SDK calls; bulk path uses `client.events.ingestEventsBulk`. There is no Go-style queued client in TS. |
| **Python** | Speakeasy `ingest_event_async`, `ingest_events_bulk_async`, and a smoke `list_raw_events_async` via `asyncio` + `async with Flexprice(...)`. |

## Test entrypoints

| Language | Entry | Local SDK |
| -------- | ----- | --------- |
| **Go** | [`api/tests/go/test_sdk.go`](go/test_sdk.go) | `go.mod` `replace github.com/flexprice/go-sdk/v2 => ../../go` |
| **Python** | [`api/tests/python/test_sdk.py`](python/test_sdk.py) | [`requirements.txt`](python/requirements.txt) uses `-e ../../python` |
| **TypeScript** | [`api/tests/ts/test_sdk.ts`](ts/test_sdk.ts) | [`package.json`](ts/package.json) uses `"@flexprice/sdk": "file:../../typescript"` (run `npm run build` in `api/typescript` if `esm/` is missing) |

**Published SDKs (optional):** Python — [`requirements-published.txt`](python/requirements-published.txt). TypeScript — `npm run test:published` in `api/tests/ts`.

---

## Environment (required)

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `FLEXPRICE_API_KEY` | Yes | API key |
| `FLEXPRICE_API_HOST` | Yes | Host **and** `/v1` path, **no** `https://` (e.g. `us.api.flexprice.io/v1` or `localhost:8080/v1`). No trailing slash. |

```bash
export FLEXPRICE_API_KEY="your-api-key"
export FLEXPRICE_API_HOST="us.api.flexprice.io/v1"
```

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
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/python test_sdk.py
```

### TypeScript

```bash
cd api/typescript && npm install && npm run build   # if esm/ output is missing
cd api/tests/ts
npm install
npm test
```

---

## Makefile (repo root)

```bash
make test-sdk
```

Installs dependencies per language (`go mod tidy`, `pip install -r requirements.txt` with editable local Python SDK, `npm install` with local `file:` TS SDK) then runs each test driver.

---

## Coverage

Same API flow across languages: Customers, Features, Plans, Addons, Entitlements, Subscriptions, Invoices, Prices, Payments, Wallets, Credit Grants, Credit Notes, **Integrations** (skipped where the generated SDK has no list-linked API), **Events** (sync + language-specific async/bulk), then cleanup. Events are stored in ClickHouse (`migrations/clickhouse/000006_create_raw_events.sql`).
