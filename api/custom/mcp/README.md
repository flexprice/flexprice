# flexprice MCP Server

A Model Context Protocol (MCP) server that exposes the flexprice API as tools for AI assistants (e.g. Claude, Cursor, VS Code, Windsurf). Use it to manage customers, plans, prices, subscriptions, invoices, payments, events, and more from your IDE or CLI.

## Table of contents

- [Prerequisites](#prerequisites)
- [How to use the flexprice MCP server](#how-to-use-the-flexprice-mcp-server)
- [Add to your MCP client](#add-to-your-mcp-client)
- [Tools](#tools)
- [Progressive discovery (dynamic mode)](#progressive-discovery-dynamic-mode)
- [Scopes](#scopes)
- [Troubleshooting](#troubleshooting)
- [Generating the MCP server](#generating-the-mcp-server)

## Prerequisites

- **Node.js** v20 or higher
- **npm** or **yarn**
- **flexprice API key** from your [flexprice account](https://app.flexprice.io)

## How to use the flexprice MCP server

You can run the server in two ways: **npm package** (one command) or **local repo** (clone and run). Pick one, then [add it to your MCP client](#add-to-your-mcp-client).

---

### Option 1: npm package

Install: `npm i @flexprice/mcp-server`. Or run with one command (no clone or build):

```bash
npx @flexprice/mcp-server start --server-url https://us.api.flexprice.io/v1 --api-key-auth YOUR_API_KEY
```

Replace `YOUR_API_KEY` with your flexprice API key. Next: [Add to your MCP client](#add-to-your-mcp-client).

---

### Option 2: Local repo

Use this if you want to change code or run without npm:

1. Clone the repository and go to the MCP server directory (e.g. `api/mcp` or the repo that contains it).
2. Install dependencies: `npm install`
3. Create a `.env` file (from `.env.example` if present) with:
   - `BASE_URL=https://us.api.flexprice.io/v1` (must include `/v1`; no trailing space or trailing slash)
   - `API_KEY_APIKEYAUTH=your_api_key_here`
4. Build: `npm run build`
5. Start: `npm start`

**Docker (stdio):** You can also build and run with stdio:

```bash
docker build -t flexprice-mcp .
docker run -i -e API_KEY_APIKEYAUTH=your_api_key_here -e BASE_URL=https://us.api.flexprice.io/v1 flexprice-mcp node bin/mcp-server.js start
```

Next: [Add to your MCP client](#add-to-your-mcp-client) and use the **Node from repo** or **Docker** config below.

## Add to your MCP client

Add the flexprice MCP server in your editor. Replace `YOUR_API_KEY` with your flexprice API key in all examples. Example config snippets are in [examples/](examples/).

**After connecting:** In Cursor, open the MCP panel and confirm the server is connected. You can list tools and try an operation (e.g. list customers) from your assistant. In Claude, use `/mcp` to see connected servers and available tools.

### Config file locations

| Host                       | Config location |
| -------------------------- | --------------- |
| **Cursor**                 | Cursor → Settings → MCP (or Cmd+Shift+P → "Cursor Settings" → MCP) |
| **VS Code**                | Command Palette → **MCP: Open User Configuration** (opens `mcp.json`) |
| **Claude Desktop (macOS)** | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| **Claude Desktop (Windows)** | `%APPDATA%\Claude\claude_desktop_config.json` |

---

### Cursor

1. Open **Cursor → Settings → Cursor Settings** and go to the **MCP** tab.
2. Add a new MCP server and use this config (Option 1 — npx):

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "npx",
      "args": [
        "-y",
        "@flexprice/mcp-server",
        "start",
        "--server-url",
        "https://us.api.flexprice.io/v1",
        "--api-key-auth",
        "YOUR_API_KEY"
      ]
    }
  }
}
```

---

### VS Code

1. Open Command Palette (**Ctrl+Shift+P** / **Cmd+Shift+P**) and run **MCP: Open User Configuration** or **MCP: Add Server**.
2. Add:

```json
{
  "servers": {
    "flexprice": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "@flexprice/mcp-server",
        "start",
        "--server-url",
        "https://us.api.flexprice.io/v1",
        "--api-key-auth",
        "YOUR_API_KEY"
      ]
    }
  }
}
```

---

### Claude Code

```bash
claude mcp add flexprice -- npx -y @flexprice/mcp-server start --server-url https://us.api.flexprice.io/v1 --api-key-auth YOUR_API_KEY
```

Then run `claude` and use `/mcp` to confirm the server is connected.

---

### Claude Desktop

Add to your Claude Desktop config file (path in the table above):

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "npx",
      "args": [
        "-y",
        "@flexprice/mcp-server",
        "start",
        "--server-url",
        "https://us.api.flexprice.io/v1",
        "--api-key-auth",
        "YOUR_API_KEY"
      ]
    }
  }
}
```

Quit and reopen Claude Desktop.

---

### Alternative configs

**Node from repo** (Option 2 — run from cloned repo):

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "node",
      "args": ["/path/to/mcp-server/bin/mcp-server.js", "start"],
      "env": {
        "API_KEY_APIKEYAUTH": "your_api_key_here",
        "BASE_URL": "https://us.api.flexprice.io/v1"
      }
    }
  }
}
```

**Docker** (Option 2 — stdio):

```json
{
  "mcpServers": {
    "flexprice": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "API_KEY_APIKEYAUTH", "-e", "BASE_URL", "flexprice-mcp"],
      "env": {
        "API_KEY_APIKEYAUTH": "your_api_key_here",
        "BASE_URL": "https://us.api.flexprice.io/v1"
      }
    }
  }
}
```

After editing, save and **restart Cursor or quit and reopen Claude Desktop** so the MCP server is loaded.

## Tools

The server exposes flexprice API operations as MCP tools. **Only operations with certain OpenAPI tags are included** (e.g. Customers, Invoices, Events). The allowed tags are configured in the repo; the filtered spec is `docs/swagger/swagger-3-0-mcp.json`. Tool names and parameters follow the OpenAPI spec. For the full list, see your MCP client’s tool list after connecting, or the OpenAPI spec (e.g. `docs/swagger/swagger-3-0.json`) in the repo.

## Progressive discovery (dynamic mode)

Servers with many tools can bloat context and token usage. **Dynamic mode** exposes a small set of meta-tools so the assistant can discover and call operations on demand:

- **`list_tools`** – List available tools with names and descriptions  
- **`describe_tool`** – Get the input schema for one or more tools  
- **`execute_tool`** – Run a tool by name with given parameters  

To enable dynamic mode, add `--mode dynamic` when starting the server:

```json
"args": ["-y", "@flexprice/mcp-server", "start", "--server-url", "https://us.api.flexprice.io/v1", "--api-key-auth", "YOUR_API_KEY", "--mode", "dynamic"]
```

This reduces tokens per request and can improve tool choice when there are many operations.

## Scopes

If the server is configured with scopes (e.g. `read`, `write`), you can limit tools by scope:

```bash
npx @flexprice/mcp-server start --server-url https://us.api.flexprice.io/v1 --api-key-auth YOUR_API_KEY --scope read
```

Use `read` for read-only access when the server defines a `read` scope.

## Troubleshooting

### "Invalid URL" or request errors

- The server builds request URLs from `BASE_URL` + path. If `BASE_URL` is unset or wrong, requests fail.
- **Fix:** Set `BASE_URL=https://us.api.flexprice.io/v1` (no trailing space or slash after `v1`). For npx, pass `--server-url https://us.api.flexprice.io/v1`.
- If you get **404** on tool calls, ensure the base URL includes `/v1`.

### API connection issues

1. **Credentials:** Check that your API key and base URL are correct. Test the key with the flexprice API (e.g. `curl -H "x-api-key: your_key" https://us.api.flexprice.io/v1/customers`).
2. **Network:** Confirm the host can reach the flexprice API (firewall, proxy).
3. **Rate limiting:** If you see rate-limit errors, reduce request frequency or contact flexprice support.

### Server issues

- **Port in use:** If something else uses the port (e.g. 3000), change the server config or stop the other process.
- **Missing dependencies:** Run `npm install` and `npm run build` in the server directory.
- **Permissions:** Ensure the entrypoint is executable (e.g. `chmod +x bin/mcp-server.js`).

### Docker

- **Build failures:** Check Docker is installed and the daemon is running; try `docker build --no-cache`.
- **Container exits:** Inspect logs with `docker logs <container_id>`.
- **Env vars:** Verify env is passed: `docker run -it --rm flexprice-mcp printenv`.

## Generating the MCP server

The server is generated from a tag-filtered OpenAPI spec (`docs/swagger/swagger-3-0-mcp.json`), not the full spec. Only operations whose tags are listed in the allowed-tags configuration are included. To regenerate after API or overlay changes:

1. (Optional) Edit the allowed-tags configuration to add or remove tags; then run `make filter-mcp-spec` to rebuild the filtered spec.
2. From the repo root, run `make sdk-all` (this runs `filter-mcp-spec` automatically, then generates the MCP server).
3. Run `make merge-custom` so custom files (including this README) are merged into the output.
4. Build and run: `npm run build` and `npm start` from the MCP output directory.

See the main repo README and AGENTS.md for SDK/MCP generation and publishing.
