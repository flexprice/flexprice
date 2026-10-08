# Custom SDK and MCP files

Files under `api/custom/<lang>/` are **merged** into the generated output after each SDK generation run. Paths must **mirror** `api/<lang>/`.

| Directory | Contents |
|-----------|----------|
| `go/` | README.md, async.go, helpers.go, examples/ |
| `typescript/` | README.md, src/sdk/customer-portal.ts |
| `python/` | README.md, examples/, MANIFEST.in |
| `mcp/` | README.md (auth, client configs, dynamic mode, scopes, troubleshooting), src/mcp-server/browser-login.ts (browser login for `serve`), tests/ |

**Apply custom:** Run `make merge-custom` (or `make sdk-all`). Do not edit generated files under `api/<lang>/` for custom logic—edit here so changes survive regeneration.

**Add new custom code:** Create the same path under `api/custom/<lang>/` as in `api/<lang>/`; merge-custom will copy it over.

**MCP browser login:** `browser-login.ts` is wired into the generated `serve` command by `scripts/patch-mcp-serve.mjs`, which merge-custom runs and which fails the build if the generated lines it edits change. It is off unless `MCP_OAUTH_ISSUER` (the login server, for example `https://<ref>.supabase.co/auth/v1`) and `MCP_PUBLIC_URL` (the URL clients connect to, for example `https://mcp.example.com/mcp`) are both set.

**Custom tests:** A `tests/` folder under `api/custom/<lang>/` is not merged into the output. `make mcp-check` builds the merged MCP server in a temp dir and runs `api/custom/mcp/tests/` against it (`MCP_SRC=<path>` points it at a generated server other than `api/mcp`).

**Verified integration tests:** The full API flow (customers, features, plans, addons, entitlements, subscriptions, invoices, prices, payments, wallets, credit grants, credit notes, events, cleanup) is covered by the integration tests in **api/tests/** (Go: `test_sdk.go`, Python: `test_sdk.py`, TypeScript: `test_sdk.ts`). See [api/tests/README.md](../tests/README.md) for run instructions and test access structure.
