#!/usr/bin/env node
// Moves the doc-only /webhook-events/* stubs out of swagger-3-0.json into webhooks.json, the OpenAPI 3.1 spec the docs read.

import { readFileSync, writeFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const specPath = resolve(repoRoot, 'docs/swagger/swagger-3-0.json');
const webhooksPath = resolve(repoRoot, 'docs/swagger/webhooks.json');

const STUB_PREFIX = '/webhook-events/';
const SCHEMA_PREFIX = '#/components/schemas/';

function signatureHeader(name, description) {
  return {
    name,
    in: 'header',
    required: false,
    description: `${description} Sent by Flexprice Cloud only.`,
    schema: { type: 'string' },
  };
}

const SIGNATURE_HEADERS = {
  SvixId: signatureHeader('svix-id', 'Unique message ID, the same on every retry, so use it to deduplicate.'),
  SvixTimestamp: signatureHeader('svix-timestamp', 'Unix time in seconds when the message was sent.'),
  SvixSignature: signatureHeader(
    'svix-signature',
    'Space-separated `v1,<base64>` signatures. See [Signature verification](/developers/webhooks/signature-verification).',
  ),
};

function sentenceCase(name) {
  const words = name.replace(/[._]/g, ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}

function groupOf(event, stub) {
  const group = stub?.tags?.[0];
  if (!group) {
    throw new Error(`${STUB_PREFIX}${event} has no @Tags group`);
  }
  return group;
}

function payloadName(event, stub) {
  const ref = stub?.responses?.['200']?.content?.['application/json']?.schema?.$ref;
  if (!ref?.startsWith(SCHEMA_PREFIX)) {
    throw new Error(`${STUB_PREFIX}${event} has no @Success 200 payload`);
  }
  return ref.slice(SCHEMA_PREFIX.length);
}

// Pins event_type to this event on every page; Svix adds the field to Cloud deliveries even when the payload type lacks it.
function bodySchema(event, name, schemas) {
  const payload = schemas[name];
  const eventType = { type: 'string', enum: [event] };
  if (!payload.properties?.event_type) eventType.description = 'Sent by Flexprice Cloud only.';
  return { ...payload, properties: { ...payload.properties, event_type: eventType } };
}

function webhook(event, stub, schemas) {
  return {
    post: {
      tags: [groupOf(event, stub)],
      summary: event,
      description: (stub.description ?? '').replace(/\s*Doc-only for parsing\.?$/, ''),
      parameters: Object.keys(SIGNATURE_HEADERS).map((key) => ({ $ref: `#/components/parameters/${key}` })),
      requestBody: {
        required: true,
        content: { 'application/json': { schema: bodySchema(event, payloadName(event, stub), schemas) } },
      },
      responses: { '2XX': { description: 'Return any 2xx status to acknowledge receipt.' } },
      'x-mint': {
        metadata: { title: sentenceCase(event), sidebarTitle: event },
        // The URL the old POST page had, so existing links keep working.
        href: `/api-reference/webhook-events/${event.replaceAll('.', '')}`,
      },
    },
  };
}

// Calls onNode on every object reachable from root, following each schema $ref once; returns the schema names reached.
function walk(root, schemas, onNode) {
  const names = new Set();
  const visit = (node) => {
    if (Array.isArray(node)) return node.forEach(visit);
    if (!node || typeof node !== 'object') return;
    onNode(node);
    if (typeof node.$ref === 'string' && node.$ref.startsWith(SCHEMA_PREFIX)) {
      const name = node.$ref.slice(SCHEMA_PREFIX.length);
      if (!names.has(name)) {
        names.add(name);
        visit(schemas[name]);
      }
    }
    Object.values(node).forEach(visit);
  };
  visit(root);
  return names;
}

function referencedSchemas(root, schemas) {
  const names = walk(root, schemas, () => {});
  return Object.fromEntries([...names].sort().map((name) => [name, structuredClone(schemas[name])]));
}

function main() {
  const spec = JSON.parse(readFileSync(specPath, 'utf8'));
  const schemas = spec.components.schemas;
  const stubs = Object.keys(spec.paths)
    .filter((path) => path.startsWith(STUB_PREFIX))
    .map((path) => [path.slice(STUB_PREFIX.length), spec.paths[path].post])
    .sort(([a, stubA], [b, stubB]) => groupOf(a, stubA).localeCompare(groupOf(b, stubB)) || a.localeCompare(b));
  if (stubs.length === 0) {
    console.error(`No ${STUB_PREFIX} stubs in ${specPath}; run make swagger to regenerate it.`);
    process.exit(1);
  }

  const webhooks = Object.fromEntries(stubs.map(([event, stub]) => [event, webhook(event, stub, schemas)]));
  const doc = {
    openapi: '3.1.0',
    info: { title: 'Flexprice webhooks', version: spec.info.version },
    tags: [...new Set(stubs.map(([event, stub]) => groupOf(event, stub)))].map((name) => ({ name })),
    webhooks,
    components: { parameters: SIGNATURE_HEADERS, schemas: referencedSchemas(webhooks, schemas) },
  };
  writeFileSync(webhooksPath, JSON.stringify(doc, null, 2) + '\n', 'utf8');

  // Without the stubs, x-speakeasy-include is what keeps the payload models in the SDKs.
  const payloads = [...new Set(stubs.map(([event, stub]) => payloadName(event, stub)))];
  for (const name of payloads) schemas[name]['x-speakeasy-include'] = true;
  // As stub responses their multi-value enums accepted unknown values; keep that so new event types don't break older SDKs.
  walk(payloads.map((name) => ({ $ref: SCHEMA_PREFIX + name })), schemas, (node) => {
    if (Array.isArray(node.enum) && node.enum.length > 1) node['x-speakeasy-unknown-values'] = 'allow';
  });
  for (const [event] of stubs) delete spec.paths[STUB_PREFIX + event];
  writeFileSync(specPath, JSON.stringify(spec, null, 2) + '\n', 'utf8');
  console.log(`Moved ${stubs.length} webhook stubs from swagger-3-0.json to webhooks.json`);
}

main();
