# Custom SDK and MCP files

Files under `api/custom/<lang>/` are **merged** into the generated output after each `speakeasy run`. Paths must **mirror** `api/<lang>/`.

| Directory | Contents |
|-----------|----------|
| `go/` | README.md, async.go, helpers.go, examples/ |
| `typescript/` | README.md, src/sdk/customer-portal.ts |
| `python/` | README.md, examples/, MANIFEST.in |
| `mcp/` | README.md (auth, client configs, dynamic mode, scopes, troubleshooting) |

**Apply custom:** Run `make merge-custom` (or `make sdk-all`). Do not edit generated files under `api/<lang>/` for custom logic—edit here so changes survive regeneration.

**Add new custom code:** Create the same path under `api/custom/<lang>/` as in `api/<lang>/`; merge-custom will copy it over.
