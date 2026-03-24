#!/usr/bin/env node
/**
 * 1. Replaces github_com_flexprice_flexprice_internal_types. with types. in all swagger files
 *    (swagger.json, swagger.yaml, docs.go, swagger-3-0.json) so schema names are clean.
 * 2. In swagger-3-0.json only: adds x-speakeasy-name-override for each components.schemas key
 *    that starts with "types." so Speakeasy-generated SDKs use names like FeatureType instead of TypesFeatureType.
 *
 * Usage: node scripts/fix_swagger_internal_types.mjs [--spec path/to/swagger-3-0.json]
 */

import { readFileSync, writeFileSync, existsSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '..');

const PREFIX = 'github_com_flexprice_flexprice_internal_types.';
const REPLACEMENT = 'types.';

const FILES = [
  'docs/swagger/swagger.json',
  'docs/swagger/swagger.yaml',
  'docs/swagger/docs.go',
  'docs/swagger/swagger-3-0.json',
];

function main() {
  const args = process.argv.slice(2);
  let specPath = resolve(repoRoot, 'docs/swagger/swagger-3-0.json');
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--spec' && args[i + 1]) {
      specPath = resolve(args[i + 1]);
      break;
    }
  }

  // 1. String replace in all files
  for (const rel of FILES) {
    const path = resolve(repoRoot, rel);
    if (!existsSync(path)) continue;
    let s = readFileSync(path, 'utf8');
    const before = s;
    s = s.split(PREFIX).join(REPLACEMENT);
    if (s !== before) {
      writeFileSync(path, s, 'utf8');
      console.log('Updated', rel);
    }
  }

  // 2. Add Speakeasy overrides in swagger-3-0.json only
  if (!existsSync(specPath)) {
    console.log('swagger-3-0.json not found; skipping x-speakeasy-name-override.');
    return;
  }

  const spec = JSON.parse(readFileSync(specPath, 'utf8'));
  const schemas = spec.components?.schemas;
  if (!schemas || typeof schemas !== 'object') {
    console.log('No components.schemas; skipping x-speakeasy-name-override.');
    return;
  }

  let count = 0;
  for (const key of Object.keys(schemas)) {
    if (key.startsWith('types.')) {
      const override = key.slice('types.'.length);
      if (schemas[key] && typeof schemas[key] === 'object') {
        schemas[key]['x-speakeasy-name-override'] = override;
        count++;
      }
    }
  }

  writeFileSync(specPath, JSON.stringify(spec, null, 2) + '\n', 'utf8');
  console.log(`Added x-speakeasy-name-override to ${count} types.* schemas in swagger-3-0.json.`);
}

main();
