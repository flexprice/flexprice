# Custom SDK and MCP files

Files under `api/custom/<lang>/` are **merged** into the generated output after each SDK generation run. Paths must **mirror** `api/<lang>/`.

| Directory | Contents |
|-----------|----------|
| `go/` | README.md, async.go, helpers.go, examples/ |
| `typescript/` | README.md, src/sdk/customer-portal.ts |
| `python/` | README.md, examples/, MANIFEST.in |
| `mcp/` | README.md (auth, client configs, dynamic mode, scopes, troubleshooting) |

**Apply custom:** Run `make merge-custom` (or `make sdk-all`). Do not edit generated files under `api/<lang>/` for custom logic—edit here so changes survive regeneration.

**Add new custom code:** Create the same path under `api/custom/<lang>/` as in `api/<lang>/`; merge-custom will copy it over.

**Verified integration tests:** The full API flow (customers, features, plans, addons, entitlements, subscriptions, invoices, prices, payments, wallets, credit grants, credit notes, events, cleanup) is covered by the integration tests in **api/tests/** (Go: `test_sdk.go`, Python: `test_sdk.py`, TypeScript: `test_sdk.ts`). See [api/tests/README.md](../tests/README.md) for run instructions and test access structure.
