#!/usr/bin/env node
/**
 * Post-processes swagger output files to produce clean schema names:
 *
 * 1. Replaces github_com_flexprice_flexprice_internal_types. with types. in all swagger files
 *    (swagger.json, swagger.yaml, docs.go, swagger-3-0.json) so schema names are clean.
 * 2. Strips the "dto." prefix from all schema/definition names in swagger.json, swagger.yaml,
 *    docs.go, and swagger-3-0.json so generated SDKs get clean type names natively.
 * 3. In swagger-3-0.json only: adds x-speakeasy-name-override for each components.schemas key
 *    that starts with "types." or "errors." so Speakeasy-generated SDKs use clean names,
 *    FeatureType instead of TypesFeatureType.
 * 4. In swagger-3-0.json only: patches string fields with timestamp-like names (created_at, updated_at,
 *    etc.) with format: date-time so the spec has ≥150 date-time fields for SDK generation.
 *
 * Usage: node scripts/fix_swagger_internal_types.mjs [--spec path/to/swagger-3-0.json]
 */

import { readFileSync, writeFileSync, existsSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(__dirname, '..');

// All known github.com/flexprice/flexprice/internal/<pkg> prefixes to strip
const INTERNAL_PREFIXES = [
  { prefix: 'github_com_flexprice_flexprice_internal_types.', replacement: 'types.' },
  { prefix: 'github_com_flexprice_flexprice_internal_errors.', replacement: 'errors.' },
];

// Keep legacy aliases for the types prefix used in the step-1 loop below
const PREFIX = INTERNAL_PREFIXES[0].prefix;
const REPLACEMENT = INTERNAL_PREFIXES[0].replacement;

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

  // 1. String replace all internal package prefixes in all files
  for (const rel of FILES) {
    const path = resolve(repoRoot, rel);
    if (!existsSync(path)) continue;
    let s = readFileSync(path, 'utf8');
    const before = s;
    for (const { prefix, replacement } of INTERNAL_PREFIXES) {
      s = s.split(prefix).join(replacement);
    }
    if (s !== before) {
      writeFileSync(path, s, 'utf8');
      console.log('Updated', rel);
    }
  }

  // 1b. Strip github_com_flexprice_flexprice_internal_domain_<pkg>. prefixes in all files.
  //     Transforms e.g. "github_com_flexprice_flexprice_internal_domain_addon.Addon" → "Addon".
  const domainPrefixRe = /github_com_flexprice_flexprice_internal_domain_[a-z_]+\./g;
  for (const rel of FILES) {
    const path = resolve(repoRoot, rel);
    if (!existsSync(path)) continue;
    let s = readFileSync(path, 'utf8');
    const before = s;
    s = s.replace(domainPrefixRe, '');
    if (s !== before) {
      writeFileSync(path, s, 'utf8');
      console.log(`Stripped domain package prefixes in ${rel}`);
    }
  }

  // 2. Strip "dto." prefix from all schema/definition names in all files.
  //    Uses a regex to avoid mangling "webhookDto." or similar compound prefixes.
  const dtoPrefixRe = /(?<![A-Za-z0-9_])dto\./g;
  for (const rel of FILES) {
    const path = resolve(repoRoot, rel);
    if (!existsSync(path)) continue;
    let s = readFileSync(path, 'utf8');
    const before = s;
    s = s.replace(dtoPrefixRe, '');
    if (s !== before) {
      writeFileSync(path, s, 'utf8');
      console.log(`Stripped dto. prefix in ${rel}`);
    }
  }

  // 3. Add Speakeasy overrides in swagger-3-0.json only
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

  // Prefixes to strip so Speakeasy generates clean SDK type names
  const STRIP_PREFIXES = ['types.', 'errors.'];

  let count = 0;
  for (const key of Object.keys(schemas)) {
    for (const prefix of STRIP_PREFIXES) {
      if (key.startsWith(prefix)) {
        const override = key.slice(prefix.length);
        if (schemas[key] && typeof schemas[key] === 'object') {
          schemas[key]['x-speakeasy-name-override'] = override;
          count++;
        }
        break;
      }
    }
  }

  // 4. Patch timestamp fields with format: date-time.
  //    Matches the same heuristic used by generate_overlay.py so the spec itself
  //    contains the format info (required for the ≥150 validation criterion) in
  //    addition to the overlay patches that Speakeasy applies at generation time.
  // Note: do not use _period — e.g. billing_period is types.BillingPeriod (MONTHLY), not a timestamp.
  const TIMESTAMP_RE = /(_at|_date|_start|_end|_time|_anchor|expires_at|expiry|due_date|close_time|archived_at|applied_at|executed_at|failed_at|finalized_at|completed_at|last_used_at|balance_updated_at|_due_lte)$/;

  function getProperties(schema) {
    const props = Object.assign({}, schema.properties || {});
    for (const combiner of ['allOf', 'anyOf', 'oneOf']) {
      for (const sub of (schema[combiner] || [])) {
        Object.assign(props, sub.properties || {});
      }
    }
    return props;
  }

  let dtCount = 0;
  for (const schema of Object.values(schemas)) {
    if (!schema || typeof schema !== 'object') continue;
    const props = getProperties(schema);
    for (const [propName, prop] of Object.entries(props)) {
      if (prop && prop.type === 'string' && prop.format !== 'date-time' && TIMESTAMP_RE.test(propName)) {
        prop.format = 'date-time';
        dtCount++;
      }
    }
  }

  // 4b. Remove mistaken date-time (legacy: _period suffix used to match billing_period).
  const NOT_DATE_TIME_STRING_FIELDS = new Set(['billing_period']);
  let stripped = 0;
  for (const schema of Object.values(schemas)) {
    if (!schema || typeof schema !== 'object') continue;
    const props = getProperties(schema);
    for (const [propName, prop] of Object.entries(props)) {
      if (
        NOT_DATE_TIME_STRING_FIELDS.has(propName) &&
        prop &&
        prop.type === 'string' &&
        prop.format === 'date-time'
      ) {
        delete prop.format;
        stripped++;
      }
    }
  }

  writeFileSync(specPath, JSON.stringify(spec, null, 2) + '\n', 'utf8');
  console.log(`Added x-speakeasy-name-override to ${count} prefixed schemas in swagger-3-0.json.`);
  console.log(`Patched ${dtCount} timestamp fields with format: date-time in swagger-3-0.json.`);
  if (stripped > 0) {
    console.log(`Stripped incorrect format: date-time from ${stripped} billing_period field(s).`);
  }
}

main();
