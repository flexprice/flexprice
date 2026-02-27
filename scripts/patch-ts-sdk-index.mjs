#!/usr/bin/env node
/**
 * Appends "export * from \"./index.extras.js\";" to api/typescript/src/index.ts
 * so shared models (enums, types) are re-exported from the package root.
 * Run after merge-custom so index.extras.ts is present.
 */
import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '..');
const indexPath = resolve(repoRoot, 'api/typescript/src/index.ts');
const exportLine = 'export * from "./index.extras.js";';

try {
  let content = readFileSync(indexPath, 'utf8');
  if (content.includes('index.extras')) {
    console.log('index.ts already exports index.extras; skipping.');
    process.exit(0);
  }
  const trimmed = content.trimEnd();
  if (!trimmed.endsWith(';') && !trimmed.endsWith('}')) {
    content = trimmed + '\n' + exportLine + '\n';
  } else {
    content = trimmed + '\n' + exportLine + '\n';
  }
  writeFileSync(indexPath, content, 'utf8');
  console.log('Patched api/typescript/src/index.ts to re-export index.extras (shared models at root).');
} catch (err) {
  if (err.code === 'ENOENT') {
    console.warn('api/typescript/src/index.ts not found; run SDK generation first.');
    process.exit(0);
  }
  throw err;
}
