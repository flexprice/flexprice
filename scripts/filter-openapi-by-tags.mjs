#!/usr/bin/env node
/**
 * Filters an OpenAPI 3.x spec to only include operations whose tags intersect
 * with a configured allowed list. Used to generate a smaller spec for MCP so
 * only tools for specific tag groups are exposed.
 *
 * Input: docs/swagger/swagger-3-0.json, api/mcp/.speakeasy/allowed-tags.yaml
 * Output: docs/swagger/swagger-3-0-mcp.json
 *
 * Usage: node scripts/filter-openapi-by-tags.mjs [--spec path] [--allowed path] [--out path]
 */

import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '..');

function parseAllowedTags(content) {
  const tags = [];
  let inAllowed = false;
  for (const line of content.split('\n')) {
    if (/^\s*allowedTags\s*:\s*$/.test(line) || /^\s*allowedTags\s*:/.test(line)) {
      inAllowed = true;
      const rest = line.replace(/^\s*allowedTags\s*:\s*/, '').trim();
      const firstItem = rest.match(/^\s*-\s*["']?([^"'#\s]+)["']?/);
      if (firstItem) tags.push(firstItem[1].trim());
      continue;
    }
    if (inAllowed) {
      const m = line.match(/^\s*-\s*["']?([^"'#\n]+)["']?\s*(#|$)/);
      if (m) tags.push(m[1].trim());
      else if (line.trim() && !line.match(/^\s*#/)) inAllowed = false;
    }
  }
  return tags;
}

function main() {
  const args = process.argv.slice(2);
  let specPath = resolve(repoRoot, 'docs/swagger/swagger-3-0.json');
  let allowedPath = resolve(repoRoot, 'api/mcp/.speakeasy/allowed-tags.yaml');
  let outPath = resolve(repoRoot, 'docs/swagger/swagger-3-0-mcp.json');

  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--spec' && args[i + 1]) specPath = resolve(args[++i]);
    else if (args[i] === '--allowed' && args[i + 1]) allowedPath = resolve(args[++i]);
    else if (args[i] === '--out' && args[i + 1]) outPath = resolve(args[++i]);
  }

  const specRaw = readFileSync(specPath, 'utf8');
  const allowedRaw = readFileSync(allowedPath, 'utf8');

  const spec = JSON.parse(specRaw);
  const allowedTags = parseAllowedTags(allowedRaw);
  const allowedSet = new Set(allowedTags);

  if (allowedTags.length === 0) {
    console.error('No allowed tags found in', allowedPath);
    process.exit(1);
  }

  const paths = spec.paths || {};
  let kept = 0;
  let removed = 0;

  for (const pathKey of Object.keys(paths)) {
    const pathItem = paths[pathKey];
    for (const method of Object.keys(pathItem)) {
      if (method.startsWith('x-')) continue;
      const op = pathItem[method];
      if (!op || typeof op !== 'object') continue;
      const tags = op.tags || [];
      const hasAllowed = tags.some((t) => allowedSet.has(t));
      if (!hasAllowed) {
        delete pathItem[method];
        removed++;
      } else {
        kept++;
      }
    }
    if (Object.keys(pathItem).every((k) => k.startsWith('x-'))) {
      delete paths[pathKey];
    }
  }

  writeFileSync(outPath, JSON.stringify(spec, null, 2), 'utf8');
  console.log(`Wrote ${outPath}: ${kept} operations kept, ${removed} removed (allowed tags: ${allowedTags.join(', ')})`);
}

main();
