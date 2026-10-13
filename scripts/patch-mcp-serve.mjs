#!/usr/bin/env node
/**
 * Wires the custom browser-login.ts (api/custom/mcp/src/mcp-server/browser-login.ts) into the generated
 * serve command, api/mcp/src/mcp-server/cli/serve/impl.ts, or the file given as the first argument.
 * Run after merge-custom. Exits 1 if the file or a generated line is missing, so a generator change stops the release.
 */
import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '..');
const servePath = resolve(process.argv[2] ?? resolve(repoRoot, 'api/mcp/src/mcp-server/cli/serve/impl.ts'));

const edits = [
  {
    line: 'import { buildSDK } from "../../tools.js";',
    to: () => 'import { mountBrowserLogin, registerEnvironmentTool, sdkForRequest } from "../../browser-login.js";',
  },
  {
    line: 'res.header("Access-Control-Allow-Headers", "*");',
    to: (indent) =>
      'res.header("Access-Control-Allow-Headers", "*, Authorization");\n' +
      `${indent}res.header("Access-Control-Expose-Headers", "WWW-Authenticate");`,
  },
  {
    line: 'app.use(express.json());',
    to: (indent) => `mountBrowserLogin(app);\n${indent}app.use(express.json());`,
  },
  {
    line: 'buildSDK(headers, cliFlags, cliFlags["disable-static-auth"], logger),',
    to: () => 'sdkForRequest(headers, cliFlags, cliFlags["disable-static-auth"], logger),',
  },
  {
    line: 'await mcpServer.connect(transport as Transport);',
    to: (indent) =>
      'registerEnvironmentTool(mcpServer, headers, cliFlags, logger);\n' +
      `${indent}await mcpServer.connect(transport as Transport);`,
  },
];

function fail(message) {
  console.error(`patch-mcp-serve: ${message}`);
  process.exitCode = 1;
}

let content;
try {
  content = readFileSync(servePath, 'utf8');
} catch (err) {
  if (err.code !== 'ENOENT') throw err;
  fail(`${servePath} not found. Did the generator move the serve command?`);
  process.exit();
}

if (content.includes('../../browser-login.js')) {
  console.log('serve/impl.ts already uses browser-login.ts; skipping.');
  process.exit(0);
}

for (const { line } of edits) {
  const count = content.split(line).length - 1;
  if (count !== 1) {
    fail(`${line}\n  expected once in ${servePath}, found ${count} times`);
  }
}
if (process.exitCode) process.exit();

const patched = edits.reduce((text, { line, to }) => {
  const start = text.indexOf(line);
  const indent = text.slice(text.lastIndexOf('\n', start) + 1, start);
  return text.slice(0, start) + to(indent) + text.slice(start + line.length);
}, content);

writeFileSync(servePath, patched, 'utf8');
console.log('Patched serve/impl.ts to use browser-login.ts.');
