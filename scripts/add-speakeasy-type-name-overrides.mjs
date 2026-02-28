#!/usr/bin/env node
/**
 * Adds x-speakeasy-name-override to OpenAPI 3.x component schemas so generated
 * SDKs use clean type names (e.g. FeatureType, Status) instead of package-prefixed
 * names (e.g. TypesFeatureType, TypesStatus).
 *
 * For each key under components.schemas that starts with "types.", sets
 * x-speakeasy-name-override to the part after the prefix (e.g. types.FeatureType → FeatureType).
 *
 * Input/Output: docs/swagger/swagger-3-0.json (modified in place)
 *
 * Usage: node scripts/add-speakeasy-type-name-overrides.mjs [--spec path]
 */

import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '..');

function main() {
  const args = process.argv.slice(2);
  let specPath = resolve(repoRoot, 'docs/swagger/swagger-3-0.json');
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--spec' && args[i + 1]) {
      specPath = resolve(args[i + 1]);
      break;
    }
  }

  const spec = JSON.parse(readFileSync(specPath, 'utf8'));
  const schemas = spec.components?.schemas;
  if (!schemas || typeof schemas !== 'object') {
    console.warn('No components.schemas found; skipping.');
    return;
  }

  let count = 0;
  for (const key of Object.keys(schemas)) {
    if (key.startsWith('types.')) {
      const override = key.slice('types.'.length);
      if (!schemas[key]) continue;
      schemas[key]['x-speakeasy-name-override'] = override;
      count++;
    }
  }

  writeFileSync(specPath, JSON.stringify(spec, null, 2) + '\n', 'utf8');
  console.log(`Added x-speakeasy-name-override to ${count} types.* schemas.`);
}

main();
