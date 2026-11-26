# Flexprice MCP Server

[![npm](https://img.shields.io/npm/v/@flexprice/mcp-server?label=npm)](https://www.npmjs.com/package/@flexprice/mcp-server)
[![Docs](https://img.shields.io/badge/docs-docs.flexprice.io-informational)](https://docs.flexprice.io/docs/connect/mcp-server)

Connect AI assistants to [Flexprice](https://flexprice.io), the billing and monetization platform for AI-native and SaaS companies. This [Model Context Protocol](https://modelcontextprotocol.io) server turns the Flexprice API into tools, so Claude, Cursor, VS Code and other MCP clients can look up customers, manage plans and subscriptions, read invoices and send usage events for you.

The [Flexprice docs](https://docs.flexprice.io/docs/connect/mcp-server) have the full guide: setup for every client, API keys, working in more than one environment, and troubleshooting.

## Connect to the hosted server

Flexprice hosts the server. Add the URL for the region your account was created in, the one whose dashboard you log in to:

| Region | Dashboard | MCP server URL |
| --- | --- | --- |
| US | [us.flexprice.io](https://us.flexprice.io) | `https://mcp.us.flexprice.io/mcp` |
| India | [admin.flexprice.io](https://admin.flexprice.io) | `https://mcp.in.flexprice.io/mcp` |

In Claude Code:

```bash
# US
claude mcp add --transport http flexprice https://mcp.us.flexprice.io/mcp

# India
claude mcp add --transport http flexprice https://mcp.in.flexprice.io/mcp
```

Then run `claude`, type `/mcp`, select **flexprice** and choose **Authenticate**. Your browser opens the Flexprice dashboard: log in, pick the environment the assistant should work in, and click **Authorize**.

In Cursor and most other clients that take a URL:

```json
{
  "mcpServers": {
    "flexprice": {
      "url": "https://mcp.us.flexprice.io/mcp"
    }
  }
}
```

For VS Code, Claude Desktop, Windsurf and other clients, or to connect with an API key, see the [docs](https://docs.flexprice.io/docs/connect/mcp-server).

## Run the server locally

Run the server on your own machine when you use a self-hosted Flexprice, want to work on the server itself, want to limit its tools, or use a client that only runs local (stdio) servers. You need **Node.js 20 or later** and a Flexprice **API key**, created in the dashboard under **Developers**.

### With npx

```bash
npx -y @flexprice/mcp-server start --server-url https://us.api.flexprice.io/v1 --api-key-auth YOUR_API_KEY
```

This starts the server on stdio, where it waits for an MCP client to talk to it. You normally don't run it in a terminal yourself: you put the command in your client's config, and the client starts it. See [Connect a client to your local server](#connect-a-client-to-your-local-server).

### From source

```bash
git clone https://github.com/flexprice/mcp-server.git
cd mcp-server
npm install
npm run build
```

The build writes `bin/mcp-server.js`. Start it the same way as with npx:

```bash
node bin/mcp-server.js start --server-url https://us.api.flexprice.io/v1 --api-key-auth YOUR_API_KEY
```

### Pick the Flexprice API

`--server-url` is the Flexprice **API** base URL, not the MCP URL. Include `/v1` and no trailing slash:

| Region | `--server-url` |
| --- | --- |
| US | `https://us.api.flexprice.io/v1` |
| India | `https://api.cloud.flexprice.io/v1` |
| Self-hosted | Your host, for example `http://localhost:8080/v1` |

The server works in the environment the API key was created in.

### Connect a client to your local server

Most clients (Cursor, Windsurf, Claude Desktop) take a config like this:

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

For a build from source, use `"command": "node"` and put `/absolute/path/to/mcp-server/bin/mcp-server.js` first in `args`, in place of `-y` and `@flexprice/mcp-server`.

- **Claude Code:** `claude mcp add flexprice -- npx -y @flexprice/mcp-server start --server-url https://us.api.flexprice.io/v1 --api-key-auth YOUR_API_KEY`
- **VS Code:** use the same `command` and `args` under `"servers"`, with `"type": "stdio"`.
- **Claude Desktop:** the config file is `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS and `%APPDATA%\Claude\claude_desktop_config.json` on Windows. Restart Claude Desktop after saving.

More config files are in [examples/](https://github.com/flexprice/mcp-server/tree/main/examples).

### Useful flags

- `--scope read` mounts only read-only tools. Repeat `--scope` to combine `read`, `write` and `delete`.
- `--mode dynamic` exposes `list_tools`, `describe_tool_input` and `execute_tool` instead of one tool per operation, which keeps fewer tools in the assistant's context.

Run `node bin/mcp-server.js start --help` to see every flag.
