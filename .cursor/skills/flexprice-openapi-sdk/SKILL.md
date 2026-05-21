---
name: flexprice-openapi-sdk
description: >-
  Regenerates Swagger/OpenAPI and downstream SDKs (Go, TS, Python, MCP) for FlexPrice.
  Use when handlers or Swagger annotations change, before publishing SDKs, or when the user
  says swagger, OpenAPI, speakeasy, sdk-all, or MCP tools.
---

# FlexPrice OpenAPI & SDK generation

## Source of truth

- Handlers: Swagger comments in `internal/api/**`.
- Generated specs: `docs/swagger/` (run generation — do not hand-edit JSON as primary source).

## Standard sequence

After route or Swagger annotation changes:

```bash
make swagger
make sdk-all
```

`make sdk-all` (per **`AGENTS.md`**) validates, generates SDKs/MCP, merges **`api/custom/**`**.

**OpenAPI-only validation:**

```bash
make speakeasy-validate
```

## Custom vs generated

- **Do not** put long-lived custom logic only in generated trees under **`api/go`**, **`api/typescript`**, **`api/python`**, **`api/mcp`**.
- Put custom code in **`api/custom/<lang>/`** and rely on **`make merge-custom`** (often part of `sdk-all`).

## MCP tooling

Filtered spec and allowed tags live under **`.speakeasy/mcp/`** — changing exposed tools requires config update + regenerate flow documented in **`AGENTS.md`**.

## When to skip full `sdk-all`

Small internal-only HTTP tweak with no exported contract change: **`make swagger`** alone may suffice; coordinate with reviewers if public SDK repos are consumers.

## Related

- **Tests after API change**: expand handler tests or **`api/tests`** if behavior is user-facing SDK contract.
