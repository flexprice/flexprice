# Flexprice MCP client config examples

Copy the relevant snippet into your editor's MCP config. Replace `YOUR_API_KEY` with your key from [app.flexprice.io](us.flexprice.io). See the main [MCP README](../README.md) for full setup instructions.

| File                                 | Use case                                            |
| ------------------------------------ | --------------------------------------------------- |
| `cursor-mcp-config.example.json`     | Cursor — read + write (recommended default)         |
| `claude-desktop-config.example.json` | Claude Desktop — read + write (recommended default) |
| `readonly.example.json`              | Any client — read-only, safest for exploration      |
| `dynamic-mode.example.json`          | Any client — dynamic mode for lower token usage     |
| `admin-full-access.example.json`     | Any client — full access including destructive ops  |
