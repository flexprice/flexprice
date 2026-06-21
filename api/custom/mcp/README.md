# Flexprice MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io) server that connects AI assistants (Claude, Cursor, VS Code, Windsurf) directly to your Flexprice account. Manage customers, subscriptions, invoices, usage events, plans, and pricing — all from your IDE or CLI.

## Quick start

Two lines to get running. Replace `YOUR_API_KEY` with your key from [app.flexprice.io](https://app.flexprice.io):

**Claude Desktop / Cursor / VS Code** — add to your MCP config:
```json
{
  "mcpServers": {
    "flexprice": {
      "command": "npx",
      "args": [
        "-y", "@flexprice/mcp-server", "start",
        "--server-url", "https://us.api.flexprice.io/v1",
        "--api-key-auth", "YOUR_API_KEY",
        "--scope", "read", "--scope", "write"
      ]
    }
  }
}
```

**Claude Code** — one command:
```bash
claude mcp add flexprice -- npx -y @flexprice/mcp-server start \
  --server-url https://us.api.flexprice.io/v1 \
  --api-key-auth YOUR_API_KEY \
  --scope read --scope write
```

> **Default scope is `read + write`.** This gives full create/update access while keeping destructive operations (delete, void, finalize) opt-in. Add `--scope delete` when you need them.

---

## Table of contents

- [Prerequisites](#prerequisites)
- [Config file locations](#config-file-locations)
- [Client setup](#client-setup)
  - [Cursor](#cursor)
  - [VS Code](#vs-code)
  - [Claude Desktop](#claude-desktop)
  - [Claude Code](#claude-code)
  - [Windsurf](#windsurf)
- [Scopes — controlling access](#scopes--controlling-access)
- [Tools](#tools)
- [Dynamic mode — fewer tokens](#dynamic-mode--fewer-tokens)
- [Available regions](#available-regions)
- [Running locally (without npx)](#running-locally-without-npx)
- [Generating the MCP server](#generating-the-mcp-server)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

- Node.js v20 or higher
- A Flexprice API key from [app.flexprice.io](https://app.flexprice.io)

---

## Config file locations

| Client | Config location |
|--------|----------------|
| **Cursor** | Settings → MCP, or `~/.cursor/mcp.json` |
| **VS Code** | Command Palette → `MCP: Open User Configuration` |
| **Claude Desktop (macOS)** | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| **Claude Desktop (Windows)** | `%APPDATA%\Claude\claude_desktop_config.json` |
| **Claude Code** | `claude mcp add` command (see below) |
| **Windsurf** | `~/.codeium/windsurf/mcp_config.json` |

---

## Client setup

All examples use `read + write` scope by default. Adjust `--scope` flags as needed — see [Scopes](#scopes--controlling-access).

### Cursor

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "npx",
      "args": [
        "-y", "@flexprice/mcp-server", "start",
        "--server-url", "https://us.api.flexprice.io/v1",
        "--api-key-auth", "YOUR_API_KEY",
        "--scope", "read", "--scope", "write"
      ]
    }
  }
}
```

### VS Code

```json
{
  "servers": {
    "flexprice": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y", "@flexprice/mcp-server", "start",
        "--server-url", "https://us.api.flexprice.io/v1",
        "--api-key-auth", "YOUR_API_KEY",
        "--scope", "read", "--scope", "write"
      ]
    }
  }
}
```

### Claude Desktop

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "npx",
      "args": [
        "-y", "@flexprice/mcp-server", "start",
        "--server-url", "https://us.api.flexprice.io/v1",
        "--api-key-auth", "YOUR_API_KEY",
        "--scope", "read", "--scope", "write"
      ]
    }
  }
}
```

Quit and reopen Claude Desktop after saving.

### Claude Code

```bash
claude mcp add flexprice -- npx -y @flexprice/mcp-server start \
  --server-url https://us.api.flexprice.io/v1 \
  --api-key-auth YOUR_API_KEY \
  --scope read --scope write
```

Verify with `/mcp` inside a Claude Code session.

### Windsurf

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "npx",
      "args": [
        "-y", "@flexprice/mcp-server", "start",
        "--server-url", "https://us.api.flexprice.io/v1",
        "--api-key-auth", "YOUR_API_KEY",
        "--scope", "read", "--scope", "write"
      ]
    }
  }
}
```

---

## Scopes — controlling access

Scopes let you limit what the AI can do. Pass one or more `--scope` flags at startup.

| Scope | What it covers | When to use |
|-------|---------------|-------------|
| `read` | All GET operations — list, get, query, search | Safe exploration, reporting, dashboards |
| `write` | Create and update operations (POST, PUT, PATCH) | Automation, onboarding workflows |
| `delete` | Destructive operations — delete, void, finalize | Admin tasks only |

### Recipes

**Read-only** — safest, for exploration and analytics:
```json
"args": ["...", "--scope", "read"]
```

**Read + write** — recommended for most use cases:
```json
"args": ["...", "--scope", "read", "--scope", "write"]
```

**Full access** — includes destructive operations, use with care:
```json
"args": ["...", "--scope", "read", "--scope", "write", "--scope", "delete"]
```

**All tools** — equivalent to full access (omitting `--scope` entirely exposes everything):
```json
"args": ["...", "--server-url", "...", "--api-key-auth", "..."]
```

> **Tip:** Run separate MCP server entries for different roles. A `flexprice-readonly` for reporting and `flexprice-admin` for operations is a common pattern.

---

## Tools

The MCP server currently exposes **70 tools** across 6 resource types:

| Resource | Tools | Examples |
|----------|-------|---------|
| **Customers** | 9 | create, get, update, delete, usage summary, entitlements |
| **Subscriptions** | 25 | create, cancel, activate, modify, schedule, line items, addons |
| **Invoices** | 14 | create, get, finalize, void, recalculate, preview, PDF |
| **Events** | 8 | ingest, bulk ingest, usage analytics, usage by meter |
| **Plans** | 7 | create, get, update, delete, clone, sync prices |
| **Prices** | 7 | create, get, update, delete, bulk create, lookup by key |

To see the live tool list in your client: in Claude use `/mcp`, in Cursor open the MCP panel.

---

## Dynamic mode — fewer tokens

With 70+ tools, every request carries the full tool list in context — increasing token usage and sometimes confusing tool selection. **Dynamic mode** replaces all tools with 3 meta-tools:

| Meta-tool | What it does |
|-----------|-------------|
| `list_tools` | List all available tools with names and descriptions |
| `describe_tool` | Get the input schema for a specific tool |
| `execute_tool` | Run any tool by name with given parameters |

The AI discovers and calls tools on demand instead of loading all schemas upfront. This typically reduces per-request token usage by **60–90%** for large tool sets.

**Enable dynamic mode** by adding `--mode dynamic`:

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "npx",
      "args": [
        "-y", "@flexprice/mcp-server", "start",
        "--server-url", "https://us.api.flexprice.io/v1",
        "--api-key-auth", "YOUR_API_KEY",
        "--scope", "read", "--scope", "write",
        "--mode", "dynamic"
      ]
    }
  }
}
```

> **Recommendation:** Use dynamic mode when working with the full tool set. Use standard mode when you've narrowed scope to a small subset of tools (e.g. `--scope read` only).

---

## Available regions

| Region | Server URL |
|--------|-----------|
| US | `https://us.api.flexprice.io/v1` |
| Cloud | `https://api.cloud.flexprice.io/v1` |

Pass the appropriate URL via `--server-url`. Defaults to US if omitted.

---

## Running locally (without npx)

Use this if you want to modify the server or avoid downloading on each run.

**From cloned repo:**
```json
{
  "mcpServers": {
    "flexprice": {
      "command": "node",
      "args": [
        "/path/to/mcp-server/bin/mcp-server.js", "start",
        "--scope", "read", "--scope", "write"
      ],
      "env": {
        "API_KEY_APIKEYAUTH": "your_api_key_here",
        "BASE_URL": "https://us.api.flexprice.io/v1"
      }
    }
  }
}
```

**Docker:**
```bash
docker build -t flexprice-mcp .
docker run -i \
  -e API_KEY_APIKEYAUTH=your_api_key_here \
  -e BASE_URL=https://us.api.flexprice.io/v1 \
  flexprice-mcp node bin/mcp-server.js start --scope read --scope write
```

Or as a config snippet:
```json
{
  "mcpServers": {
    "flexprice": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "API_KEY_APIKEYAUTH", "-e", "BASE_URL", "flexprice-mcp",
               "node", "bin/mcp-server.js", "start", "--scope", "read", "--scope", "write"],
      "env": {
        "API_KEY_APIKEYAUTH": "your_api_key_here",
        "BASE_URL": "https://us.api.flexprice.io/v1"
      }
    }
  }
}
```

---

## Generating the MCP server

The server is generated from a tag-filtered OpenAPI spec (`docs/swagger/swagger-3-0-mcp.json`). Only tags listed in `.speakeasy/mcp/allowed-tags.yaml` are included.

**To add or remove exposed APIs:**

1. Edit `.speakeasy/mcp/allowed-tags.yaml`
2. Run `make filter-mcp-spec` to rebuild the filtered spec
3. Run `make sdk-all` to regenerate the server (runs filter step automatically)
4. Run `make merge-custom` to merge custom files including this README

**To regenerate after API changes:**
```bash
make sdk-all        # uses existing swagger spec
make update-sdk     # regenerates swagger first, then sdk-all
```

Custom files in `api/custom/mcp/` (including this README and examples) are preserved across regenerations via `make merge-custom`.

---

## Troubleshooting

### Server not connecting

- Confirm Node.js v20+ is installed: `node --version`
- Try running the npx command directly in your terminal to see error output
- In Claude, run `/mcp` to check server status

### "Invalid URL" or 404 errors

- `--server-url` must include `/v1` with no trailing slash: `https://us.api.flexprice.io/v1`
- If using env vars, set `BASE_URL=https://us.api.flexprice.io/v1` (same requirement)

### Authentication errors

Test your key directly:
```bash
curl -H "x-api-key: YOUR_API_KEY" https://us.api.flexprice.io/v1/customers
```

### Tool not available

Check the scope you started with. If a tool is missing, you may need to add `--scope write` or `--scope delete`. List available tools with `/mcp` in Claude or the MCP panel in Cursor.

### Docker issues

- Build failures: ensure Docker daemon is running; try `docker build --no-cache`
- Container exits immediately: check logs with `docker logs <container_id>`
- Env not passed: verify with `docker run -it --rm flexprice-mcp printenv`
