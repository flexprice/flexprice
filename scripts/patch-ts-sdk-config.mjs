#!/usr/bin/env node
/**
 * Adds the bearerAuth and environmentId options to SDKOptions in api/typescript/src/lib/config.ts,
 * right after the generated apiKeyAuth line. The custom auth-headers hook reads them.
 * Run after merge-custom. Exits 1 if the apiKeyAuth line is missing, so a generator change stops the release.
 */
import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '..');
const configPath = resolve(repoRoot, 'api/typescript/src/lib/config.ts');
const apiKeyLine = /^([ \t]*)apiKeyAuth\?: .*;$/m;

const content = readFileSync(configPath, 'utf8');
if (/^\s*bearerAuth\?:/m.test(content)) {
  console.log('config.ts already declares bearerAuth; skipping.');
  process.exit(0);
}

const match = content.match(apiKeyLine);
if (!match) {
  console.error('patch-ts-sdk-config: apiKeyAuth line not found in SDKOptions in api/typescript/src/lib/config.ts.');
  process.exit(1);
}

const indent = match[1];
const fields = [
  '/** Signed-in user\'s JWT, sent as `Authorization: Bearer <token>`. Use instead of apiKeyAuth. */',
  'bearerAuth?: string | (() => string | undefined | Promise<string | undefined>) | undefined;',
  '/** Sent as `X-Environment-ID`. Needed with bearerAuth on environment-scoped endpoints. */',
  'environmentId?: string | (() => string | undefined | Promise<string | undefined>) | undefined;',
].map((line) => indent + line).join('\n');

writeFileSync(configPath, content.replace(apiKeyLine, (line) => `${line}\n${fields}`), 'utf8');
console.log('Patched api/typescript/src/lib/config.ts with bearerAuth and environmentId options.');
